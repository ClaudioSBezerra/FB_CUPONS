// Comando api sobe a API do Cupons ITD.
package main

import (
	"context"
	"errors"
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

	// 2. Administrador inicial, se o banco está vazio.
	if cfg.AdminEmail != "" && cfg.AdminSenha != "" {
		hash, err := auth.HashSenha(cfg.AdminSenha)
		if err != nil {
			return err
		}
		criado, err := st.GarantirAdmin(ctx, cfg.AdminEmail, "Administrador", hash)
		if err != nil {
			return err
		}
		if criado {
			log.Info("usuário administrador criado", "email", cfg.AdminEmail)
		}
	} else {
		log.Warn("ADMIN_EMAIL/ADMIN_SENHA não definidos: nenhum usuário será criado automaticamente")
	}

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
