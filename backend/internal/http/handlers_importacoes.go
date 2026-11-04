package http

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/importacao"
)

// criarImportacao recebe PDFs e/ou imagens em multipart e processa o lote.
//
// É a rota que o pedido "importar por arquivos em PDF como o Claude Web"
// atende: cada página do PDF vira uma imagem e vai para a leitura por visão.
func (s *Servidor) criarImportacao(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.LeituraHabilitada() {
		erroJSON(w, http.StatusServiceUnavailable,
			"a leitura automática está desligada: defina ANTHROPIC_API_KEY no servidor. "+
				"O resto do sistema (revisão, cobrança, Excel) continua funcionando.")
		return
	}

	limite := s.cfg.UploadMaxMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limite)

	// Mantém o multipart em memória até 32 MB; o excedente vai para disco
	// temporário, que o Go limpa ao fechar o form.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		erroJSON(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"não foi possível ler o envio (limite de %d MB por lote): %v", s.cfg.UploadMaxMB, err))
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	cabecalhos := r.MultipartForm.File["arquivos"]
	if len(cabecalhos) == 0 {
		cabecalhos = r.MultipartForm.File["arquivo"] // tolera o singular
	}
	if len(cabecalhos) == 0 {
		erroJSON(w, http.StatusBadRequest,
			`envie os arquivos no campo "arquivos" (PDF ou imagens)`)
		return
	}

	arquivos := make([]importacao.Arquivo, 0, len(cabecalhos))
	for _, ch := range cabecalhos {
		f, err := ch.Open()
		if err != nil {
			erroJSON(w, http.StatusBadRequest, fmt.Sprintf("não foi possível abrir %q: %v", ch.Filename, err))
			return
		}
		dados := make([]byte, ch.Size)
		if _, err := io.ReadFull(f, dados); err != nil {
			f.Close()
			erroJSON(w, http.StatusBadRequest, fmt.Sprintf("não foi possível ler %q: %v", ch.Filename, err))
			return
		}
		f.Close()
		arquivos = append(arquivos, importacao.Arquivo{Nome: ch.Filename, Dados: dados})
	}

	// O mês de referência ajuda a detectar ano lido errado (OCR lia 2025 em
	// cupons de 2026). Opcional.
	mes := strings.TrimSpace(r.FormValue("mes"))
	if mes != "" && !mesValido(mes) {
		erroJSON(w, http.StatusBadRequest, "o mês de referência deve estar no formato AAAA-MM")
		return
	}

	// A leitura roda DESTACADA da requisição e a resposta sai na hora.
	//
	// Ler 18 páginas pela API de visão leva minutos, e o proxy da Cloudflare
	// corta a requisição por volta de 100 segundos. Quando cortava, o contexto
	// era cancelado e todas as leituras morriam com "context canceled" — um
	// lote inteiro perdido depois de já ter gasto as chamadas. A tela
	// acompanha por GET /api/importacoes/{id}.
	usuario, _ := UsuarioDoContexto(r.Context())
	imp, err := s.importador.EmSegundoPlano(r.Context(), arquivos, &usuario.ID, mes)
	if err != nil {
		s.log.Error("falha ao abrir a importação", "erro", err, "arquivos", len(arquivos))
		erroJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	escreverJSON(w, http.StatusAccepted, imp)
}

func (s *Servidor) obterImportacao(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	imp, err := s.store.ImportacaoPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, imp)
}
