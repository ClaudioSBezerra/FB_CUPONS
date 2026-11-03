// Package validacao implementa as regras de negócio de um cupom fiscal:
// CNPJ, aritmética de itens e do cupom, conciliação pelo catálogo e datas.
//
// Nenhuma regra aqui depende de banco nem de IA — é código puro e testável.
// Princípio: na dúvida, marcar incerto e avisar. Nunca "chutar" em silêncio.
package validacao

import (
	"fmt"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// pesosDV1 e pesosDV2 são os pesos oficiais do módulo 11 do CNPJ.
var (
	pesosDV1 = [12]int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	pesosDV2 = [13]int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
)

// CNPJValido confere formato (14 dígitos) e os dois dígitos verificadores.
// Rejeita também os repetidos (00000000000000, 11111111111111, ...), que
// passam no módulo 11 mas não são CNPJ real.
func CNPJValido(cnpj string) bool {
	d := dominio.SoDigitos(cnpj)
	if len(d) != 14 {
		return false
	}
	if todosIguais(d) {
		return false
	}
	return int(d[12]-'0') == dvModulo11(d[:12], pesosDV1[:]) &&
		int(d[13]-'0') == dvModulo11(d[:13], pesosDV2[:])
}

func dvModulo11(digitos string, pesos []int) int {
	soma := 0
	for i, c := range digitos {
		soma += int(c-'0') * pesos[i]
	}
	resto := soma % 11
	if resto < 2 {
		return 0
	}
	return 11 - resto
}

func todosIguais(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

// DistanciaHamming conta em quantas posições dois CNPJs de mesmo tamanho
// diferem. Devolve -1 se os tamanhos divergirem.
func DistanciaHamming(a, b string) int {
	da, db := dominio.SoDigitos(a), dominio.SoDigitos(b)
	if len(da) != len(db) {
		return -1
	}
	n := 0
	for i := range da {
		if da[i] != db[i] {
			n++
		}
	}
	return n
}

// LimiteHamming é a distância máxima aceita para sugerir um CNPJ conhecido.
const LimiteHamming = 3

// SugestaoCNPJ é o resultado de procurar um CNPJ conhecido parecido com o lido.
type SugestaoCNPJ struct {
	CNPJ      string // CNPJ conhecido sugerido (só dígitos)
	Distancia int    // distância de Hamming até o lido
	Empate    bool   // houve mais de um candidato à mesma distância mínima
}

// SugereCNPJ procura, entre os CNPJs conhecidos, um único candidato a
// distância de Hamming <= LimiteHamming cujo DV seja válido.
//
// Regra crítica (CLAUDE.md §2): os CNPJs da Maternidade Célia Câmara e da
// Dona Íris diferem em exatamente 3 dígitos, então o empate é real. Em caso
// de empate NÃO se sugere nada — o lido é mantido e marcado como incerto.
func SugereCNPJ(lido string, conhecidos []string) (SugestaoCNPJ, bool) {
	d := dominio.SoDigitos(lido)
	if len(d) != 14 {
		return SugestaoCNPJ{}, false
	}
	melhor := SugestaoCNPJ{Distancia: LimiteHamming + 1}
	for _, c := range conhecidos {
		cd := dominio.SoDigitos(c)
		if !CNPJValido(cd) {
			continue // nunca sugerir um "conhecido" com DV quebrado
		}
		dist := DistanciaHamming(d, cd)
		if dist < 0 || dist > LimiteHamming {
			continue
		}
		switch {
		case dist < melhor.Distancia:
			melhor = SugestaoCNPJ{CNPJ: cd, Distancia: dist, Empate: false}
		case dist == melhor.Distancia && cd != melhor.CNPJ:
			melhor.Empate = true
		}
	}
	if melhor.CNPJ == "" || melhor.Empate {
		return melhor, false
	}
	return melhor, true
}

// ValidaCNPJCliente aplica a regra 6.1 ao CNPJ do cliente lido no cupom.
// Devolve o CNPJ a gravar (sempre o lido — correção automática nunca é
// aplicada em lote), se é incerto, e os avisos gerados.
func ValidaCNPJCliente(lido string, conhecidos []string) (cnpj string, incerto bool, avisos []string) {
	d := dominio.SoDigitos(lido)
	if d == "" {
		return "", true, []string{"CNPJ do cliente não foi lido."}
	}
	if len(d) != 14 {
		return d, true, []string{fmt.Sprintf("CNPJ do cliente com %d dígitos (esperado 14): %s", len(d), lido)}
	}
	if CNPJValido(d) {
		return d, false, nil
	}
	av := []string{fmt.Sprintf("CNPJ do cliente %s tem dígito verificador inválido.", dominio.FormataCNPJ(d))}
	if s, ok := SugereCNPJ(d, conhecidos); ok {
		av = append(av, fmt.Sprintf("Sugestão: %s (difere em %d dígito(s)). Confira na foto antes de aplicar.",
			dominio.FormataCNPJ(s.CNPJ), s.Distancia))
	} else if s.Empate {
		av = append(av, "Há mais de um CNPJ conhecido igualmente parecido — não é possível sugerir. Confira na foto.")
	}
	return d, true, av
}

// ValidaCNPJEmitente confere o CNPJ do emitente. Divergência leve
// (Hamming <= 3) é ajustada para o CNPJ oficial com aviso, porque o
// emitente é sempre o mesmo. Divergência grande é mantida e avisada.
func ValidaCNPJEmitente(lido string) (cnpj string, avisos []string) {
	d := dominio.SoDigitos(lido)
	if d == "" {
		return dominio.CNPJEmitente, []string{"CNPJ do emitente não foi lido; assumido o da ITD/STRAVAGANZA."}
	}
	if d == dominio.CNPJEmitente {
		return d, nil
	}
	if dist := DistanciaHamming(d, dominio.CNPJEmitente); dist > 0 && dist <= LimiteHamming {
		return dominio.CNPJEmitente, []string{fmt.Sprintf(
			"CNPJ do emitente lido como %s, ajustado para o da ITD/STRAVAGANZA %s (%d dígito(s) de diferença).",
			dominio.FormataCNPJ(d), dominio.FormataCNPJ(dominio.CNPJEmitente), dist)}
	}
	return d, []string{fmt.Sprintf(
		"CNPJ do emitente %s não é o da ITD/STRAVAGANZA (%s). Este cupom é de outro emitente?",
		dominio.FormataCNPJ(d), dominio.FormataCNPJ(dominio.CNPJEmitente))}
}
