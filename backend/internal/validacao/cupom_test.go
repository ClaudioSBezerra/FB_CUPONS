package validacao

import (
	"strings"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// catalogoFake implementa Catalogo com o seed do CLAUDE.md §2 (recorte).
type catalogoFake struct{ produtos []dominio.Produto }

func (c catalogoFake) PorCodigo(codigo string) (dominio.Produto, bool) {
	for _, p := range c.produtos {
		if p.Codigo == codigo {
			return p, true
		}
	}
	return dominio.Produto{}, false
}

func (c catalogoFake) Todos() []dominio.Produto { return c.produtos }

func preco(reais string) *dominio.Valor {
	v, err := dominio.ParseValor(reais)
	if err != nil {
		panic(err)
	}
	return &v
}

func catalogoSeed() catalogoFake {
	return catalogoFake{produtos: []dominio.Produto{
		{Codigo: "20771", Descricao: "PÃO FRANCÊS 50G", Unidade: dominio.UND, PrecoRef: preco("1,00"), Confirmado: true},
		{Codigo: "22121", Descricao: "PÃO DOCE 50G", Unidade: dominio.UND, PrecoRef: preco("1,50"), Confirmado: true},
		{Codigo: "2755", Descricao: "ROSCAS 80G", Unidade: dominio.UND, PrecoRef: preco("3,30"), Confirmado: true},
		{Codigo: "1645", Descricao: "ROSCAS 50G", Unidade: dominio.UND, PrecoRef: preco("3,00"), Confirmado: true},
		{Codigo: "3636", Descricao: "PÃO DE FORMA 500G", Unidade: dominio.UND, PrecoRef: preco("9,99"), Confirmado: true},
		{Codigo: "288", Descricao: "PÃO DE QUEIJO", Unidade: dominio.KG, PrecoRef: preco("45,99"), Confirmado: true},
	}}
}

func ctxSeed() Contexto {
	return Contexto{
		Catalogo:       catalogoSeed(),
		CNPJConhecidos: conhecidos(),
		MesReferencia:  "2026-10",
	}
}

func item(ordem int, cod string, qtd, vun, vtot string) dominio.Item {
	return dominio.Item{
		Ordem:  ordem,
		Codigo: cod,
		Qtd:    mustQtd(qtd),
		Vun:    mustValor(vun),
		Vtot:   mustValor(vtot),
	}
}

func mustQtd(s string) dominio.Qtd {
	q, err := dominio.ParseQtd(s)
	if err != nil {
		panic(err)
	}
	return q
}

func mustValor(s string) dominio.Valor {
	v, err := dominio.ParseValor(s)
	if err != nil {
		panic(err)
	}
	return v
}

// cupomBase é um cupom da Maternidade que fecha: 120×1,00 + 110×1,50 = 285,00.
func cupomBase() dominio.Cupom {
	return dominio.Cupom{
		Numero:       "1340923",
		Data:         dominio.NovaData(2026, 10, 16),
		Cliente:      "MATERNIDADE CÉLIA CÂMARA",
		CNPJCliente:  cnpjMaternidade,
		CNPJEmitente: dominio.CNPJEmitente,
		Total:        mustValor("285,00"),
		Itens: []dominio.Item{
			item(1, "20771", "120", "1,00", "120,00"),
			item(2, "22121", "110", "1,50", "165,00"),
		},
	}
}

func TestValidaCupomQueFecha(t *testing.T) {
	c := cupomBase()
	r := Valida(&c, ctxSeed())

	if !r.PodeConfirmar {
		t.Fatalf("cupom correto não pode confirmar: %v", r.Bloqueios)
	}
	if len(c.Avisos) != 0 {
		t.Errorf("cupom correto gerou avisos: %v", c.Avisos)
	}
	// Número e data vão sempre para o olho humano (regra do prompt §5).
	if !c.TemIncerto(dominio.CampoNumero) || !c.TemIncerto(dominio.CampoData) {
		t.Errorf("número e data deveriam entrar em incertos, veio %v", c.Incertos)
	}
	// Descrição e unidade vêm do catálogo.
	if c.Itens[0].Produto != "PÃO FRANCÊS 50G" || c.Itens[0].Unidade != dominio.UND {
		t.Errorf("catálogo não preencheu o item: %+v", c.Itens[0])
	}
}

func TestAritmeticaDoItem(t *testing.T) {
	casos := []struct {
		nome        string
		it          dominio.Item
		querQtd     string
		querVun     string
		querVtot    string
		querIncerto string // campo que deve ficar marcado; "" = nenhum
		querAviso   bool
	}{
		{
			nome:    "fecha exatamente",
			it:      item(1, "20771", "120", "1,00", "120,00"),
			querQtd: "120", querVun: "1,00", querVtot: "120,00",
		},
		{
			nome:    "KG com arredondamento de centavo",
			it:      item(1, "288", "1,340", "45,99", "61,63"), // 61,6266 -> 61,63
			querQtd: "1,340", querVun: "45,99", querVtot: "61,63",
		},
		{
			nome:    "valor total ilegível é deduzido",
			it:      item(1, "2755", "75", "3,30", "0"),
			querQtd: "75", querVun: "3,30", querVtot: "247,50",
			querIncerto: dominio.CampoVtot,
			querAviso:   true,
		},
		{
			nome:    "valor unitário ilegível é deduzido",
			it:      item(1, "2755", "75", "0", "247,50"),
			querQtd: "75", querVun: "3,30", querVtot: "247,50",
			querIncerto: dominio.CampoVun,
			querAviso:   true,
		},
		{
			nome:    "quantidade ilegível é deduzida",
			it:      item(1, "2755", "0", "3,30", "247,50"),
			querQtd: "75", querVun: "3,30", querVtot: "247,50",
			querIncerto: dominio.CampoQtd,
			querAviso:   true,
		},
		{
			nome: "quantidade fracionária é rejeitada em UND",
			// 100,00 / 3,30 = 30,30... não é inteiro: não deduz nada.
			it:      item(1, "2755", "0", "3,30", "100,00"),
			querQtd: "0", querVun: "3,30", querVtot: "100,00",
			querIncerto: dominio.CampoQtd,
			querAviso:   true,
		},
		{
			nome:    "divergência real não é 'corrigida'",
			it:      item(1, "20771", "120", "1,00", "130,00"),
			querQtd: "120", querVun: "1,00", querVtot: "130,00",
			querIncerto: dominio.CampoVtot,
			querAviso:   true,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			c := cupomBase()
			c.Itens = []dominio.Item{caso.it}
			c.Total = 0 // isola a validação do item da validação da soma
			Valida(&c, ctxSeed())

			got := c.Itens[0]
			if got.Qtd != mustQtd(caso.querQtd) {
				t.Errorf("qtd = %s, quer %s", got.Qtd, caso.querQtd)
			}
			if got.Vun != mustValor(caso.querVun) {
				t.Errorf("vun = %s, quer %s", got.Vun, caso.querVun)
			}
			if got.Vtot != mustValor(caso.querVtot) {
				t.Errorf("vtot = %s, quer %s", got.Vtot, caso.querVtot)
			}
			if caso.querIncerto != "" && !got.TemIncerto(caso.querIncerto) {
				t.Errorf("campo %s deveria ser incerto, veio %v", caso.querIncerto, got.Incertos)
			}
			if caso.querAviso && len(c.Avisos) == 0 {
				t.Error("dedução/divergência sem aviso: seria mudança silenciosa")
			}
			if !caso.querAviso {
				for _, a := range c.Avisos {
					if strings.Contains(a, "Item 1") {
						t.Errorf("item correto gerou aviso: %q", a)
					}
				}
			}
		})
	}
}

