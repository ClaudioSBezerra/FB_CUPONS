// Package dominio define os tipos do domínio de cupons fiscais.
//
// Valores monetários e quantidades NUNCA usam float64: este é um sistema
// crítico de cobrança e arredondamento binário introduz centavos fantasmas.
// Valor é inteiro em centavos; Qtd é inteiro em milésimos.
package dominio

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Valor é um valor monetário em centavos (espelha NUMERIC(14,2) no banco).
type Valor int64

// Qtd é uma quantidade em milésimos (espelha NUMERIC(14,3) no banco).
// Serve tanto para unidades inteiras (120 UND = 120000) quanto para peso
// fracionado (1,340 KG = 1340).
type Qtd int64

const (
	centavosPorReal  = 100
	milesimosPorUnid = 1000
)

// ValorDeReais converte reais em Valor, arredondando para o centavo mais
// próximo (meio para cima). Use só na fronteira com JSON/float externo.
func ValorDeReais(reais float64) Valor {
	return Valor(math.Round(reais * centavosPorReal))
}

// QtdDeUnidades converte uma quantidade decimal em Qtd (milésimos).
func QtdDeUnidades(un float64) Qtd {
	return Qtd(math.Round(un * milesimosPorUnid))
}

// Reais devolve o valor como float64. Só para exibição e serialização —
// nunca para aritmética.
func (v Valor) Reais() float64 { return float64(v) / centavosPorReal }

// Unidades devolve a quantidade como float64. Só para exibição.
func (q Qtd) Unidades() float64 { return float64(q) / milesimosPorUnid }

// Multiplicar devolve qtd × valor unitário, arredondado ao centavo mais
// próximo (meio para cima) — é a regra do cupom impresso.
func (q Qtd) Multiplicar(unitario Valor) Valor {
	produto := int64(q) * int64(unitario) // centavos × milésimos
	if produto < 0 {
		return Valor(-arredondaDiv(-produto, milesimosPorUnid))
	}
	return Valor(arredondaDiv(produto, milesimosPorUnid))
}

func arredondaDiv(num, den int64) int64 { return (num + den/2) / den }

// Abs devolve o módulo do valor.
func (v Valor) Abs() Valor {
	if v < 0 {
		return -v
	}
	return v
}

// String formata no padrão pt-BR sem o prefixo "R$": 1.234,56.
func (v Valor) String() string {
	neg := v < 0
	if neg {
		v = -v
	}
	inteiros := int64(v) / centavosPorReal
	cents := int64(v) % centavosPorReal
	s := agrupaMilhares(inteiros) + "," + fmt.Sprintf("%02d", cents)
	if neg {
		return "-" + s
	}
	return s
}

// String formata a quantidade sem zeros decimais inúteis: 120 ou 1,340.
func (q Qtd) String() string {
	neg := q < 0
	if neg {
		q = -q
	}
	inteiros := int64(q) / milesimosPorUnid
	frac := int64(q) % milesimosPorUnid
	s := agrupaMilhares(inteiros)
	if frac != 0 {
		s += "," + strings.TrimRight(fmt.Sprintf("%03d", frac), "0")
	}
	if neg {
		return "-" + s
	}
	return s
}

func agrupaMilhares(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	primeiro := len(s) % 3
	if primeiro > 0 {
		b.WriteString(s[:primeiro])
	}
	for i := primeiro; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// MarshalJSON emite número decimal (1234.56), não string — o frontend e o
// Excel precisam de número.
func (v Valor) MarshalJSON() ([]byte, error) {
	return []byte(decimalJSON(int64(v), centavosPorReal, 2)), nil
}

func (v *Valor) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*v = ValorDeReais(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("valor inválido: %s", b)
	}
	p, err := ParseValor(s)
	if err != nil {
		return err
	}
	*v = p
	return nil
}

func (q Qtd) MarshalJSON() ([]byte, error) {
	return []byte(decimalJSON(int64(q), milesimosPorUnid, 3)), nil
}

func (q *Qtd) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*q = QtdDeUnidades(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("quantidade inválida: %s", b)
	}
	p, err := ParseQtd(s)
	if err != nil {
		return err
	}
	*q = p
	return nil
}

// decimalJSON formata um inteiro escalado como decimal JSON, preservando o
// sinal mesmo quando a parte inteira é zero (-0,50 -> "-0.50").
func decimalJSON(n, escala int64, casas int) string {
	sinal := ""
	if n < 0 {
		sinal = "-"
		n = -n
	}
	return fmt.Sprintf("%s%d.%0*d", sinal, n/escala, casas, n%escala)
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ParseValor aceita "1.234,56" (pt-BR), "1234.56" (ponto decimal), "1234"
// e strings com "R$" / espaços.
func ParseValor(s string) (Valor, error) {
	n, err := parseDecimal(s, 2)
	if err != nil {
		return 0, fmt.Errorf("valor %q inválido: %w", s, err)
	}
	return Valor(n), nil
}

// ParseQtd aceita "120", "1,340" e "1.340" com até 3 decimais.
func ParseQtd(s string) (Qtd, error) {
	n, err := parseDecimal(s, 3)
	if err != nil {
		return 0, fmt.Errorf("quantidade %q inválida: %w", s, err)
	}
	return Qtd(n), nil
}

// parseDecimal converte texto decimal em inteiro escalado por 10^casas.
// Desambigua pt-BR de en-US pelo último separador presente.
func parseDecimal(s string, casas int) (int64, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "R$")
	t = strings.NewReplacer(" ", "", " ", "").Replace(t)
	if t == "" {
		return 0, fmt.Errorf("vazio")
	}
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimPrefix(t, "-")

	ultVirgula := strings.LastIndex(t, ",")
	ultPonto := strings.LastIndex(t, ".")
	var sepDecimal int
	switch {
	case ultVirgula > ultPonto:
		sepDecimal = ultVirgula // pt-BR: 1.234,56
	case ultPonto > ultVirgula:
		sepDecimal = ultPonto // en-US: 1,234.56
	default:
		sepDecimal = -1 // sem decimais
	}

	var inteiro, frac string
	if sepDecimal >= 0 {
		inteiro, frac = t[:sepDecimal], t[sepDecimal+1:]
	} else {
		inteiro = t
	}
	inteiro = strings.NewReplacer(".", "", ",", "").Replace(inteiro)
	if inteiro == "" {
		inteiro = "0"
	}
	if len(frac) > casas {
		return 0, fmt.Errorf("mais de %d casas decimais", casas)
	}
	frac += strings.Repeat("0", casas-len(frac))

	var n int64
	for _, parte := range []string{inteiro, frac} {
		for _, c := range parte {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("caractere inesperado %q", c)
			}
			n = n*10 + int64(c-'0')
		}
	}
	if neg {
		n = -n
	}
	return n, nil
}
