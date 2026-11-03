// Package exportxlsx gera a planilha de cobrança mensal.
//
// Dois cuidados herdados do protótipo: datas vão como data pura (um bug de
// fuso fazia aparecerem um dia antes) e valores vão como NÚMERO com formato
// monetário, não como texto — senão o Excel não soma.
package exportxlsx

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

// Dados é tudo o que a planilha precisa.
type Dados struct {
	Mes      string
	Cobranca store.Cobranca
	Itens    []store.ItemExportacao
	Cupons   []store.CupomExportacao
}

const (
	abaCobranca = "Cobrança"
	abaItens    = "Itens"
	abaCupons   = "Cupons"
)

// Gerar escreve o .xlsx no writer.
func Gerar(w io.Writer, d Dados) error {
	f := excelize.NewFile()
	defer f.Close()

	estilos, err := montarEstilos(f)
	if err != nil {
		return err
	}

	if err := abaDeCobranca(f, d, estilos); err != nil {
		return err
	}
	if err := abaDeItens(f, d, estilos); err != nil {
		return err
	}
	if err := abaDeCupons(f, d, estilos); err != nil {
		return err
	}

	// A aba criada por padrão ("Sheet1") não serve.
	if idx, err := f.GetSheetIndex(abaCobranca); err == nil {
		f.SetActiveSheet(idx)
	}
	f.DeleteSheet("Sheet1")

	_, err = f.WriteTo(w)
	return err
}

type estilos struct {
	cabecalho     int
	dinheiro      int
	dinheiroTotal int
	data          int
	quantidade    int
	total         int
	alerta        int
}

func montarEstilos(f *excelize.File) (estilos, error) {
	var e estilos
	var err error

	e.cabecalho, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"44546A"}},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return e, err
	}

	// R$ #.##0,00 — formato monetário brasileiro sobre um número de verdade.
	const fmtDinheiro = `"R$" #,##0.00`
	e.dinheiro, err = f.NewStyle(&excelize.Style{CustomNumFmt: ptr(fmtDinheiro)})
	if err != nil {
		return e, err
	}
	e.dinheiroTotal, err = f.NewStyle(&excelize.Style{
		CustomNumFmt: ptr(fmtDinheiro),
		Font:         &excelize.Font{Bold: true},
	})
	if err != nil {
		return e, err
	}
	e.data, err = f.NewStyle(&excelize.Style{CustomNumFmt: ptr("dd/mm/yyyy")})
	if err != nil {
		return e, err
	}
	e.quantidade, err = f.NewStyle(&excelize.Style{CustomNumFmt: ptr(`#,##0.###`)})
	if err != nil {
		return e, err
	}
	e.total, err = f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E7E6E6"}},
	})
	if err != nil {
		return e, err
	}
	e.alerta, err = f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Color: "9C5700"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFEB9C"}},
	})
	return e, err
}

func ptr[T any](v T) *T { return &v }