func TestSomaDoCupom(t *testing.T) {
	t.Run("divergência avisa e bloqueia confirmação", func(t *testing.T) {
		c := cupomBase()
		c.Total = mustValor("300,00") // itens somam 285,00
		r := Valida(&c, ctxSeed())

		if r.PodeConfirmar {
			t.Error("não deveria poder confirmar com soma divergente")
		}
		if !c.TemIncerto(dominio.CampoTotal) {
			t.Error("total deveria ficar incerto")
		}
		if !temAviso(c.Avisos, "difere do total lido") {
			t.Errorf("faltou o aviso da regra 6.3: %v", c.Avisos)
		}
	})

	t.Run("total zerado sugere a soma sem sobrescrever", func(t *testing.T) {
		c := cupomBase()
		c.Total = 0
		r := Valida(&c, ctxSeed())

		if c.Total != 0 {
			t.Errorf("total foi sobrescrito para %s — deve ficar com o operador", c.Total)
		}
		if r.PodeConfirmar {
			t.Error("regra 6.7: total zero sempre fica pendente")
		}
		if !temAviso(c.Avisos, "soma dos itens é 285,00") {
			t.Errorf("faltou sugerir a soma: %v", c.Avisos)
		}
	})

	t.Run("item único incerto recebe o valor deduzido como sugestão", func(t *testing.T) {
		// Caso real da Dona Íris: cupom 1341236, item 2156 ilegível, total
		// impresso 926,98. Os demais itens somam 862,48 -> o item vale 64,50.
		c := dominio.Cupom{
			Numero: "1341236", Data: dominio.NovaData(2026, 10, 17),
			Cliente: "DONA ÍRIS", CNPJCliente: cnpjDonaIris,
			CNPJEmitente: dominio.CNPJEmitente,
			Total:        mustValor("926,98"),
			Itens: []dominio.Item{
				item(1, "20771", "180", "1,00", "180,00"),
				item(2, "22121", "85", "1,50", "127,50"),
				item(3, "3636", "2", "9,99", "19,98"),
				item(4, "1930", "75", "3,30", "247,50"),
				item(5, "2156", "15", "3,70", "55,50"), // leitura duvidosa
				item(6, "2755", "75", "3,30", "247,50"),
				item(7, "1594", "10", "4,00", "40,00"),
			},
		}
		c.Itens[4].MarcaIncerto(dominio.CampoVtot)
		r := Valida(&c, ctxSeed())

		if r.PodeConfirmar {
			t.Error("cupom com soma divergente não pode ser confirmado")
		}
		if !temAviso(c.Avisos, "valer 64,50") {
			t.Errorf("faltou a sugestão de R$ 64,50: %v", c.Avisos)
		}
		// O valor NÃO pode ter sido aplicado automaticamente.
		if c.Itens[4].Vtot != mustValor("55,50") {
			t.Errorf("item 5 foi alterado para %s — a dedução é só sugestão", c.Itens[4].Vtot)
		}
	})

	t.Run("sem itens", func(t *testing.T) {
		c := cupomBase()
		c.Itens = nil
		r := Valida(&c, ctxSeed())
		if r.PodeConfirmar {
			t.Error("cupom sem itens não pode ser confirmado")
		}
	})
}

