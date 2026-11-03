package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/validacao"
)

// CupomResposta é o cupom devolvido à tela, já com o veredito da validação.
type CupomResposta struct {
	dominio.Cupom
	// PodeConfirmar e Bloqueios dizem à tela se o botão Confirmar vale.
	PodeConfirmar bool          `json:"pode_confirmar"`
	Bloqueios     []string      `json:"bloqueios"`
	SomaItens     dominio.Valor `json:"soma_itens"`
	// Parecidos avisa sobre cupons de número semelhante em outro cliente.
	Parecidos []string `json:"parecidos,omitempty"`
}

func (s *Servidor) listarCupons(w http.ResponseWriter, r *http.Request) {
	f := store.Filtro{
		Mes:       strings.TrimSpace(r.URL.Query().Get("mes")),
		ClienteID: int64(inteiroDaQuery(r, "cliente_id", 0)),
		Status:    dominio.Status(strings.TrimSpace(r.URL.Query().Get("status"))),
		Numero:    strings.TrimSpace(r.URL.Query().Get("numero")),
		Limite:    inteiroDaQuery(r, "limite", 200),
		Offset:    inteiroDaQuery(r, "offset", 0),
	}
	if f.Mes != "" && !mesValido(f.Mes) {
		erroJSON(w, http.StatusBadRequest, "o mês deve estar no formato AAAA-MM")
		return
	}
	if f.Status != "" && f.Status != dominio.StatusPendente &&
		f.Status != dominio.StatusConfirmado && f.Status != dominio.StatusRejeitado {
		erroJSON(w, http.StatusBadRequest, "status deve ser pendente, confirmado ou rejeitado")
		return
	}

	cupons, err := s.store.ListarCupons(r.Context(), f)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if cupons == nil {
		cupons = []dominio.Cupom{}
	}
	escreverJSON(w, http.StatusOK, cupons)
}

func (s *Servidor) obterCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	cupom, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	resp, err := s.montarResposta(r, cupom)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, resp)
}

// montarResposta reavalia o cupom com as regras atuais, sem gravar nada: a
// tela de revisão precisa dos avisos e bloqueios recalculados.
func (s *Servidor) montarResposta(r *http.Request, cupom dominio.Cupom) (CupomResposta, error) {
	ctxValidacao, err := s.contextoValidacao(r, cupom)
	if err != nil {
		return CupomResposta{}, err
	}

	copia := cupom
	copia.Itens = append([]dominio.Item(nil), cupom.Itens...)
	res := validacao.Valida(&copia, ctxValidacao)

	resp := CupomResposta{
		Cupom:         copia,
		PodeConfirmar: res.PodeConfirmar,
		Bloqueios:     res.Bloqueios,
		SomaItens:     copia.SomaItens(),
	}
	if resp.Bloqueios == nil {
		resp.Bloqueios = []string{}
	}

	// Aviso de número parecido em outro cliente (caso 1342024 vs 1342014).
	if parecidos, err := s.store.NumerosParecidos(r.Context(), cupom.Numero,
		cupom.CNPJCliente, cupom.Data.Competencia()); err == nil {
		for _, p := range parecidos {
			resp.Parecidos = append(resp.Parecidos, fmt.Sprintf(
				"O cupom nº %s (%s) tem número parecido com este. Confira se não houve troca de dígito.",
				p.Numero, p.Cliente))
		}
	}

	return resp, nil
}

func (s *Servidor) contextoValidacao(r *http.Request, cupom dominio.Cupom) (validacao.Contexto, error) {
	catalogo, err := s.store.CarregarCatalogo(r.Context())
	if err != nil {
		return validacao.Contexto{}, err
	}
	clientes, err := s.store.ListarClientes(r.Context(), true)
	if err != nil {
		return validacao.Contexto{}, err
	}
	cnpjs := make([]string, 0, len(clientes))
	for _, c := range clientes {
		cnpjs = append(cnpjs, c.CNPJ)
	}
	return validacao.Contexto{
		Catalogo:       catalogo,
		CNPJConhecidos: cnpjs,
		MesReferencia:  cupom.Data.Competencia(),
	}, nil
}

