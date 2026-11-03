package http

import (
	"net/http"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/validacao"
)

func (s *Servidor) listarClientes(w http.ResponseWriter, r *http.Request) {
	incluirInativos := r.URL.Query().Get("inativos") == "1"
	cs, err := s.store.ListarClientes(r.Context(), incluirInativos)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if cs == nil {
		cs = []dominio.Cliente{}
	}
	escreverJSON(w, http.StatusOK, cs)
}

type pedidoCliente struct {
	Nome  string `json:"nome"`
	CNPJ  string `json:"cnpj"`
	Ativo *bool  `json:"ativo"`
	Obs   string `json:"obs"`
}

func (s *Servidor) criarCliente(w http.ResponseWriter, r *http.Request) {
	var p pedidoCliente
	if !lerJSON(w, r, &p) {
		return
	}
	c, ok := clienteDoPedido(w, p, 0)
	if !ok {
		return
	}
	criado, err := s.store.CriarCliente(r.Context(), c)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusCreated, criado)
}

func (s *Servidor) atualizarCliente(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	var p pedidoCliente
	if !lerJSON(w, r, &p) {
		return
	}
	c, ok := clienteDoPedido(w, p, id)
	if !ok {
		return
	}
	if err := s.store.AtualizarCliente(r.Context(), c); err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, c)
}

// clienteDoPedido valida o corpo. CNPJ de cliente é dado crítico: um CNPJ
// inválido no cadastro contaminaria a sugestão de correção das leituras, por
// isso o dígito verificador é exigido aqui.
func clienteDoPedido(w http.ResponseWriter, p pedidoCliente, id int64) (dominio.Cliente, bool) {
	nome := strings.TrimSpace(p.Nome)
	if nome == "" {
		erroJSON(w, http.StatusBadRequest, "informe o nome do cliente")
		return dominio.Cliente{}, false
	}
	cnpj := dominio.SoDigitos(p.CNPJ)
	if !validacao.CNPJValido(cnpj) {
		erroJSON(w, http.StatusBadRequest,
			"o CNPJ informado é inválido (confira os 14 dígitos e o dígito verificador)")
		return dominio.Cliente{}, false
	}
	ativo := true
	if p.Ativo != nil {
		ativo = *p.Ativo
	}
	return dominio.Cliente{ID: id, Nome: nome, CNPJ: cnpj, Ativo: ativo, Obs: strings.TrimSpace(p.Obs)}, true
}

func (s *Servidor) listarProdutos(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListarProdutos(r.Context())
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	if ps == nil {
		ps = []dominio.Produto{}
	}
	escreverJSON(w, http.StatusOK, ps)
}

type pedidoProduto struct {
	Codigo     string          `json:"codigo"`
	Descricao  string          `json:"descricao"`
	Unidade    dominio.Unidade `json:"unidade"`
	PrecoRef   *dominio.Valor  `json:"preco_ref"`
	Confirmado *bool           `json:"confirmado"`
}

func (s *Servidor) criarProduto(w http.ResponseWriter, r *http.Request) {
	var p pedidoProduto
	if !lerJSON(w, r, &p) {
		return
	}
	prod, ok := produtoDoPedido(w, p, 0)
	if !ok {
		return
	}
	criado, err := s.store.CriarProduto(r.Context(), prod)
	if err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusCreated, criado)
}

func (s *Servidor) atualizarProduto(w http.ResponseWriter, r *http.Request) {
	id, ok := idDaRota(w, r)
	if !ok {
		return
	}
	var p pedidoProduto
	if !lerJSON(w, r, &p) {
		return
	}
	prod, ok := produtoDoPedido(w, p, id)
	if !ok {
		return
	}
	if err := s.store.AtualizarProduto(r.Context(), prod); err != nil {
		erroDeStore(w, err, s.log)
		return
	}
	escreverJSON(w, http.StatusOK, prod)
}

func produtoDoPedido(w http.ResponseWriter, p pedidoProduto, id int64) (dominio.Produto, bool) {
	desc := strings.TrimSpace(p.Descricao)
	if desc == "" {
		erroJSON(w, http.StatusBadRequest, "informe a descrição do produto")
		return dominio.Produto{}, false
	}
	un := p.Unidade
	if un != dominio.UND && un != dominio.KG {
		erroJSON(w, http.StatusBadRequest, "a unidade deve ser UND ou KG")
		return dominio.Produto{}, false
	}
	// Confirmar um produto é ato humano: o padrão de um cadastro manual é
	// confirmado, mas a tela pode marcar como pendente de confirmação.
	confirmado := true
	if p.Confirmado != nil {
		confirmado = *p.Confirmado
	}
	return dominio.Produto{
		ID: id, Codigo: strings.TrimSpace(p.Codigo), Descricao: desc,
		Unidade: un, PrecoRef: p.PrecoRef, Confirmado: confirmado,
	}, true
}