func TestCatalogo(t *testing.T) {
	t.Run("código fora do catálogo é aceito com aviso", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{item(1, "9999", "10", "2,00", "20,00")}
		c.Total = mustValor("20,00")
		Valida(&c, ctxSeed())

		if c.Itens[0].Codigo != "9999" {
			t.Errorf("código foi alterado para %q", c.Itens[0].Codigo)
		}
		if !c.Itens[0].TemIncerto(dominio.CampoCod) {
			t.Error("código desconhecido deveria ser incerto")
		}
		if !temAviso(c.Avisos, "não está no catálogo") {
			t.Errorf("faltou aviso de código desconhecido: %v", c.Avisos)
		}
	})

	t.Run("código a distância 1 é sugerido, nunca aplicado", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{item(1, "2075", "10", "2,00", "20,00")} // perto de 20771? não; de 2755 sim
		c.Total = mustValor("20,00")
		Valida(&c, ctxSeed())

		if c.Itens[0].Codigo != "2075" {
			t.Errorf("código foi substituído por %q — proibido", c.Itens[0].Codigo)
		}
	})

	t.Run("descrição parcial adota a do catálogo", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{{Ordem: 1, Codigo: "20771", Produto: "PAO FRANCES 50G",
			Qtd: mustQtd("10"), Vun: mustValor("1,00"), Vtot: mustValor("10,00")}}
		c.Total = mustValor("10,00")
		Valida(&c, ctxSeed())

		if c.Itens[0].Produto != "PÃO FRANCÊS 50G" {
			t.Errorf("descrição = %q, quer a do catálogo", c.Itens[0].Produto)
		}
	})

	t.Run("descrição muito diferente é marcada", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{{Ordem: 1, Codigo: "20771", Produto: "BOLO DE CENOURA",
			Qtd: mustQtd("10"), Vun: mustValor("1,00"), Vtot: mustValor("10,00")}}
		c.Total = mustValor("10,00")
		Valida(&c, ctxSeed())

		if !c.Itens[0].TemIncerto(dominio.CampoProd) {
			t.Error("descrição divergente deveria ser incerta")
		}
	})

	t.Run("unidade divergente do catálogo é marcada", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{{Ordem: 1, Codigo: "288", Unidade: dominio.UND,
			Qtd: mustQtd("1"), Vun: mustValor("45,99"), Vtot: mustValor("45,99")}}
		c.Total = mustValor("45,99")
		Valida(&c, ctxSeed())

		if !c.Itens[0].TemIncerto(dominio.CampoProd) {
			t.Error("unidade divergente deveria ser marcada")
		}
	})

	t.Run("item sem código é aceito e avisado", func(t *testing.T) {
		c := cupomBase()
		c.Itens = []dominio.Item{{Ordem: 1, Produto: "PÃO DOCE 25G",
			Qtd: mustQtd("30"), Vun: mustValor("0,60"), Vtot: mustValor("18,00")}}
		c.Total = mustValor("18,00")
		Valida(&c, ctxSeed())

		if !c.Itens[0].TemIncerto(dominio.CampoCod) {
			t.Error("item sem código deveria ter cod incerto")
		}
		if c.Itens[0].Unidade != dominio.UND {
			t.Errorf("unidade padrão = %q, quer UND", c.Itens[0].Unidade)
		}
	})
}

