package leitura

import (
	"strings"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

func TestInterpretarCupomCompleto(t *testing.T) {
	bruto := []byte(`{
	  "numero": "1340923",
	  "data": "2026-10-16",
	  "cnpj_cliente": "37.678.845/0007-36",
	  "cliente": "MATERNIDADE CÉLIA CÂMARA",
	  "total": 435.00,
	  "itens": [
	    {"cod":"20771","prod":"PÃO FRANCÊS 50G","un":"UND","qtd":120,"vun":1.00,"vtot":120.00,"incertos":[]},
	    {"cod":"22121","prod":"PÃO DOCE 50G","un":"UND","qtd":110,"vun":1.50,"vtot":165.00,"incertos":[]},
	    {"cod":"1645","prod":"ROSCAS 50G","un":"UND","qtd":50,"vun":3.00,"vtot":150.00,"incertos":[]}
	  ],
	  "incertos": [],
	  "avisos": []
	}`)

	c, err := Interpretar(bruto, "cupom-outubro.pdf p.2")
	if err != nil {
		t.Fatal(err)
	}

	if c.Numero != "1340923" {
		t.Errorf("numero = %q", c.Numero)
	}
	if c.Data.String() != "2026-10-16" {
		t.Errorf("data = %q", c.Data.String())
	}
	// CNPJ chega formatado do modelo e tem de ser gravado só com dígitos.
	if c.CNPJCliente != "37678845000736" {
		t.Errorf("cnpj = %q, quer só dígitos", c.CNPJCliente)
	}
	if c.Total != dominio.Valor(43500) {
		t.Errorf("total = %s", c.Total)
	}
	if len(c.Itens) != 3 {
		t.Fatalf("itens = %d", len(c.Itens))
	}
	if c.Itens[0].Qtd != dominio.Qtd(120000) || c.Itens[0].Vtot != dominio.Valor(12000) {
		t.Errorf("item 1 = %+v", c.Itens[0])
	}
	if c.Itens[0].Ordem != 1 || c.Itens[2].Ordem != 3 {
		t.Error("a ordem dos itens não foi preservada")
	}
	if c.Status != dominio.StatusPendente {
		t.Errorf("status = %q, toda leitura nasce pendente", c.Status)
	}
	if c.CNPJEmitente != dominio.CNPJEmitente {
		t.Errorf("emitente = %q", c.CNPJEmitente)
	}
	if c.Fonte != "cupom-outubro.pdf p.2" {
		t.Errorf("fonte = %q", c.Fonte)
	}
}

func TestInterpretarCamposNulosViramIncertos(t *testing.T) {
	// É o caso da Dona Íris: cupom apagado, número e total ilegíveis.
	bruto := []byte(`{
	  "numero": null,
	  "data": "2026-10-24",
	  "cnpj_cliente": "37678845000302",
	  "cliente": "DONA ÍRIS",
	  "total": null,
	  "itens": [
	    {"cod":"20771","prod":"PÃO FRANCÊS 50G","un":"UND","qtd":310,"vun":1.00,"vtot":310.00,"incertos":[]},
	    {"cod":"1645","prod":null,"un":"UND","qtd":null,"vun":null,"vtot":null,"incertos":["prod","qtd","vun","vtot"]}
	  ],
	  "incertos": ["numero","total"],
	  "avisos": ["Impressão muito apagada no item 2."]
	}`)

	c, err := Interpretar(bruto, "dona-iris.pdf p.9")
	if err != nil {
		t.Fatal(err)
	}

	if !c.TemIncerto(dominio.CampoNumero) || !c.TemIncerto(dominio.CampoTotal) {
		t.Errorf("incertos = %v, quer numero e total", c.Incertos)
	}
	if c.Total != 0 {
		t.Errorf("total = %s, quer 0 para null", c.Total)
	}
	if c.Numero != "" {
		t.Errorf("numero = %q, quer vazio", c.Numero)
	}

	it := c.Itens[1]
	for _, campo := range []string{dominio.CampoProd, dominio.CampoQtd, dominio.CampoVun, dominio.CampoVtot} {
		if !it.TemIncerto(campo) {
			t.Errorf("item 2: campo %s deveria ser incerto, veio %v", campo, it.Incertos)
		}
	}
	// Produto sem nome recebe rótulo explícito com o código.
	if it.Produto != "PRODUTO 1645 (nome ilegível na foto)" {
		t.Errorf("item 2 produto = %q", it.Produto)
	}
	if len(c.Avisos) != 1 {
		t.Errorf("avisos = %v", c.Avisos)
	}
}

func TestInterpretarSemCodigo(t *testing.T) {
	bruto := []byte(`{"numero":"1","data":"2026-10-01","cnpj_cliente":"37678845000302",
	  "cliente":"DONA ÍRIS","total":10.0,
	  "itens":[{"cod":null,"prod":null,"un":null,"qtd":1,"vun":10.0,"vtot":10.0,"incertos":["cod","prod"]}],
	  "incertos":[],"avisos":[]}`)

	c, err := Interpretar(bruto, "")
	if err != nil {
		t.Fatal(err)
	}
	it := c.Itens[0]
	if it.Codigo != "" {
		t.Errorf("codigo = %q, quer vazio", it.Codigo)
	}
	if it.Produto != "PRODUTO (nome ilegível na foto)" {
		t.Errorf("produto = %q", it.Produto)
	}
	if it.Unidade != "" {
		t.Errorf("unidade = %q, quer vazia para null (o catálogo decide)", it.Unidade)
	}
}

func TestInterpretarToleraCercaMarkdown(t *testing.T) {
	bruto := []byte("```json\n{\"numero\":\"1\",\"data\":\"2026-10-01\",\"cnpj_cliente\":\"37678845000302\"," +
		"\"cliente\":\"DONA ÍRIS\",\"total\":10.0,\"itens\":[],\"incertos\":[],\"avisos\":[]}\n```")

	c, err := Interpretar(bruto, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Numero != "1" {
		t.Errorf("numero = %q", c.Numero)
	}
}

func TestInterpretarToleraTextoEmVolta(t *testing.T) {
	bruto := []byte("Aqui está o JSON:\n{\"numero\":\"1\",\"data\":\"2026-10-01\",\"cnpj_cliente\":\"37678845000302\"," +
		"\"cliente\":\"DONA ÍRIS\",\"total\":10.0,\"itens\":[],\"incertos\":[],\"avisos\":[]}\nQualquer coisa.")

	if _, err := Interpretar(bruto, ""); err != nil {
		t.Fatal(err)
	}
}

func TestInterpretarDataInvalidaNaoPerdeOCupom(t *testing.T) {
	bruto := []byte(`{"numero":"1","data":"16 de outubro","cnpj_cliente":"37678845000302",
	  "cliente":"DONA ÍRIS","total":10.0,"itens":[],"incertos":[],"avisos":[]}`)

	c, err := Interpretar(bruto, "")
	if err != nil {
		t.Fatalf("data malformada não deveria derrubar a leitura: %v", err)
	}
	if !c.TemIncerto(dominio.CampoData) {
		t.Error("data malformada deveria virar incerteza")
	}
	if !temTexto(c.Avisos, "não está em um formato reconhecido") {
		t.Errorf("avisos = %v", c.Avisos)
	}
}

func TestInterpretarJSONQuebrado(t *testing.T) {
	if _, err := Interpretar([]byte("não sou json"), ""); err == nil {
		t.Error("deveria falhar com JSON inválido")
	}
}

func TestInterpretarCampoForaDoSchemaNaoPerdeOCupom(t *testing.T) {
	bruto := []byte(`{"numero":"1","data":"2026-10-01","cnpj_cliente":"37678845000302",
	  "cliente":"DONA ÍRIS","total":10.0,"itens":[],"incertos":[],"avisos":[],
	  "campo_inventado":"x"}`)

	c, err := Interpretar(bruto, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Numero != "1" {
		t.Errorf("numero = %q", c.Numero)
	}
	if !temTexto(c.Avisos, "fora do schema") {
		t.Errorf("avisos = %v", c.Avisos)
	}
}

func TestMontarSistemaEhEstavel(t *testing.T) {
	// O prompt de sistema é cacheado: qualquer instabilidade na ordem
	// invalidaria o cache a cada página e multiplicaria o custo do lote.
	ref := Referencias{
		Produtos: []dominio.Produto{
			{Codigo: "22121", Descricao: "PÃO DOCE 50G", Unidade: dominio.UND},
			{Codigo: "20771", Descricao: "PÃO FRANCÊS 50G", Unidade: dominio.UND},
		},
		Clientes: []dominio.Cliente{
			{Nome: "DONA ÍRIS", CNPJ: "37678845000302"},
			{Nome: "MATERNIDADE CÉLIA CÂMARA", CNPJ: "37678845000736"},
		},
	}
	invertido := Referencias{
		Produtos: []dominio.Produto{ref.Produtos[1], ref.Produtos[0]},
		Clientes: []dominio.Cliente{ref.Clientes[1], ref.Clientes[0]},
	}

	if MontarSistema(ref) != MontarSistema(invertido) {
		t.Error("a ordem de entrada mudou o prompt: o cache seria invalidado a cada página")
	}

	s := MontarSistema(ref)
	for _, esperado := range []string{"29.220.487/0001-08", "20771", "DONA ÍRIS", "NUNCA invente"} {
		if !strings.Contains(s, esperado) {
			t.Errorf("prompt não menciona %q", esperado)
		}
	}
}

func TestSchemaJSONPermiteNulos(t *testing.T) {
	s := SchemaJSON()
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema sem properties")
	}
	for _, campo := range []string{"numero", "data", "cnpj_cliente", "cliente", "total"} {
		p, ok := props[campo].(map[string]any)
		if !ok {
			t.Fatalf("schema sem o campo %s", campo)
		}
		tipos, ok := p["type"].([]any)
		if !ok {
			t.Fatalf("campo %s não é nullable: %v", campo, p["type"])
		}
		temNulo := false
		for _, tp := range tipos {
			if tp == "null" {
				temNulo = true
			}
		}
		if !temNulo {
			t.Errorf("campo %s precisa aceitar null para o modelo poder dizer 'não li'", campo)
		}
	}
}

func temTexto(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}