func abaDeCobranca(f *excelize.File, d Dados, e estilos) error {
	if _, err := f.NewSheet(abaCobranca); err != nil {
		return err
	}

	cabecalhos := []string{"Mês", "Cliente", "CNPJ", "Código", "Produto", "Un.",
		"Qtd. total", "Nº cupons", "Valor total"}
	if err := escreverCabecalho(f, abaCobranca, cabecalhos, e); err != nil {
		return err
	}

	larguras := map[string]float64{"A": 10, "B": 30, "C": 20, "D": 10, "E": 42, "F": 6, "G": 12, "H": 11, "I": 15}
	for col, l := range larguras {
		if err := f.SetColWidth(abaCobranca, col, col, l); err != nil {
			return err
		}
	}

	linha := 2
	clienteAtual := int64(-1)
	for _, l := range d.Cobranca.Linhas {
		if clienteAtual != -1 && l.ClienteID != clienteAtual {
			if err := escreverSubtotal(f, abaCobranca, &linha, d, clienteAtual, e); err != nil {
				return err
			}
			linha++ // linha em branco entre clientes
		}
		clienteAtual = l.ClienteID

		valores := []any{
			d.Mes, l.Cliente, dominio.FormataCNPJ(l.CNPJCliente), l.Codigo,
			l.Produto, string(l.Unidade),
			l.QtdTotal.Unidades(), l.NumCupons, l.ValorTotal.Reais(),
		}
		if err := escreverLinha(f, abaCobranca, linha, valores); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaCobranca, cel("G", linha), cel("G", linha), e.quantidade); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaCobranca, cel("I", linha), cel("I", linha), e.dinheiro); err != nil {
			return err
		}
		linha++
	}
	if clienteAtual != -1 {
		if err := escreverSubtotal(f, abaCobranca, &linha, d, clienteAtual, e); err != nil {
			return err
		}
		linha++
	}

	// Total geral.
	if err := f.SetCellStr(abaCobranca, cel("A", linha), "TOTAL GERAL (cupons confirmados)"); err != nil {
		return err
	}
	if err := f.SetCellValue(abaCobranca, cel("I", linha), d.Cobranca.Total.Reais()); err != nil {
		return err
	}
	if err := f.SetCellStyle(abaCobranca, cel("A", linha), cel("H", linha), e.total); err != nil {
		return err
	}
	if err := f.SetCellStyle(abaCobranca, cel("I", linha), cel("I", linha), e.dinheiroTotal); err != nil {
		return err
	}
	linha += 2

	// Aviso do que ficou de fora: a cobrança só leva cupons confirmados, e o
	// operador precisa ver isso na planilha, não só na tela.
	if d.Cobranca.Pendentes > 0 {
		msg := fmt.Sprintf("Atenção: %d cupom(ns) ainda pendente(s) de revisão NÃO entraram nestes totais.",
			d.Cobranca.Pendentes)
		if err := f.SetCellStr(abaCobranca, cel("A", linha), msg); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaCobranca, cel("A", linha), cel("I", linha), e.alerta); err != nil {
			return err
		}
	}

	return f.SetPanes(abaCobranca, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1,
		TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

func escreverSubtotal(f *excelize.File, aba string, linha *int, d Dados, clienteID int64, e estilos) error {
	for _, st := range d.Cobranca.Subtotais {
		if st.ClienteID != clienteID {
			continue
		}
		if err := f.SetCellStr(aba, cel("A", *linha), "SUBTOTAL"); err != nil {
			return err
		}
		if err := f.SetCellStr(aba, cel("B", *linha), st.Cliente); err != nil {
			return err
		}
		if err := f.SetCellStr(aba, cel("C", *linha), dominio.FormataCNPJ(st.CNPJCliente)); err != nil {
			return err
		}
		if err := f.SetCellInt(aba, cel("H", *linha), int64(st.NumCupons)); err != nil {
			return err
		}
		if err := f.SetCellValue(aba, cel("I", *linha), st.Total.Reais()); err != nil {
			return err
		}
		if err := f.SetCellStyle(aba, cel("A", *linha), cel("H", *linha), e.total); err != nil {
			return err
		}
		if err := f.SetCellStyle(aba, cel("I", *linha), cel("I", *linha), e.dinheiroTotal); err != nil {
			return err
		}
		*linha++
		return nil
	}
	return nil
}

func abaDeItens(f *excelize.File, d Dados, e estilos) error {
	if _, err := f.NewSheet(abaItens); err != nil {
		return err
	}
	cabecalhos := []string{"Situação", "Cupom", "Data", "Cliente", "CNPJ", "Código",
		"Produto", "Un.", "Qtd.", "Valor unit.", "Valor total"}
	if err := escreverCabecalho(f, abaItens, cabecalhos, e); err != nil {
		return err
	}
	larguras := map[string]float64{"A": 12, "B": 12, "C": 12, "D": 30, "E": 20, "F": 10,
		"G": 42, "H": 6, "I": 10, "J": 13, "K": 14}
	for col, l := range larguras {
		if err := f.SetColWidth(abaItens, col, col, l); err != nil {
			return err
		}
	}

	for i, it := range d.Itens {
		linha := i + 2
		if err := escreverLinha(f, abaItens, linha, []any{
			rotuloStatus(it.Status), it.Numero, nil, it.Cliente,
			dominio.FormataCNPJ(it.CNPJCliente), it.Codigo, it.Produto, string(it.Unidade),
			it.Qtd.Unidades(), it.Vun.Reais(), it.Vtot.Reais(),
		}); err != nil {
			return err
		}
		if err := escreverData(f, abaItens, cel("C", linha), it.Data, e); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaItens, cel("I", linha), cel("I", linha), e.quantidade); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaItens, cel("J", linha), cel("K", linha), e.dinheiro); err != nil {
			return err
		}
	}

	return f.SetPanes(abaItens, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

func abaDeCupons(f *excelize.File, d Dados, e estilos) error {
	if _, err := f.NewSheet(abaCupons); err != nil {
		return err
	}
	cabecalhos := []string{"Situação", "Cupom", "Data", "Cliente", "CNPJ", "Itens",
		"Total do cupom", "Observação"}
	if err := escreverCabecalho(f, abaCupons, cabecalhos, e); err != nil {
		return err
	}
	larguras := map[string]float64{"A": 12, "B": 12, "C": 12, "D": 30, "E": 20, "F": 7,
		"G": 16, "H": 70}
	for col, l := range larguras {
		if err := f.SetColWidth(abaCupons, col, col, l); err != nil {
			return err
		}
	}

	for i, c := range d.Cupons {
		linha := i + 2
		if err := escreverLinha(f, abaCupons, linha, []any{
			rotuloStatus(c.Status), c.Numero, nil, c.Cliente,
			dominio.FormataCNPJ(c.CNPJCliente), c.NumItens, c.Total.Reais(), c.Obs,
		}); err != nil {
			return err
		}
		if err := escreverData(f, abaCupons, cel("C", linha), c.Data, e); err != nil {
			return err
		}
		if err := f.SetCellStyle(abaCupons, cel("G", linha), cel("G", linha), e.dinheiro); err != nil {
			return err
		}
		if c.Status != dominio.StatusConfirmado {
			if err := f.SetCellStyle(abaCupons, cel("A", linha), cel("A", linha), e.alerta); err != nil {
				return err
			}
		}
	}

	return f.SetPanes(abaCupons, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

func escreverCabecalho(f *excelize.File, aba string, cabecalhos []string, e estilos) error {
	for i, h := range cabecalhos {
		c, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellStr(aba, c, h); err != nil {
			return err
		}
	}
	fim, err := excelize.CoordinatesToCellName(len(cabecalhos), 1)
	if err != nil {
		return err
	}
	if err := f.SetRowHeight(aba, 1, 28); err != nil {
		return err
	}
	return f.SetCellStyle(aba, "A1", fim, e.cabecalho)
}

func escreverLinha(f *excelize.File, aba string, linha int, valores []any) error {
	for i, v := range valores {
		if v == nil {
			continue // célula preenchida separadamente (data)
		}
		c, err := excelize.CoordinatesToCellName(i+1, linha)
		if err != nil {
			return err
		}
		switch t := v.(type) {
		case string:
			if err := f.SetCellStr(aba, c, t); err != nil {
				return err
			}
		case int:
			if err := f.SetCellInt(aba, c, int64(t)); err != nil {
				return err
			}
		case float64:
			// SetCellValue grava a menor representação exata do número; a
			// aparência fica a cargo do formato da célula.
			if err := f.SetCellValue(aba, c, t); err != nil {
				return err
			}
		default:
			if err := f.SetCellValue(aba, c, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// escreverData grava a data como data pura. Data zero fica em branco em vez
// de virar 30/12/1899.
func escreverData(f *excelize.File, aba, celula string, d dominio.Data, e estilos) error {
	if d.Zero() {
		return nil
	}
	if err := f.SetCellValue(aba, celula, d.Time()); err != nil {
		return err
	}
	return f.SetCellStyle(aba, celula, celula, e.data)
}

func rotuloStatus(s dominio.Status) string {
	switch s {
	case dominio.StatusConfirmado:
		return "Confirmado"
	case dominio.StatusRejeitado:
		return "Rejeitado"
	default:
		return "A revisar"
	}
}

func cel(coluna string, linha int) string { return fmt.Sprintf("%s%d", coluna, linha) }

// NomeArquivo monta "cobranca-AAAA-MM[-cliente].xlsx".
func NomeArquivo(mes, cliente string) string {
	if cliente == "" {
		return fmt.Sprintf("cobranca-%s.xlsx", mes)
	}
	return fmt.Sprintf("cobranca-%s-%s.xlsx", mes, sanitizar(cliente))
}

func sanitizar(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ', r == '-', r == '_':
			out = append(out, '-')
		}
	}
	return string(out)
}