// pedidoAtualizarCupom é o corpo de PUT /api/cupons/{id}: a edição feita na
// tela de revisão.
type pedidoAtualizarCupom struct {
	Numero      string        `json:"numero"`
	Data        dominio.Data  `json:"data"`
	Cliente     string        `json:"cliente"`
	CNPJCliente string        `json:"cnpj_cliente"`
	Total       dominio.Valor `json:"total"`
	Obs         string        `json:"obs"`
	Itens       []pedidoItem  `json:"itens"`
}

type pedidoItem struct {
	Codigo  string          `json:"cod"`
	Produto string          `json:"prod"`
	Unidade dominio.Unidade `json:"un"`
	Qtd     dominio.Qtd     `json:"qtd"`
	Vun     dominio.Valor   `json:"vun"`
	Vtot    dominio.Valor   `json:"vtot"`
}

func (s *Servidor) atualizarCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}

	atual, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if atual.Status == dominio.StatusConfirmado {
		erroJSON(w, http.StatusConflict,
			"este cupom já está confirmado; rejeite-o antes de editar, para não alterar uma cobrança já fechada")
		return
	}

	var p pedidoAtualizarCupom
	if !lerJSON(w, r, &p) {
		return
	}

	// A edição humana substitui a leitura: os campos editados deixam de ser
	// incertos, mas número e data continuam na lista por regra do domínio
	// (validacao.Valida os recoloca).
	cupom := atual
	cupom.Numero = strings.TrimSpace(p.Numero)
	cupom.Data = p.Data
	cupom.Cliente = strings.TrimSpace(p.Cliente)
	cupom.CNPJCliente = dominio.SoDigitos(p.CNPJCliente)
	cupom.Total = p.Total
	cupom.Obs = strings.TrimSpace(p.Obs)
	cupom.Incertos = nil
	cupom.Avisos = nil

	cupom.Itens = make([]dominio.Item, 0, len(p.Itens))
	for i, it := range p.Itens {
		cupom.Itens = append(cupom.Itens, dominio.Item{
			Ordem:   i + 1,
			Codigo:  strings.TrimSpace(it.Codigo),
			Produto: strings.TrimSpace(it.Produto),
			Unidade: it.Unidade,
			Qtd:     it.Qtd,
			Vun:     it.Vun,
			Vtot:    it.Vtot,
		})
	}

	ctxValidacao, err := s.contextoValidacao(r, cupom)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	validacao.Valida(&cupom, ctxValidacao)

	grav, err := s.store.SalvarCupom(r.Context(), cupom, nil)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	resp, err := s.montarResposta(r, grav.Cupom)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, resp)
}

type pedidoDecisao struct {
	// Obs vira a justificativa gravada quando o operador confirma um cupom
	// que não fecha (regra 6.8).
	Obs string `json:"obs"`
	// ForcarDivergencia autoriza confirmar apesar dos bloqueios, desde que
	// haja justificativa.
	ForcarDivergencia bool `json:"forcar_divergencia"`
}

func (s *Servidor) confirmarCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	var p pedidoDecisao
	if r.ContentLength > 0 && !lerJSON(w, r, &p) {
		return
	}

	cupom, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	ctxValidacao, err := s.contextoValidacao(r, cupom)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	copia := cupom
	copia.Itens = append([]dominio.Item(nil), cupom.Itens...)
	res := validacao.Valida(&copia, ctxValidacao)

	if !res.PodeConfirmar {
		// Regra 6.8: só passa com confirmação explícita E justificativa, que
		// fica gravada em obs.
		if !p.ForcarDivergencia {
			erroJSON(w, http.StatusUnprocessableEntity,
				"este cupom não pode ser confirmado como está", res.Bloqueios...)
			return
		}
		if strings.TrimSpace(p.Obs) == "" {
			erroJSON(w, http.StatusUnprocessableEntity,
				"para confirmar um cupom com divergência, escreva a justificativa", res.Bloqueios...)
			return
		}
		p.Obs = fmt.Sprintf("Confirmado com divergência: %s | Impedimentos: %s",
			strings.TrimSpace(p.Obs), strings.Join(res.Bloqueios, " "))
	}

	usuario, _ := UsuarioDoContexto(r.Context())
	if err := s.store.MudarStatus(r.Context(), id, dominio.StatusConfirmado, p.Obs, &usuario.ID); err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	atualizado, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	resp, err := s.montarResposta(r, atualizado)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, resp)
}

