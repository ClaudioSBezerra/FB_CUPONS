package http

import (
	"fmt"
	"io"
	"net/http"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/importacao"
)

// importarLegado carrega os JSONs exportados pelo protótipo HTML.
//
// Aceita multipart (campo "arquivos") ou um único JSON no corpo. É idempotente
// pela chave (numero, cnpj_cliente): reimportar não duplica.
//
// A resposta traz a conferência por cliente, para comparar com o critério de
// aceite do CLAUDE.md §10 sem precisar abrir o banco.
func (s *Servidor) importarLegado(w http.ResponseWriter, r *http.Request) {
	limite := s.cfg.UploadMaxMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limite)

	tipo := r.Header.Get("Content-Type")
	var resumos []importacao.ResumoLegado

	if len(tipo) >= 19 && tipo[:19] == "multipart/form-data" {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			erroJSON(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("não foi possível ler o envio: %v", err))
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		cabecalhos := r.MultipartForm.File["arquivos"]
		if len(cabecalhos) == 0 {
			erroJSON(w, http.StatusBadRequest, `envie os JSONs no campo "arquivos"`)
			return
		}
		for _, ch := range cabecalhos {
			f, err := ch.Open()
			if err != nil {
				erroJSON(w, http.StatusBadRequest,
					fmt.Sprintf("não foi possível abrir %q: %v", ch.Filename, err))
				return
			}
			dados, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				erroJSON(w, http.StatusBadRequest,
					fmt.Sprintf("não foi possível ler %q: %v", ch.Filename, err))
				return
			}
			resumo, err := s.importador.ImportarLegado(r.Context(), ch.Filename, dados)
			if err != nil {
				erroJSON(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			resumos = append(resumos, resumo)
		}
	} else {
		dados, err := io.ReadAll(r.Body)
		if err != nil {
			erroJSON(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}
		resumo, err := s.importador.ImportarLegado(r.Context(), "corpo-da-requisicao", dados)
		if err != nil {
			erroJSON(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		resumos = append(resumos, resumo)
	}

	escreverJSON(w, http.StatusOK, map[string]any{"importacoes": resumos})
}
