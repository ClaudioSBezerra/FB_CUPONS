package validacao

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// SimilaridadeMinima é o corte da regra 6.4: se a descrição lida for pelo
// menos 55% similar à do catálogo, usa-se a do catálogo.
const SimilaridadeMinima = 0.55

// aplicaCatalogo implementa a regra 6.4: código exato preenche descrição e
// unidade; código a distância 1 de um do catálogo é apenas sugerido; código
// desconhecido é aceito e o produto ficará com confirmado=false.
func aplicaCatalogo(c *dominio.Cupom, it *dominio.Item, ctx Contexto) {
	it.Codigo = strings.TrimSpace(it.Codigo)
	it.Produto = strings.TrimSpace(it.Produto)

	if it.Codigo == "" {
		it.MarcaIncerto(dominio.CampoCod)
		c.Avisa(fmt.Sprintf("Item %d sem código legível%s.", it.Ordem, comDescricao(it)))
		if it.Unidade == "" {
			it.Unidade = dominio.UND
		}
		return
	}

	if ctx.Catalogo == nil {
		return
	}

	if p, ok := ctx.Catalogo.PorCodigo(it.Codigo); ok {
		aplicaProduto(c, it, p)
		return
	}

	// Código fora do catálogo: aceita, mas procura um vizinho a distância 1
	// de edição para sugerir. Nunca aplica automaticamente.
	it.MarcaIncerto(dominio.CampoCod)
	vizinhos := codigosADistancia1(it.Codigo, ctx.Catalogo.Todos())
	switch len(vizinhos) {
	case 0:
		c.Avisa(fmt.Sprintf("Item %d: código %s não está no catálogo%s. Será cadastrado como produto a confirmar.",
			it.Ordem, it.Codigo, comDescricao(it)))
	case 1:
		c.Avisa(fmt.Sprintf("Item %d: código %s não está no catálogo. Teria sido lido %s (%s)? Confira na foto.",
			it.Ordem, it.Codigo, vizinhos[0].Codigo, vizinhos[0].Descricao))
	default:
		c.Avisa(fmt.Sprintf("Item %d: código %s não está no catálogo e há %d códigos parecidos — confira na foto.",
			it.Ordem, it.Codigo, len(vizinhos)))
	}
	if it.Unidade == "" {
		it.Unidade = dominio.UND
	}
}

// aplicaProduto completa o item com os dados do catálogo e confere o preço
// como referência de validação (o preço oficial é o impresso no cupom).
func aplicaProduto(c *dominio.Cupom, it *dominio.Item, p dominio.Produto) {
	if it.Produto == "" {
		it.Produto = p.Descricao
	} else if it.Produto != p.Descricao && DescricaoCompativel(it.Produto, p.Descricao) {
		it.Produto = p.Descricao // leitura parcial: adota a descrição oficial
	} else if it.Produto != p.Descricao {
		it.MarcaIncerto(dominio.CampoProd)
		c.Avisa(fmt.Sprintf("Item %d (cód. %s): descrição lida %q não parece com a do catálogo %q.",
			it.Ordem, it.Codigo, it.Produto, p.Descricao))
	}

	if it.Unidade == "" {
		it.Unidade = p.Unidade
	} else if it.Unidade != p.Unidade {
		it.MarcaIncerto(dominio.CampoProd)
		c.Avisa(fmt.Sprintf("Item %d (cód. %s): unidade lida %s, catálogo diz %s.",
			it.Ordem, it.Codigo, it.Unidade, p.Unidade))
	}

	if p.PrecoRef != nil && it.Vun != 0 && it.Vun != *p.PrecoRef {
		c.Avisa(fmt.Sprintf("Item %d (cód. %s): valor unitário %s difere do preço de referência %s. Vale o impresso no cupom.",
			it.Ordem, it.Codigo, it.Vun, *p.PrecoRef))
	}
}

// codigosADistancia1 devolve os produtos do catálogo cujo código está a uma
// única edição (troca, inserção ou remoção de dígito) do código lido.
func codigosADistancia1(codigo string, catalogo []dominio.Produto) []dominio.Produto {
	var out []dominio.Produto
	for _, p := range catalogo {
		if p.Codigo == "" || p.Codigo == codigo {
			continue
		}
		if distanciaEdicao(codigo, p.Codigo, 1) == 1 {
			out = append(out, p)
		}
	}
	return out
}

// distanciaEdicao calcula a distância de Levenshtein com teto: devolve
// limite+1 assim que souber que passou do limite.
func distanciaEdicao(a, b string, limite int) int {
	ra, rb := []rune(a), []rune(b)
	if dif := len(ra) - len(rb); dif > limite || -dif > limite {
		return limite + 1
	}
	linha := make([]int, len(rb)+1)
	for j := range linha {
		linha[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		anterior := linha[0]
		linha[0] = i
		menor := i
		for j := 1; j <= len(rb); j++ {
			custo := 1
			if ra[i-1] == rb[j-1] {
				custo = 0
			}
			atual := min3(linha[j]+1, linha[j-1]+1, anterior+custo)
			anterior = linha[j]
			linha[j] = atual
			if atual < menor {
				menor = atual
			}
		}
		if menor > limite {
			return limite + 1
		}
	}
	return linha[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// DescricaoCompativel decide se a descrição lida e a do catálogo são o mesmo
// produto. Usada só quando o CÓDIGO bateu exatamente, então a descrição do
// catálogo é a autoridade — o papel desta função é separar "leitura ruim do
// mesmo produto" de "código certo com descrição de outro produto", que é
// sinal de erro e precisa de revisão humana.
//
// Impressão térmica apagada trunca o fim da linha ("ROSCAS 80G" no lugar de
// "ROSCAS 80G DIET/INTEGRAL"), e a distância de edição penaliza demais essa
// diferença de comprimento. Por isso contenção conta como compatível.
func DescricaoCompativel(lida, catalogo string) bool {
	nl, nc := normaliza(lida), normaliza(catalogo)
	if nl == "" || nc == "" {
		return false
	}
	if strings.Contains(nc, nl) || strings.Contains(nl, nc) {
		return true
	}
	return Similaridade(lida, catalogo) >= SimilaridadeMinima
}

// Similaridade devolve um índice de 0 a 1 entre duas descrições de produto,
// ignorando acentos, caixa e pontuação. Baseada na distância de edição.
func Similaridade(a, b string) float64 {
	na, nb := normaliza(a), normaliza(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	maior := len([]rune(na))
	if n := len([]rune(nb)); n > maior {
		maior = n
	}
	d := distanciaEdicao(na, nb, maior)
	return 1 - float64(d)/float64(maior)
}

// normaliza prepara texto para comparação: minúsculas, sem acento, sem
// pontuação e com espaços colapsados.
func normaliza(s string) string {
	var b strings.Builder
	espaco := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		r = semAcento(r)
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if espaco && b.Len() > 0 {
				b.WriteByte(' ')
			}
			espaco = false
			b.WriteRune(r)
		default:
			espaco = true
		}
	}
	return b.String()
}

// semAcento mapeia as vogais acentuadas e o cedilha do português.
func semAcento(r rune) rune {
	const acentuadas = "áàâãäéèêëíìîïóòôõöúùûüçñ"
	const simples = "aaaaaeeeeiiiiooooouuuucn"
	if i := strings.IndexRune(acentuadas, r); i >= 0 {
		return []rune(simples)[len([]rune(acentuadas[:i]))]
	}
	return r
}

func comDescricao(it *dominio.Item) string {
	if it.Produto == "" {
		return ""
	}
	return fmt.Sprintf(" (lido como %q)", it.Produto)
}
