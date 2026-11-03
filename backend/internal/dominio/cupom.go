package dominio

import (
	"strings"
	"time"
)

// CNPJEmitente é o CNPJ da ITD Padaria e Confeitaria Eireli – ME
// (STRAVAGANZA), só dígitos.
const CNPJEmitente = "29220487000108"

// NomeEmitente é a razão social curta impressa nos cupons.
const NomeEmitente = "STRAVAGANZA"

// Status de um cupom no fluxo de revisão.
type Status string

const (
	StatusPendente   Status = "pendente"
	StatusConfirmado Status = "confirmado"
	StatusRejeitado  Status = "rejeitado"
)

// Unidade de medida de um item.
type Unidade string

const (
	UND Unidade = "UND"
	KG  Unidade = "KG"
)

// Campos que podem ser marcados como incertos (amarelos na revisão).
const (
	CampoData    = "data"
	CampoNumero  = "numero"
	CampoTotal   = "total"
	CampoCNPJCli = "cnpjCli"
	CampoCliente = "cli"
	CampoCod     = "cod"
	CampoProd    = "prod"
	CampoQtd     = "qtd"
	CampoVun     = "vun"
	CampoVtot    = "vtot"
)

// Data é uma data pura, sem fuso — no protótipo um bug de timezone fez datas
// aparecerem um dia antes. Serializa sempre como "AAAA-MM-DD".
type Data struct {
	Ano, Mes, Dia int
}

// NovaData monta uma Data a partir de componentes.
func NovaData(ano, mes, dia int) Data { return Data{ano, mes, dia} }

// DataDeTime extrai a data de um time.Time ignorando o fuso.
func DataDeTime(t time.Time) Data {
	return Data{t.Year(), int(t.Month()), t.Day()}
}

// Time converte para time.Time em UTC à meia-noite. Usado só na fronteira
// com o driver do Postgres e com o excelize.
func (d Data) Time() time.Time {
	return time.Date(d.Ano, time.Month(d.Mes), d.Dia, 0, 0, 0, 0, time.UTC)
}

// Zero indica data não preenchida.
func (d Data) Zero() bool { return d.Ano == 0 && d.Mes == 0 && d.Dia == 0 }

// String devolve "AAAA-MM-DD".
func (d Data) String() string {
	if d.Zero() {
		return ""
	}
	return d.Time().Format("2006-01-02")
}

// Formatada devolve "DD/MM/AAAA", como impresso no cupom.
func (d Data) Formatada() string {
	if d.Zero() {
		return ""
	}
	return d.Time().Format("02/01/2006")
}

// Mes devolve a competência "AAAA-MM".
func (d Data) Competencia() string {
	if d.Zero() {
		return ""
	}
	return d.Time().Format("2006-01")
}

// Valida confere se a data existe no calendário (31/02 é rejeitado).
func (d Data) Valida() bool {
	if d.Zero() {
		return false
	}
	t := d.Time()
	return t.Year() == d.Ano && int(t.Month()) == d.Mes && t.Day() == d.Dia
}

func (d Data) MarshalJSON() ([]byte, error) {
	if d.Zero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Data) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*d = Data{}
		return nil
	}
	p, err := ParseData(s)
	if err != nil {
		return err
	}
	*d = p
	return nil
}

// ParseData aceita "AAAA-MM-DD" e "DD/MM/AAAA".
func ParseData(s string) (Data, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return DataDeTime(t), nil
		}
	}
	return Data{}, &ErroCampo{Campo: CampoData, Msg: "data " + s + " não está em DD/MM/AAAA nem AAAA-MM-DD"}
}

// ErroCampo é um erro atrelado a um campo do cupom.
type ErroCampo struct {
	Campo string
	Msg   string
}

func (e *ErroCampo) Error() string { return e.Campo + ": " + e.Msg }

// Cliente é o destinatário da cobrança.
type Cliente struct {
	ID    int64  `json:"id"`
	Nome  string `json:"nome"`
	CNPJ  string `json:"cnpj"` // só dígitos
	Ativo bool   `json:"ativo"`
	Obs   string `json:"obs,omitempty"`
}

