package http

import (
	"errors"
	"net/http"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/auth"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

type pedidoLogin struct {
	Email string `json:"email"`
	Senha string `json:"senha"`
}

func (s *Servidor) login(w http.ResponseWriter, r *http.Request) {
	var p pedidoLogin
	if !lerJSON(w, r, &p) {
		return
	}

	usuario, err := s.store.UsuarioPorEmail(r.Context(), p.Email)
	if err != nil {
		if errors.Is(err, store.ErroNaoEncontrado) {
			// Mesma mensagem e mesmo custo de um usuário existente com senha
			// errada, para não revelar quais e-mails estão cadastrados.
			erroJSON(w, http.StatusUnauthorized, auth.ErroCredenciais.Error())
			return
		}
		erroDeStore(w, err, s.log)
		return
	}
	if !usuario.Ativo {
		erroJSON(w, http.StatusForbidden, "este usuário está inativo")
		return
	}
	if err := auth.ConferirSenha(usuario.SenhaHash, p.Senha); err != nil {
		s.log.Warn("tentativa de login recusada", "email", p.Email, "ip", ipDoCliente(r))
		erroJSON(w, http.StatusUnauthorized, auth.ErroCredenciais.Error())
		return
	}

	token, expira, err := s.sessoes.Emitir(auth.Sessao{
		UsuarioID: usuario.ID, Email: usuario.Email, Papel: usuario.Papel,
	})
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	s.sessoes.GravarCookie(w, token, expira)

	escreverJSON(w, http.StatusOK, map[string]any{
		"usuario": usuario,
		"expira":  expira,
	})
}

func (s *Servidor) logout(w http.ResponseWriter, r *http.Request) {
	s.sessoes.LimparCookie(w)
	escreverJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Servidor) me(w http.ResponseWriter, r *http.Request) {
	u, ok := UsuarioDoContexto(r.Context())
	if !ok {
		erroJSON(w, http.StatusUnauthorized, "é necessário entrar no sistema")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]any{
		"usuario":            u,
		"leitura_habilitada": s.cfg.LeituraHabilitada(),
		"modelo":             s.cfg.ClaudeModelo,
		"modelo_forte":       s.cfg.ClaudeModeloForte,
	})
}

type pedidoUsuario struct {
	Email string `json:"email"`
	Nome  string `json:"nome"`
	Senha string `json:"senha"`
	Papel string `json:"papel"`
}

func (s *Servidor) listarUsuarios(w http.ResponseWriter, r *http.Request) {
	us, err := s.store.ListarUsuarios(r.Context())
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, us)
}

func (s *Servidor) criarUsuario(w http.ResponseWriter, r *http.Request) {
	var p pedidoUsuario
	if !lerJSON(w, r, &p) {
		return
	}
	if p.Papel != "admin" && p.Papel != "operador" {
		p.Papel = "operador"
	}
	hash, err := auth.HashSenha(p.Senha)
	if err != nil {
		erroJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.CriarUsuario(r.Context(), store.Usuario{
		Email: p.Email, Nome: p.Nome, SenhaHash: hash, Papel: p.Papel, Ativo: true,
	})
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusCreated, u)
}

func (s *Servidor) uso(w http.ResponseWriter, r *http.Request) {
	dias := inteiroDaQuery(r, "dias", 30)
	uso, err := s.store.UsoPorDia(r.Context(), dias)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, map[string]any{
		"uso":                 uso,
		"limite_leituras_dia": s.cfg.LimiteLeiturasDia,
	})
}
