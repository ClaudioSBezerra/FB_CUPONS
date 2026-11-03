// Package http expõe a API REST do Cupons ITD.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

// RespostaErro é o corpo devolvido em qualquer falha. A mensagem é sempre em
// português e voltada ao operador.
type RespostaErro struct {
	Erro string `json:"erro"`
	// Detalhes traz, quando houver, a lista de impedimentos (ex.: por que o
	// cupom não pode ser confirmado).
	Detalhes []string `json:"detalhes,omitempty"`
}

func escreverJSON(w http.ResponseWriter, status int, corpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if corpo == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(corpo); err != nil {
		slog.Error("falha ao escrever a resposta JSON", "erro", err)
	}
}

func erroJSON(w http.ResponseWriter, status int, msg string, detalhes ...string) {
	escreverJSON(w, status, RespostaErro{Erro: msg, Detalhes: detalhes})
}

// erroDeStore traduz os erros do banco para status HTTP.
func erroDeStore(w http.ResponseWriter, err error, log *slog.Logger) {
	var conflito *store.ErroConflito
	switch {
	case errors.Is(err, store.ErroNaoEncontrado):
		erroJSON(w, http.StatusNotFound, "registro não encontrado")
	case errors.As(err, &conflito):
		erroJSON(w, http.StatusConflict, conflito.Msg)
	default:
		// Erro interno não vaza detalhe de banco para o cliente.
		log.Error("erro interno", "erro", err)
		erroJSON(w, http.StatusInternalServerError,
			"erro interno no servidor; a falha foi registrada no log")
	}
}

func lerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		erroJSON(w, http.StatusBadRequest, "não foi possível entender o corpo da requisição: "+err.Error())
		return false
	}
	return true
}