// Produto é uma linha do catálogo. O preço é só referência de validação;
// o preço oficial é o impresso no cupom.
type Produto struct {
	ID         int64   `json:"id"`
	Codigo     string  `json:"codigo"` // vazio = produto sem código legível
	Descricao  string  `json:"descricao"`
	Unidade    Unidade `json:"unidade"`
	PrecoRef   *Valor  `json:"preco_ref"`
	Confirmado bool    `json:"confirmado"` // false = nome ilegível, a confirmar
}

// Item é uma linha de um cupom.
type Item struct {
	ID       int64    `json:"id,omitempty"`
	Ordem    int      `json:"ordem"`
	Codigo   string   `json:"cod"`
	Produto  string   `json:"prod"`
	Unidade  Unidade  `json:"un"`
	Qtd      Qtd      `json:"qtd"`
	Vun      Valor    `json:"vun"`
	Vtot     Valor    `json:"vtot"`
	Incertos []string `json:"incertos"`
}

// Cupom é um cupom fiscal térmico lido de uma página.
type Cupom struct {
	ID           int64     `json:"id,omitempty"`
	Numero       string    `json:"numero"`
	Data         Data      `json:"data"`
	ClienteID    *int64    `json:"cliente_id"`
	Cliente      string    `json:"cliente"`
	CNPJCliente  string    `json:"cnpj_cliente"` // só dígitos, como lido/confirmado
	CNPJEmitente string    `json:"cnpj_emitente"`
	Total        Valor     `json:"total"`
	Status       Status    `json:"status"`
	Itens        []Item    `json:"itens"`
	Incertos     []string  `json:"incertos"`
	Avisos       []string  `json:"avisos"`
	Fonte        string    `json:"fonte"` // ex.: "cupom-outubro.pdf p.7"
	Obs          string    `json:"obs"`
	FotoID       *int64    `json:"foto_id"`
	CriadoEm     time.Time `json:"criado_em,omitempty"`
}

// SomaItens soma o valor total dos itens.
func (c *Cupom) SomaItens() Valor {
	var s Valor
	for _, it := range c.Itens {
		s += it.Vtot
	}
	return s
}

// TemIncerto informa se o campo está na lista de incertos do cupom.
func (c *Cupom) TemIncerto(campo string) bool { return contem(c.Incertos, campo) }

// MarcaIncerto adiciona o campo aos incertos, sem duplicar.
func (c *Cupom) MarcaIncerto(campos ...string) {
	for _, campo := range campos {
		if !contem(c.Incertos, campo) {
			c.Incertos = append(c.Incertos, campo)
		}
	}
}

// Avisa acrescenta um aviso, sem duplicar.
func (c *Cupom) Avisa(msgs ...string) {
	for _, m := range msgs {
		if !contem(c.Avisos, m) {
			c.Avisos = append(c.Avisos, m)
		}
	}
}

// TemIncerto informa se o campo está na lista de incertos do item.
func (i *Item) TemIncerto(campo string) bool { return contem(i.Incertos, campo) }

// MarcaIncerto adiciona o campo aos incertos do item, sem duplicar.
func (i *Item) MarcaIncerto(campos ...string) {
	for _, campo := range campos {
		if !contem(i.Incertos, campo) {
			i.Incertos = append(i.Incertos, campo)
		}
	}
}

func contem(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// SoDigitos remove tudo que não é dígito — usado para CNPJ.
func SoDigitos(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// FormataCNPJ devolve 00.000.000/0000-00; se não tiver 14 dígitos, devolve
// a entrada inalterada.
func FormataCNPJ(cnpj string) string {
	d := SoDigitos(cnpj)
	if len(d) != 14 {
		return cnpj
	}
	return d[0:2] + "." + d[2:5] + "." + d[5:8] + "/" + d[8:12] + "-" + d[12:14]
}
