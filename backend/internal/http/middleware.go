package http

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/auth"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

type chaveContexto string

const chaveUsuario chaveContexto = "usuario"

// UsuarioDoContexto devolve o usuário autenticado da requisição.
func UsuarioDoContexto(ctx context.Context) (store.Usuario, bool) {
	u, ok := ctx.Value(chaveUsuario).(store.Usuario)
	return u, ok
}

// exigirAutenticacao valida a sessão e carrega o usuário.
func (s *Servidor) exigirAutenticacao(prox http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(auth.NomeCookie); err == nil {
			token = c.Value
		}
		// Bearer também é aceito, para scripts e para o roteiro de operação.
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if token == "" {
			erroJSON(w, http.StatusUnauthorized, "é necessário entrar no sistema")
			return
		}

		sessao, err := s.sessoes.Validar(token)
		if err != nil {
			s.sessoes.LimparCookie(w)
			erroJSON(w, http.StatusUnauthorized, "sua sessão expirou; entre novamente")
			return
		}

		usuario, err := s.store.UsuarioPorID(r.Context(), sessao.UsuarioID)
		if err != nil || !usuario.Ativo {
			s.sessoes.LimparCookie(w)
			erroJSON(w, http.StatusUnauthorized, "usuário sem acesso ao sistema")
			return
		}

		prox.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chaveUsuario, usuario)))
	})
}

// exigirAdmin restringe a rota ao papel admin.
func (s *Servidor) exigirAdmin(prox http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UsuarioDoContexto(r.Context())
		if !ok || u.Papel != "admin" {
			erroJSON(w, http.StatusForbidden, "esta ação é restrita a administradores")
			return
		}
		prox.ServeHTTP(w, r)
	})
}

// registrarAcesso loga cada requisição de forma estruturada, sem corpo nem
// cabeçalho de autenticação.
func registrarAcesso(log *slog.Logger) func(http.Handler) http.Handler {
	return func(prox http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inicio := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			prox.ServeHTTP(ww, r)

			nivel := slog.LevelInfo
			if ww.Status() >= 500 {
				nivel = slog.LevelError
			}
			log.Log(r.Context(), nivel, "requisição",
				"metodo", r.Method,
				"rota", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"ms", time.Since(inicio).Milliseconds(),
				"id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// limitador é um rate limit simples por IP, em memória. Protege a importação,
// que é a rota cara (cada página é uma chamada paga à API do Claude).
type limitador struct {
	mu        sync.Mutex
	janela    time.Duration
	maximo    int
	registros map[string][]time.Time
}

func novoLimitador(maximo int, janela time.Duration) *limitador {
	return &limitador{janela: janela, maximo: maximo, registros: map[string][]time.Time{}}
}

func (l *limitador) permitir(chave string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	agora := time.Now()
	corte := agora.Add(-l.janela)

	recentes := l.registros[chave][:0]
	for _, t := range l.registros[chave] {
		if t.After(corte) {
			recentes = append(recentes, t)
		}
	}
	if len(recentes) >= l.maximo {
		l.registros[chave] = recentes
		return false
	}
	l.registros[chave] = append(recentes, agora)
	return true
}

// middleware aplica o limite por IP.
func (l *limitador) middleware(prox http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.permitir(ipDoCliente(r)) {
			erroJSON(w, http.StatusTooManyRequests,
				"muitas requisições em pouco tempo; aguarde um instante e tente de novo")
			return
		}
		prox.ServeHTTP(w, r)
	})
}

func ipDoCliente(r *http.Request) string {
	// Atrás do proxy reverso o IP real vem no cabeçalho; chi/middleware.RealIP
	// já normaliza RemoteAddr.
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}
