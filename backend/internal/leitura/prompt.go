// Package leitura lê cupons fiscais a partir de imagens usando a API de
// visão do Claude.
//
// O protótipo usava OCR Tesseract no navegador e falhava em impressão térmica
// apagada (cupons da Dona Íris). A leitura pelo Claude é a referência de
// qualidade, e este pacote a reproduz no servidor.
package leitura

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// Referencias é o contexto de negócio injetado no prompt para desambiguar
// leituras — códigos e CNPJs conhecidos.
type Referencias struct {
	Produtos []dominio.Produto
	Clientes []dominio.Cliente
}

// sistemaBase é a parte do prompt que NÃO muda entre cupons. Fica primeiro e
// é marcada para cache: o catálogo e a lista de clientes são idênticos em
// todas as páginas de um lote, e trocar a ordem invalidaria o cache.
const sistemaBase = `Você lê cupons fiscais térmicos de uma padaria.

Emitente: STRAVAGANZA / ITD Padaria e Confeitaria Eireli – ME, CNPJ 29.220.487/0001-08.

A imagem contém UM cupom fiscal. A impressão é matricial/térmica e muitas vezes
está apagada, com traço fino e falhado. Extraia:

- número do cupom
- data (dd/mm/aaaa)
- CNPJ e nome do cliente (o destinatário, não o emitente)
- TODOS os itens: código, descrição, unidade (UND ou KG), quantidade,
  valor unitário e valor total do item
- o total do cupom

Regras de leitura, em ordem de prioridade:

1. NUNCA invente. Se um campo estiver ilegível, devolva null nesse campo e
   liste o nome do campo em "incertos". Um campo nulo e sinalizado é sempre
   melhor do que um palpite: este é um sistema de cobrança e cada erro vira
   uma fatura errada.
2. Leia os números dígito a dígito. Em impressão térmica apagada 3/8, 5/6 e
   0/8 se confundem; se não tiver certeza de um dígito, o campo é ilegível.
3. Aritmética: quantidade × valor unitário tem de dar o valor total do item,
   e a soma dos valores dos itens tem de dar o total do cupom. Se não fechar,
   releia a imagem antes de responder. O que continuar divergindo depois da
   releitura vai para "avisos", descrito em português.
4. Quantidade em UND é sempre inteira. Em KG pode ter até 3 decimais.
5. Use os códigos de produto conhecidos para desambiguar uma descrição
   apagada, mas NUNCA substitua um código ilegível por um palpite: código
   ilegível é null com "cod" em "incertos" do item.
6. Use um CNPJ de cliente conhecido somente se os dígitos que você conseguiu
   ler forem compatíveis com ele. Os dois maiores clientes têm CNPJ que difere
   em apenas 3 dígitos — na dúvida entre eles, devolva o que está impresso e
   marque "cnpj_cliente" em "incertos".
7. Valores e quantidades vão como número decimal com ponto (1234.56), nunca
   como texto com vírgula ou "R$".
8. Datas vão no formato aaaa-mm-dd.

Responda apenas com o JSON do schema, sem texto em volta.`

// MontarSistema monta o prompt de sistema completo: a parte fixa seguida do
// catálogo e dos clientes conhecidos, ambos ordenados para que o texto seja
// byte a byte idêntico entre chamadas e o cache de prompt funcione.
func MontarSistema(ref Referencias) string {
	var b strings.Builder
	b.WriteString(sistemaBase)

	if len(ref.Produtos) > 0 {
		produtos := make([]dominio.Produto, len(ref.Produtos))
		copy(produtos, ref.Produtos)
		sort.Slice(produtos, func(i, j int) bool { return produtos[i].Codigo < produtos[j].Codigo })

		b.WriteString("\n\n## Códigos de produto conhecidos\n\n")
		b.WriteString("código | produto | unidade | preço de referência\n")
		for _, p := range produtos {
			if p.Codigo == "" {
				continue
			}
			preco := "—"
			if p.PrecoRef != nil {
				preco = p.PrecoRef.String()
			}
			fmt.Fprintf(&b, "%s | %s | %s | %s\n", p.Codigo, p.Descricao, p.Unidade, preco)
		}
		b.WriteString("\nO preço de referência serve só para conferência: o valor que vale é o impresso no cupom.")
	}

	if len(ref.Clientes) > 0 {
		clientes := make([]dominio.Cliente, len(ref.Clientes))
		copy(clientes, ref.Clientes)
		sort.Slice(clientes, func(i, j int) bool { return clientes[i].CNPJ < clientes[j].CNPJ })

		b.WriteString("\n\n## CNPJs de clientes conhecidos\n\n")
		for _, c := range clientes {
			fmt.Fprintf(&b, "%s — %s\n", dominio.FormataCNPJ(c.CNPJ), c.Nome)
		}
	}

	return b.String()
}

// instrucaoPagina é o texto que acompanha a imagem. Vem DEPOIS do bloco de
// imagem e identifica a origem, para ajudar na conferência humana.
func instrucaoPagina(fonte string) string {
	if fonte == "" {
		return "Leia este cupom e responda com o JSON do schema."
	}
	return fmt.Sprintf("Leia este cupom (origem: %s) e responda com o JSON do schema.", fonte)
}

// SchemaJSON é o schema de saída estruturada exigido do modelo.
//
// Todo campo que pode ser ilegível é nullable: é isso que permite ao modelo
// dizer "não consegui ler" em vez de inventar.
func SchemaJSON() map[string]any {
	str := map[string]any{"type": []any{"string", "null"}}
	num := map[string]any{"type": []any{"number", "null"}}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []any{
			"numero", "data", "cnpj_cliente", "cliente", "total", "itens", "incertos", "avisos",
		},
		"properties": map[string]any{
			"numero": map[string]any{
				"type":        []any{"string", "null"},
				"description": "Número do cupom, só dígitos, como impresso. null se ilegível.",
			},
			"data": map[string]any{
				"type":        []any{"string", "null"},
				"description": "Data do cupom em aaaa-mm-dd. null se ilegível.",
			},
			"cnpj_cliente": map[string]any{
				"type":        []any{"string", "null"},
				"description": "CNPJ do cliente destinatário, 14 dígitos sem pontuação. null se ilegível.",
			},
			"cliente": map[string]any{
				"type":        []any{"string", "null"},
				"description": "Nome do cliente destinatário como impresso. null se ilegível.",
			},
			"total": map[string]any{
				"type":        []any{"number", "null"},
				"description": "Total do cupom em reais. null se ilegível — não use a soma dos itens aqui.",
			},
			"itens": map[string]any{
				"type":        "array",
				"description": "Todos os itens do cupom, na ordem impressa.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"cod", "prod", "un", "qtd", "vun", "vtot", "incertos"},
					"properties": map[string]any{
						"cod":  str,
						"prod": str,
						"un": map[string]any{
							"type": []any{"string", "null"},
							"enum": []any{"UND", "KG", nil},
						},
						"qtd":  num,
						"vun":  num,
						"vtot": num,
						"incertos": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string", "enum": []any{"cod", "prod", "un", "qtd", "vun", "vtot"}},
							"description": "Campos deste item que estão ilegíveis.",
						},
					},
				},
			},
			"incertos": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []any{"numero", "data", "cnpj_cliente", "cliente", "total"}},
				"description": "Campos do cupom que estão ilegíveis.",
			},
			"avisos": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Observações em português sobre o que não fechou ou ficou duvidoso.",
			},
		},
	}
}
