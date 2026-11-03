package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/exportxlsx"
)

func (s *Servidor) obterCobranca(w http.ResponseWriter, r *http.Request) {
	mes := strings.TrimSpace(r.URL.Query().Get("mes"))
	if !mesValido(mes) {
		erroJSON(w, http.StatusBadRequest, "informe o mês da cobrança no formato AAAA-MM")
		return
	}
	clienteID := int64(inteiroDaQuery(r, "cliente_id", 0))

	cob, err := s.store.Cobrancar(r.Context(), mes, clienteID)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, cob)
}

// exportarCobranca devolve a planilha do mês.
func (s *Servidor) exportarCobranca(w http.ResponseWriter, r *http.Request) {
	mes := strings.TrimSpace(r.URL.Query().Get("mes"))
	if !mesValido(mes) {
		erroJSON(w, http.StatusBadRequest, "informe o mês da cobrança no formato AAAA-MM")
		return
	}
	clienteID := int64(inteiroDaQuery(r, "cliente_id", 0))

	cob, err := s.store.Cobrancar(r.Context(), mes, clienteID)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	itens, err := s.store.ItensDoMes(r.Context(), mes, clienteID)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	cupons, err := s.store.CuponsDoMes(r.Context(), mes, clienteID)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	nomeCliente := ""
	if clienteID > 0 && len(cob.Subtotais) > 0 {
		nomeCliente = cob.Subtotais[0].Cliente
	}
	nome := exportxlsx.NomeArquivo(mes, nomeCliente)

	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, nome))
	w.Header().Set("Cache-Control", "private, no-store")

	if err := exportxlsx.Gerar(w, exportxlsx.Dados{
		Mes: mes, Cobranca: cob, Itens: itens, Cupons: cupons,
	}); err != nil {
		// O cabeçalho já foi enviado: só resta registrar.
		s.log.Error("falha ao gerar a planilha", "mes", mes, "erro", err)
	}
}
