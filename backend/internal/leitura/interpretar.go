package leitura

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// respostaModelo espelha o SchemaJSON. Todo campo é ponteiro porque null
// significa "ilegível", que é informação — diferente de zero.
type respostaModelo struct {
	Numero      *string      `json:"numero"`
	Data        *string      `json:"data"`
	CNPJCliente *string      `json:"cnpj_cliente"`
	Cliente     *string      `json:"cliente"`
	Total       *float64     `json:"total"`
	Itens       []itemModelo `json:"itens"`
	Incertos    []string     `json:"incertos"`
	Avisos      []string     `json:"avisos"`
}

type itemModelo struct {
	Cod      *string  `json:"cod"`
	Prod     *string  `json:"prod"`
	Un       *string  `json:"un"`
	Qtd      *float64 `json:"qtd"`
	Vun      *float64 `json:"vun"`
	Vtot     *float64 `json:"vtot"`
	Incertos []string `json:"incertos"`
}

// mapaCamposCupom traduz os nomes do schema para os campos do domínio.
var mapaCamposCupom = map[string]string{
	"numero":       dominio.CampoNumero,
	"data":         dominio.CampoData,
	"cnpj_cliente": dominio.CampoCNPJCli,
	"cliente":      dominio.CampoCliente,
	"total":        dominio.CampoTotal,
}

var mapaCamposItem = map[string]string{
	"cod":  dominio.CampoCod,
	"prod": dominio.CampoProd,
	"un":   dominio.CampoProd, // unidade divergente é problema de produto
	"qtd":  dominio.CampoQtd,
	"vun":  dominio.CampoVun,
	"vtot": dominio.CampoVtot,
}

// Interpretar converte a resposta JSON do modelo em um Cupom do domínio.
//
// Campos nulos viram zero/vazio e entram em Incertos — nunca em um palpite.
// A validação de negócio (pacote validacao) roda depois, sobre este cupom.
func Interpretar(bruto []byte, fonte string) (dominio.Cupom, error) {
	limpo := extrairJSON(bruto)

	var r respostaModelo
	dec := json.NewDecoder(strings.NewReader(limpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		// Campo extra não é motivo para perder a leitura inteira: tenta de
		// novo sem a restrição e avisa.
		var r2 respostaModelo
		if err2 := json.Unmarshal([]byte(limpo), &r2); err2 != nil {
			return dominio.Cupom{}, fmt.Errorf("a resposta da leitura não é um JSON válido: %w", err2)
		}
		r = r2
		r.Avisos = append(r.Avisos, "A resposta da leitura trouxe campos fora do schema; foram ignorados.")
	}

	c := dominio.Cupom{
		CNPJEmitente: dominio.CNPJEmitente,
		Status:       dominio.StatusPendente,
		Fonte:        fonte,
		Avisos:       limparTextos(r.Avisos),
	}

	if r.Numero != nil {
		c.Numero = strings.TrimSpace(*r.Numero)
	}
	if r.Cliente != nil {
		c.Cliente = strings.TrimSpace(*r.Cliente)
	}
	if r.CNPJCliente != nil {
		c.CNPJCliente = dominio.SoDigitos(*r.CNPJCliente)
	}
	if r.Total != nil {
		c.Total = dominio.ValorDeReais(*r.Total)
	}
	if r.Data != nil && strings.TrimSpace(*r.Data) != "" {
		d, err := dominio.ParseData(*r.Data)
		if err != nil {
			c.MarcaIncerto(dominio.CampoData)
			c.Avisa(fmt.Sprintf("A data lida (%q) não está em um formato reconhecido.", *r.Data))
		} else {
			c.Data = d
		}
	}

	// Campos nulos são ilegíveis por definição, mesmo que o modelo esqueça
	// de listá-los em "incertos".
	if r.Numero == nil || c.Numero == "" {
		c.MarcaIncerto(dominio.CampoNumero)
	}
	if r.Data == nil || c.Data.Zero() {
		c.MarcaIncerto(dominio.CampoData)
	}
	if r.CNPJCliente == nil || c.CNPJCliente == "" {
		c.MarcaIncerto(dominio.CampoCNPJCli)
	}
	if r.Cliente == nil || c.Cliente == "" {
		c.MarcaIncerto(dominio.CampoCliente)
	}
	if r.Total == nil {
		c.MarcaIncerto(dominio.CampoTotal)
	}
	for _, nome := range r.Incertos {
		if campo, ok := mapaCamposCupom[strings.TrimSpace(nome)]; ok {
			c.MarcaIncerto(campo)
		}
	}

	for i, im := range r.Itens {
		c.Itens = append(c.Itens, interpretarItem(i+1, im))
	}

	return c, nil
}

func interpretarItem(ordem int, im itemModelo) dominio.Item {
	it := dominio.Item{Ordem: ordem}

	if im.Cod != nil {
		it.Codigo = strings.TrimSpace(*im.Cod)
	}
	if im.Prod != nil {
		it.Produto = strings.TrimSpace(*im.Prod)
	}
	if im.Un != nil {
		switch strings.ToUpper(strings.TrimSpace(*im.Un)) {
		case "KG":
			it.Unidade = dominio.KG
		case "UND", "UN", "UNID":
			it.Unidade = dominio.UND
		}
	}
	if im.Qtd != nil {
		it.Qtd = dominio.QtdDeUnidades(*im.Qtd)
	}
	if im.Vun != nil {
		it.Vun = dominio.ValorDeReais(*im.Vun)
	}
	if im.Vtot != nil {
		it.Vtot = dominio.ValorDeReais(*im.Vtot)
	}

	if im.Cod == nil || it.Codigo == "" {
		it.MarcaIncerto(dominio.CampoCod)
	}
	if im.Prod == nil || it.Produto == "" {
		it.MarcaIncerto(dominio.CampoProd)
	}
	if im.Qtd == nil {
		it.MarcaIncerto(dominio.CampoQtd)
	}
	if im.Vun == nil {
		it.MarcaIncerto(dominio.CampoVun)
	}
	if im.Vtot == nil {
		it.MarcaIncerto(dominio.CampoVtot)
	}
	for _, nome := range im.Incertos {
		if campo, ok := mapaCamposItem[strings.TrimSpace(nome)]; ok {
			it.MarcaIncerto(campo)
		}
	}

	// Produto sem nome legível recebe um rótulo explícito, no mesmo padrão
	// dos dados do protótipo, para o operador saber o que confirmar.
	if it.Produto == "" {
		if it.Codigo != "" {
			it.Produto = fmt.Sprintf("PRODUTO %s (nome ilegível na foto)", it.Codigo)
		} else {
			it.Produto = "PRODUTO (nome ilegível na foto)"
		}
	}

	return it
}

// extrairJSON tolera um modelo que embrulhe o JSON em cerca de markdown ou
// acrescente texto em volta, apesar da instrução e do schema.
func extrairJSON(bruto []byte) string {
	s := strings.TrimSpace(string(bruto))

	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}

	if !strings.HasPrefix(s, "{") {
		if i := strings.Index(s, "{"); i >= 0 {
			if j := strings.LastIndex(s, "}"); j > i {
				s = s[i : j+1]
			}
		}
	}
	return s
}

func limparTextos(xs []string) []string {
	var out []string
	for _, x := range xs {
		if t := strings.TrimSpace(x); t != "" {
			out = append(out, t)
		}
	}
	return out
}
