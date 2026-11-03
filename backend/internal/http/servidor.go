package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/auth"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/config"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/importacao"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/leitura"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

// Servidor reúne as dependências dos handlers.
type Servidor struct {
	cfg        config.Config
	store      *store.Store
	sessoes    *auth.Gerenciador
	importador *importacao.Servico
	leitor     leitura.Leitor
	log        *slog.Logger
}

// NovoServidor monta o servidor HTTP.
func NovoServidor(cfg config.Config, st *store.Store, sessoes *auth.Gerenciador,
	importador *importacao.Servico, leitor leitura.Leitor, log *slog.Logger) *Servidor {
	return &Servidor{
		cfg: cfg, store: st, sessoes: sessoes,
		importador: importador, leitor: leitor, log: log,
	}
}

// Rotas monta o roteador completo.
func (s *Servidor) Rotas() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(registrarAcesso(s.log))
	r.Use(middleware.Timeout(10 * time.Minute)) // um PDF de 20 páginas demora

	if len(s.cfg.OrigensCORS) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.cfg.OrigensCORS,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			AllowCredentials: true, // o cookie de sessão depende disso
			MaxAge:           300,
		}))
	}

	// Sondas de saúde ficam fora da autenticação, para o proxy e o Docker.
	r.Get("/healthz", s.saude)
	r.Get("/readyz", s.pronto)

	limiteImportacao := novoLimitador(20, time.Minute)
	limiteLogin := novoLimitador(10, time.Minute)

	r.Route("/api", func(api chi.Router) {
		api.Group(func(publico chi.Router) {
			publico.Use(limiteLogin.middleware)
			publico.Post("/auth/login", s.login)
			publico.Post("/auth/logout", s.logout)
		})

		api.Group(func(priv chi.Router) {
			priv.Use(s.exigirAutenticacao)

			priv.Get("/me", s.me)

			priv.With(limiteImportacao.middleware).Post("/importacoes", s.criarImportacao)
			priv.Get("/importacoes/{id}", s.obterImportacao)

			priv.Get("/cupons", s.listarCupons)
			priv.Get("/cupons/{id}", s.obterCupom)
			priv.Put("/cupons/{id}", s.atualizarCupom)
			priv.Post("/cupons/{id}/confirmar", s.confirmarCupom)
			priv.Post("/cupons/{id}/rejeitar", s.rejeitarCupom)
			priv.With(limiteImportacao.middleware).Post("/cupons/{id}/reprocessar", s.reprocessarCupom)
			priv.Get("/cupons/{id}/foto", s.fotoDoCupom)

			priv.Get("/clientes", s.listarClientes)
			priv.Post("/clientes", s.criarCliente)
			priv.Put("/clientes/{id}", s.atualizarCliente)

			priv.Get("/produtos", s.listarProdutos)
			priv.Post("/produtos", s.criarProduto)
			priv.Put("/produtos/{id}", s.atualizarProduto)

			priv.Get("/cobranca", s.obterCobranca)
			priv.Get("/cobranca/export.xlsx", s.exportarCobranca)

			priv.With(s.exigirAdmin).Post("/legado/importar", s.importarLegado)
			priv.With(s.exigirAdmin).Get("/admin/usuarios", s.listarUsuarios)
			priv.With(s.exigirAdmin).Post("/admin/usuarios", s.criarUsuario)
			priv.With(s.exigirAdmin).Get("/admin/uso", s.uso)
		})
	})

	return r
}

// saude responde sempre que o processo está vivo.
func (s *Servidor) saude(w http.ResponseWriter, r *http.Request) {
	escreverJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"leitura": s.cfg.LeituraHabilitada(),
	})
}

// pronto confere as dependências — é esta que o proxy deve usar.
func (s *Servidor) pronto(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		s.log.Error("banco não respondeu na sonda", "erro", err)
		escreverJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"banco": "indisponível",
		})
		return
	}
	escreverJSON(w, http.StatusOK, map[string]any{"ok": true, "banco": "ok"})
}
