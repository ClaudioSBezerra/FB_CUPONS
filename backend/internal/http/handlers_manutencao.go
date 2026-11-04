package http

import (
	"net/http"
	"strings"
)

// ConfirmacaoZerar é a palavra que o operador precisa digitar para apagar o
// movimento. Existe para que um clique acidental não limpe a base.
const ConfirmacaoZerar = "APAGAR"

type pedidoZerar struct {
	Confirmacao string `json:"confirmacao"`
}

// zerarMovimento apaga cupons, itens, fotos, importações e o log de leituras,
// preservando clientes, produtos e usuários.
//
// Restrito a admin e protegido por confirmação digitada. Serve para repetir a
// importação de um PDF durante os testes: como a importação deduplica imagens
// por SHA-256, sem limpar as fotos o mesmo arquivo seria recusado.
func (s *Servidor) zerarMovimento(w http.ResponseWriter, r *http.Request) {
	var p pedidoZerar
	if r.ContentLength > 0 && !lerJSON(w, r, &p) {
		return
	}

	if strings.TrimSpace(p.Confirmacao) != ConfirmacaoZerar {
		erroJSON(w, http.StatusUnprocessableEntity,
			`esta ação apaga todos os cupons e não tem volta. Para confirmar, envie {"confirmacao":"`+
				ConfirmacaoZerar+`"}`)
		return
	}

	usuario, _ := UsuarioDoContexto(r.Context())

	// Antes de apagar, registra o que havia. Sem isso, não há como saber
	// depois o que foi perdido.
	contagem, err := s.store.ContarPorStatus(r.Context(), "", 0)
	if err == nil {
		s.log.Warn("APAGANDO todo o movimento a pedido do operador",
			"usuario", usuario.Email,
			"confirmados_antes", contagem["confirmado"],
			"pendentes_antes", contagem["pendente"],
			"rejeitados_antes", contagem["rejeitado"],
			"ip", ipDoCliente(r))
	}

	apagado, err := s.store.ZerarMovimento(r.Context())
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	s.log.Warn("movimento apagado",
		"usuario", usuario.Email, "cupons", apagado.Cupons, "itens", apagado.Itens,
		"fotos", apagado.Fotos, "importacoes", apagado.Importacoes,
		"leituras", apagado.Leituras)

	escreverJSON(w, http.StatusOK, map[string]any{
		"apagado": apagado,
		"preservado": []string{
			"clientes", "produtos (inclusive os a confirmar)", "usuários",
		},
	})
}
