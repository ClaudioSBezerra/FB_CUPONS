package exportxlsx

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

func valor(t *testing.T, s string) dominio.Valor {
	t.Helper()
	v, err := dominio.ParseValor(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func qtd(t *testing.T, s string) dominio.Qtd {
	t.Helper()
	q, err := dominio.ParseQtd(s)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// dadosDeTeste reproduz um recorte dos dados reais de outubro/2026, com os
// totais do critério de aceite do CLAUDE.md §10.
func dadosDeTeste(t *testing.T) Dados {
	t.Helper()
	return Dados{
		Mes: "2026-10",
		Cobranca: store.Cobranca{
			Mes: "2026-10",
			Linhas: []store.LinhaCobranca{
				{Mes: "2026-10", ClienteID: 2, Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302",
					Codigo: "20771", Produto: "PÃO FRANCÊS 50G", Unidade: dominio.UND,
					QtdTotal: qtd(t, "2480"), NumCupons: 10, ValorTotal: valor(t, "2.480,00")},
				{Mes: "2026-10", ClienteID: 2, Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302",
					Codigo: "288", Produto: "PÃO DE QUEIJO", Unidade: dominio.KG,
					QtdTotal: qtd(t, "1,340"), NumCupons: 1, ValorTotal: valor(t, "61,63")},
				{Mes: "2026-10", ClienteID: 1, Cliente: "MATERNIDADE CÉLIA CÂMARA", CNPJCliente: "37678845000736",
					Codigo: "22121", Produto: "PÃO DOCE 50G", Unidade: dominio.UND,
					QtdTotal: qtd(t, "2330"), NumCupons: 16, ValorTotal: valor(t, "3.495,00")},
			},
			Subtotais: []store.Subtotal{
				{ClienteID: 2, Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302",
					NumCupons: 12, Total: valor(t, "11.500,46")},
				{ClienteID: 1, Cliente: "MATERNIDADE CÉLIA CÂMARA", CNPJCliente: "37678845000736",
					NumCupons: 18, Total: valor(t, "7.225,77")},
			},
			Total:     valor(t, "18.726,23"),
			Pendentes: 5,
		},
		Itens: []store.ItemExportacao{
			{Status: dominio.StatusConfirmado, Data: dominio.NovaData(2026, 10, 16), Numero: "1340905",
				Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302", Codigo: "20771",
				Produto: "PÃO FRANCÊS 50G", Unidade: dominio.UND,
				Qtd: qtd(t, "230"), Vun: valor(t, "1,00"), Vtot: valor(t, "230,00")},
			{Status: dominio.StatusPendente, Data: dominio.NovaData(2026, 10, 24), Numero: "1180221",
				Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302", Codigo: "1645",
				Produto: "PRODUTO 1645 (nome ilegível na foto)", Unidade: dominio.UND,
				Qtd: 0, Vun: 0, Vtot: 0},
		},
		Cupons: []store.CupomExportacao{
			{Status: dominio.StatusConfirmado, Data: dominio.NovaData(2026, 10, 16), Numero: "1340905",
				Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302", NumItens: 7,
				Total: valor(t, "1.010,73")},
			{Status: dominio.StatusPendente, Data: dominio.NovaData(2026, 10, 24), Numero: "1180221",
				Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302", NumItens: 7,
				Total: 0, Obs: "3º item e total ilegíveis."},
			// Cupom sem data: não pode virar 30/12/1899 na planilha.
			{Status: dominio.StatusPendente, Numero: "9999",
				Cliente: "DONA ÍRIS", CNPJCliente: "37678845000302", NumItens: 1, Total: valor(t, "10,00")},
		},
	}
}

func gerarEAbrir(t *testing.T, d Dados) *excelize.File {
	t.Helper()
	var buf bytes.Buffer
	if err := Gerar(&buf, d); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestGerarCriaAsTresAbas(t *testing.T) {
	f := gerarEAbrir(t, dadosDeTeste(t))

	abas := f.GetSheetList()
	quer := map[string]bool{abaCobranca: false, abaItens: false, abaCupons: false}
	for _, a := range abas {
		if _, ok := quer[a]; ok {
			quer[a] = true
		}
		if a == "Sheet1" {
			t.Error("a aba padrão Sheet1 não deveria sobrar na planilha")
		}
	}
	for nome, achou := range quer {
		if !achou {
			t.Errorf("falta a aba %q (abas: %v)", nome, abas)
		}
	}
}

func TestValoresSaoNumerosNaoTexto(t *testing.T) {
	// Se o valor virar texto, o Excel não soma a coluna — foi um dos
	// problemas do protótipo.
	f := gerarEAbrir(t, dadosDeTeste(t))

	linhas, err := f.GetRows(abaCobranca)
	if err != nil {
		t.Fatal(err)
	}
	if len(linhas) < 2 {
		t.Fatalf("a aba Cobrança veio com %d linhas", len(linhas))
	}

	// Primeira linha de dados: Dona Íris / PÃO FRANCÊS 50G.
	// Célula numérica pura não carrega atributo de tipo no XML, então o que
	// importa é NÃO ser string: texto viraria InlineString/SharedString e o
	// Excel não somaria a coluna.
	tipo, err := f.GetCellType(abaCobranca, "I2")
	if err != nil {
		t.Fatal(err)
	}
	if tipo == excelize.CellTypeInlineString || tipo == excelize.CellTypeSharedString {
		t.Errorf("valor total em I2 foi gravado como texto (%v); o Excel não somaria a coluna", tipo)
	}

	bruto, err := f.GetCellValue(abaCobranca, "I2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	if bruto != "2480" {
		t.Errorf("valor bruto em I2 = %q, quer 2480", bruto)
	}

	// Quantidade fracionária de KG precisa sobreviver com 3 decimais.
	bruto, err = f.GetCellValue(abaCobranca, "G3", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	if bruto != "1.34" {
		t.Errorf("quantidade em G3 = %q, quer 1.34", bruto)
	}

	// E o valor com centavos tem de chegar exato, sem ruído de float.
	bruto, err = f.GetCellValue(abaCobranca, "I3", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	if bruto != "61.63" {
		t.Errorf("valor em I3 = %q, quer 61.63", bruto)
	}
}

func TestSomaDaPlanilhaFechaComOCriterioDeAceite(t *testing.T) {
	d := dadosDeTeste(t)
	f := gerarEAbrir(t, d)

	linhas, err := f.GetRows(abaCobranca, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}

	var achouTotalGeral bool
	for _, l := range linhas {
		if len(l) > 0 && l[0] == "TOTAL GERAL (cupons confirmados)" {
			achouTotalGeral = true
			if len(l) < 9 {
				t.Fatalf("a linha de total geral tem %d colunas", len(l))
			}
			if l[8] != "18726.23" {
				t.Errorf("total geral = %q, quer 18726.23", l[8])
			}
		}
	}
	if !achouTotalGeral {
		t.Error("a planilha não tem linha de total geral")
	}

	// Os subtotais por cliente são os do critério de aceite.
	querSubtotais := map[string]string{
		"DONA ÍRIS":                "11500.46",
		"MATERNIDADE CÉLIA CÂMARA": "7225.77",
	}
	for _, l := range linhas {
		if len(l) >= 9 && l[0] == "SUBTOTAL" {
			if quer, ok := querSubtotais[l[1]]; ok {
				if l[8] != quer {
					t.Errorf("subtotal de %s = %q, quer %q", l[1], l[8], quer)
				}
				delete(querSubtotais, l[1])
			}
		}
	}
	for cliente := range querSubtotais {
		t.Errorf("faltou o subtotal de %s", cliente)
	}
}

func TestDatasSaoDataPuraSemFuso(t *testing.T) {
	// No protótipo um bug de fuso fez as datas aparecerem um dia antes.
	f := gerarEAbrir(t, dadosDeTeste(t))

	valor, err := f.GetCellValue(abaCupons, "C2")
	if err != nil {
		t.Fatal(err)
	}
	if valor != "16/10/2026" {
		t.Errorf("data em C2 = %q, quer 16/10/2026", valor)
	}

	// Confere pelo serial também, para garantir que não houve deslocamento.
	bruto, err := f.GetCellValue(abaCupons, "C2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	serial, err := excelize.ExcelDateToTime(parseFloat(t, bruto), false)
	if err != nil {
		t.Fatal(err)
	}
	quer := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if !serial.Equal(quer) {
		t.Errorf("serial em C2 virou %v, quer %v", serial, quer)
	}
}

func TestCupomSemDataFicaEmBranco(t *testing.T) {
	f := gerarEAbrir(t, dadosDeTeste(t))

	// Terceira linha de dados (linha 4) é o cupom sem data.
	valor, err := f.GetCellValue(abaCupons, "C4")
	if err != nil {
		t.Fatal(err)
	}
	if valor != "" {
		t.Errorf("cupom sem data mostrou %q; quer célula em branco, não 30/12/1899", valor)
	}
}

func TestAbaCuponsMarcaOQueFaltaRevisar(t *testing.T) {
	f := gerarEAbrir(t, dadosDeTeste(t))

	situacao, err := f.GetCellValue(abaCupons, "A3")
	if err != nil {
		t.Fatal(err)
	}
	if situacao != "A revisar" {
		t.Errorf("situação em A3 = %q, quer \"A revisar\"", situacao)
	}
}

func TestAvisoDePendentesNaCobranca(t *testing.T) {
	// A planilha precisa dizer que há cupons fora do total; o operador não
	// pode descobrir isso só na tela.
	f := gerarEAbrir(t, dadosDeTeste(t))

	linhas, err := f.GetRows(abaCobranca)
	if err != nil {
		t.Fatal(err)
	}
	achou := false
	for _, l := range linhas {
		if len(l) > 0 && contem(l[0], "pendente") {
			achou = true
		}
	}
	if !achou {
		t.Error("a aba Cobrança não avisa sobre os cupons pendentes fora do total")
	}
}

func TestSemPendentesNaoAvisa(t *testing.T) {
	d := dadosDeTeste(t)
	d.Cobranca.Pendentes = 0
	f := gerarEAbrir(t, d)

	linhas, err := f.GetRows(abaCobranca)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range linhas {
		if len(l) > 0 && contem(l[0], "pendente") {
			t.Errorf("avisou sobre pendentes sem haver nenhum: %q", l[0])
		}
	}
}

func TestNomeArquivo(t *testing.T) {
	casos := []struct {
		mes, cliente, quer string
	}{
		{"2026-10", "", "cobranca-2026-10.xlsx"},
		{"2026-10", "DONA ÍRIS", "cobranca-2026-10-DONA-RIS.xlsx"},
		{"2026-10", "MATERNIDADE CÉLIA CÂMARA", "cobranca-2026-10-MATERNIDADE-CLIA-CMARA.xlsx"},
	}
	for _, c := range casos {
		if got := NomeArquivo(c.mes, c.cliente); got != c.quer {
			t.Errorf("NomeArquivo(%q,%q) = %q, quer %q", c.mes, c.cliente, got, c.quer)
		}
	}
}

func TestCobrancaVaziaNaoQuebra(t *testing.T) {
	var buf bytes.Buffer
	err := Gerar(&buf, Dados{Mes: "2026-11", Cobranca: store.Cobranca{Mes: "2026-11"}})
	if err != nil {
		t.Fatalf("mês sem cupom nenhum deveria gerar planilha vazia: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("não gerou arquivo")
	}
}

func contem(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()
	var f float64
	var neg bool
	i := 0
	if i < len(s) && s[i] == '-' {
		neg = true
		i++
	}
	for ; i < len(s) && s[i] != '.'; i++ {
		f = f*10 + float64(s[i]-'0')
	}
	if i < len(s) && s[i] == '.' {
		escala := 0.1
		for i++; i < len(s); i++ {
			f += float64(s[i]-'0') * escala
			escala /= 10
		}
	}
	if neg {
		f = -f
	}
	return f
}