func TestSimilaridade(t *testing.T) {
	casos := []struct {
		a, b   string
		minimo float64
	}{
		{"PÃO FRANCÊS 50G", "PAO FRANCES 50G", 1.0},
		{"PAO FRANCES 50G", "PÃO FRANCÊS 50G", 1.0},
		{"PAO DOCE 50G", "PÃO DOCE 50G", SimilaridadeMinima},
		{"B0L0S 90G", "BOLOS 90G", SimilaridadeMinima},
	}
	for _, c := range casos {
		if got := Similaridade(c.a, c.b); got < c.minimo {
			t.Errorf("Similaridade(%q,%q) = %.2f, quer >= %.2f", c.a, c.b, got, c.minimo)
		}
	}
	if got := Similaridade("PÃO FRANCÊS 50G", "BOLO DE CENOURA"); got >= SimilaridadeMinima {
		t.Errorf("produtos distintos deram %.2f de similaridade", got)
	}
	if got := Similaridade("", "PÃO"); got != 0 {
		t.Errorf("vazio = %.2f, quer 0", got)
	}
}

func TestDescricaoCompativel(t *testing.T) {
	casos := []struct {
		lida, catalogo string
		quer           bool
	}{
		// Leitura truncada no fim da linha: mesmo produto.
		{"ROSCAS 80G", "ROSCAS 80G DIET/INTEGRAL", true},
		{"PÃO DE QUEIJO", "PÃO DE QUEIJO 80G", true},
		// Acento e caixa perdidos na impressão térmica.
		{"PAO FRANCES 50G", "PÃO FRANCÊS 50G", true},
		{"pao doce 50g", "PÃO DOCE 50G", true},
		// Produto claramente diferente: precisa de revisão humana.
		{"BOLO DE CENOURA", "PÃO FRANCÊS 50G", false},
		{"SALGADINHOS VARIADOS 30G", "PETIT GATEAU", false},
		{"", "PÃO FRANCÊS 50G", false},
	}
	for _, c := range casos {
		if got := DescricaoCompativel(c.lida, c.catalogo); got != c.quer {
			t.Errorf("DescricaoCompativel(%q,%q) = %v, quer %v", c.lida, c.catalogo, got, c.quer)
		}
	}
}

