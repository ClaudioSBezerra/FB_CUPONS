// Comando api sobe a API do Cupons ITD.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/auth"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/config"
	apihttp "github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/http"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/importacao"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/leitura"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/pdf"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := executar(log); err != nil {
		log.Error("a API não pôde subir", "erro", err)
		os.Exit(1)
	}
}

// garantirAdmin cria o usuário administrador inicial quando o banco ainda não
// tem nenhum usuário. Problemas aqui são avisados, não fatais.
func garantirAdmin(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) {
	if cfg.AdminEmail == "" {
		log.Warn("ADMIN_EMAIL não definida: nenhum usuário será criado automaticamente. " +
			"Sem um usuário, ninguém consegue entrar no sistema")
		return
	}

	senha := cfg.AdminSenha
	sorteada := false
	if !cfg.SenhaAdminUtilizavel() {
		nova, err := auth.SenhaAleatoria()
		if err != nil {
			log.Error("não foi possível sortear a senha do administrador; nenhum usuário foi criado",
				"erro", err)
			return
		}
		senha, sorteada = nova, true
	}

	hash, err := auth.HashSenha(senha)
	if err != nil {
		log.Error("não foi possível preparar a senha do administrador; nenhum usuário foi criado",
			"erro", err)
		return
	}

	criado, err := st.GarantirAdmin(ctx, cfg.AdminEmail, "Administrador", hash)
	if err != nil {
		log.Error("não foi possível criar o administrador inicial; a API segue no ar",
			"erro", err, "email", cfg.AdminEmail)
		return
	}

	switch {
	case criado && sorteada:
		motivo := "ADMIN_SENHA não foi definida"
		if cfg.AdminSenha != "" {
			motivo = fmt.Sprintf("ADMIN_SENHA tem %d caracteres e o mínimo é %d",
				len(cfg.AdminSenha), config.MinimoSenha)
		}
		// A senha aparece no log UMA vez, de propósito: é a única forma de o
		// operador entrar, e o alternativa seria a API não subir.
		log.Warn("administrador criado com SENHA SORTEADA — anote e troque assim que entrar",
			"email", cfg.AdminEmail, "senha", senha, "motivo", motivo)
	case criado:
		log.Info("usuário administrador criado", "email", cfg.AdminEmail)
	default:
		log.Info("já existe usuário cadastrado; o administrador não foi recriado",
			"observacao", "mudar ADMIN_SENHA não altera usuário existente")
	}
}

func executar(log *slog.Logger) error {
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}

	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	// 1. Migrations antes de qualquer consulta.
	log.Info("aplicando migrations")
	if err := store.MigrarFS(cfg.DatabaseURL, migrations.Arquivos, "."); err != nil {
		return err
	}

	st, err := store.Abrir(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Fechar()

	// 2. Administrador inicial. NUNCA fatal: se a senha informada não serve,
	//    sorteia-se uma e anuncia-se no log. Uma API com banco de pé e
	//    migrations aplicadas não deve ficar fora do ar por causa da senha de
	//    bootstrap — foi exatamente esse laço de reinício que atrasou o
	//    primeiro deploy.
	garantirAdmin(ctx, cfg, st, log)

	// 3. Leitor de cupons. Sem credencial, o sistema sobe sem leitura
	//    automática — revisão manual, cobrança e Excel continuam valendo.
	var leitor leitura.Leitor
	if cfg.LeituraHabilitada() {
		leitor = leitura.NovoClienteClaude(leitura.Opcoes{
			APIKey:  cfg.AnthropicAPIKey,
			Modelo:  cfg.ClaudeModelo,
			Esforco: cfg.ClaudeEsforco,
			Timeout: cfg.ClaudeTimeout,
		})
		log.Info("leitura via Claude habilitada",
			"modelo", cfg.ClaudeModelo, "modelo_forte", cfg.ClaudeModeloForte,
			"esforco", cfg.ClaudeEsforco, "concorrencia", cfg.Concorrencia)
	} else {
		leitor = leitura.NovoLeitorGravado()
		log.Warn("ANTHROPIC_API_KEY não definida: a importação por PDF está indisponível")
	}

	importador := importacao.NovoServico(st, leitor, log)
	importador.Concorrencia = cfg.Concorrencia
	importador.LadoMaxImagem = cfg.LadoMaxImagem
	importador.LimiteDiario = cfg.LimiteLeiturasDia
	importador.UsarExtrator(&pdf.Extrator{Pdftoppm: cfg.Pdftoppm, DPI: cfg.DPI})

	servidor := apihttp.NovoServidor(cfg, st, auth.NovoGerenciador(cfg.JWTSecret, cfg.SessaoDuracao, cfg.Producao()),
		importador, leitor, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Porta,
		Handler:           servidor.Rotas(),
		ReadHeaderTimeout: 15 * time.Second,
		// Um PDF de 20 páginas pode levar minutos: o timeout de escrita é
		// generoso de propósito, e o de leitura acompanha o upload grande.
		ReadTimeout:  15 * time.Minute,
		WriteTimeout: 15 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	erros := make(chan error, 1)
	go func() {
		log.Info("API ouvindo", "porta", cfg.Porta, "ambiente", cfg.Ambiente)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
	}()

	select {
	case err := <-erros:
		return err
	case <-ctx.Done():
		log.Info("desligando a API")
	}

	// Desligamento limpo: dá tempo de a importação em andamento terminar.
	ctxDesligar, cancelarDesligar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelarDesligar()
	return srv.Shutdown(ctxDesligar)
}