func (s *Servidor) rejeitarCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	var p pedidoDecisao
	if r.ContentLength > 0 && !lerJSON(w, r, &p) {
		return
	}
	usuario, _ := UsuarioDoContexto(r.Context())
	if err := s.store.MudarStatus(r.Context(), id, dominio.StatusRejeitado, p.Obs, &usuario.ID); err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, map[string]any{"ok": true, "status": dominio.StatusRejeitado})
}

type pedidoReprocessar struct {
	// Forte usa o modelo mais capaz; Modelo permite escolher um específico.
	Forte  bool   `json:"forte"`
	Modelo string `json:"modelo"`
}

// reprocessarCupom relê a foto do cupom, opcionalmente com modelo mais forte.
func (s *Servidor) reprocessarCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	if !s.cfg.LeituraHabilitada() {
		erroJSON(w, http.StatusServiceUnavailable,
			"a leitura automática está desligada: defina ANTHROPIC_API_KEY no servidor")
		return
	}

	var p pedidoReprocessar
	if r.ContentLength > 0 && !lerJSON(w, r, &p) {
		return
	}

	cupom, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if cupom.Status == dominio.StatusConfirmado {
		erroJSON(w, http.StatusConflict,
			"este cupom já está confirmado; rejeite-o antes de reprocessar")
		return
	}
	if cupom.FotoID == nil {
		erroJSON(w, http.StatusUnprocessableEntity,
			"este cupom não tem foto guardada (provavelmente veio da importação do legado); não há o que reler")
		return
	}

	modelo := strings.TrimSpace(p.Modelo)
	if modelo == "" && p.Forte {
		modelo = s.cfg.ClaudeModeloForte
	}

	novo, err := s.importador.Reprocessar(r.Context(), cupom, modelo)
	if err != nil {
		s.log.Error("falha ao reprocessar cupom", "cupom", id, "erro", err)
		erroJSON(w, http.StatusBadGateway, err.Error())
		return
	}

	resp, err := s.montarResposta(r, novo)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, resp)
}

func (s *Servidor) fotoDoCupom(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	cupom, err := s.store.CupomPorID(r.Context(), id)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if cupom.FotoID == nil {
		erroJSON(w, http.StatusNotFound, "este cupom não tem foto guardada")
		return
	}

	mime, dados, err := s.store.ConteudoFoto(r.Context(), *cupom.FotoID)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(dados)))
	// Fotos de cupom contêm CNPJ de cliente: nunca em cache compartilhado.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(dados); err != nil {
		s.log.Warn("falha ao enviar a foto", "cupom", id, "erro", err)
	}
}

// --- auxiliares ---

func idDaRota(w http.ResponseWriter, r *http.Request) (int64, bool) {
	txt := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(txt, 10, 64)
	if err != nil || id <= 0 {
		erroJSON(w, http.StatusBadRequest, fmt.Sprintf("identificador inválido: %q", txt))
		return 0, false
	}
	return id, true
}

func inteiroDaQuery(r *http.Request, chave string, padrao int) int {
	v := strings.TrimSpace(r.URL.Query().Get(chave))
	if v == "" {
		return padrao
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return padrao
	}
	return n
}

// mesValido confere o formato AAAA-MM.
func mesValido(mes string) bool {
	if len(mes) != 7 || mes[4] != '-' {
		return false
	}
	_, err := dominio.ParseData(mes + "-01")
	return err == nil
}