func TestLeituraTruncadaAdotaDescricaoDoCatalogo(t *testing.T) {
	// Código 1645 bateu exatamente; a impressão cortou o fim da descrição.
	c := cupomBase()
	c.Itens = []dominio.Item{{Ordem: 1, Codigo: "1645", Produto: "ROSCAS",
		Qtd: mustQtd("50"), Vun: mustValor("3,00"), Vtot: mustValor("150,00")}}
	c.Total = mustValor("150,00")
	Valida(&c, ctxSeed())

	if c.Itens[0].Produto != "ROSCAS 50G" {
		t.Errorf("descrição = %q, quer a do catálogo %q", c.Itens[0].Produto, "ROSCAS 50G")
	}
	if c.Itens[0].TemIncerto(dominio.CampoProd) {
		t.Error("leitura truncada do mesmo produto não deveria virar incerteza")
	}
}

func TestDatas(t *testing.T) {
	casos := []struct {
		nome       string
		data       dominio.Data
		referencia string
		querAviso  bool
	}{
		{"no mês de referência", dominio.NovaData(2026, 10, 16), "2026-10", false},
		{"mês anterior ainda passa", dominio.NovaData(2026, 9, 30), "2026-10", false},
		{"mês seguinte ainda passa", dominio.NovaData(2026, 11, 1), "2026-10", false},
		{"ano errado é suspeito (OCR leu 2025)", dominio.NovaData(2025, 10, 16), "2026-10", true},
		{"mês distante é suspeito", dominio.NovaData(2026, 3, 16), "2026-10", true},
		{"data não existente", dominio.NovaData(2026, 2, 31), "2026-10", true},
		{"data não lida", dominio.Data{}, "2026-10", true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			c := cupomBase()
			c.Data = caso.data
			ctx := ctxSeed()
			ctx.MesReferencia = caso.referencia
			Valida(&c, ctx)

			temDataAviso := temAviso(c.Avisos, "Data") || temAviso(c.Avisos, "data")
			if temDataAviso != caso.querAviso {
				t.Errorf("aviso de data = %v, quer %v (avisos: %v)", temDataAviso, caso.querAviso, c.Avisos)
			}
		})
	}
}

func TestEmitenteDivergenteGeraAviso(t *testing.T) {
	c := cupomBase()
	c.CNPJEmitente = "29220487000107" // 1 dígito de diferença
	Valida(&c, ctxSeed())

	if c.CNPJEmitente != dominio.CNPJEmitente {
		t.Errorf("emitente = %q, quer ajuste para o oficial", c.CNPJEmitente)
	}
	if !temAviso(c.Avisos, "emitente") {
		t.Errorf("ajuste do emitente sem aviso: %v", c.Avisos)
	}
}

func TestConfirmacaoBloqueadaPorCNPJInvalido(t *testing.T) {
	c := cupomBase()
	c.CNPJCliente = "37678845000735"
	r := Valida(&c, ctxSeed())

	if r.PodeConfirmar {
		t.Error("CNPJ inválido deveria bloquear a confirmação")
	}
	if !temBloqueio(r.Bloqueios, "CNPJ do cliente inválido") {
		t.Errorf("bloqueios = %v", r.Bloqueios)
	}
}

func temAviso(avisos []string, sub string) bool {
	for _, a := range avisos {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

func temBloqueio(bloqueios []string, sub string) bool { return temAviso(bloqueios, sub) }
