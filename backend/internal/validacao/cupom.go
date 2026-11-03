package validacao

import (
	"fmt"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// Tolerancia é a folga aceita em comparações de valor (R$ 0,01).
const Tolerancia = dominio.Valor(1)

// Catalogo é a visão do catálogo de produtos usada pelas validações.
type Catalogo interface {
	// PorCodigo devolve o produto de código exato, se existir.
	PorCodigo(codigo string) (dominio.Produto, bool)
	// Todos devolve o catálogo inteiro (para busca por similaridade).
	Todos() []dominio.Produto
}

// Contexto reúne o que a validação precisa saber do mundo externo.
type Contexto struct {
	Catalogo       Catalogo
	CNPJConhecidos []string // CNPJs de clientes cadastrados, só dígitos
	// MesReferencia é a competência esperada ("AAAA-MM"); vazio desliga a
	// checagem de ano/mês suspeito.
	MesReferencia string
}

// Resultado é o veredito da validação de um cupom.
type Resultado struct {
	// PodeConfirmar indica se o cupom satisfaz a regra 6.8 e poderia ir a
	// 'confirmado' sem justificativa do operador.
	PodeConfirmar bool
	// Bloqueios lista, em português, por que o cupom não pode ser confirmado.
	Bloqueios []string
}

// Valida aplica todas as regras da seção 6 do CLAUDE.md ao cupom, alterando-o
// no lugar: normaliza CNPJs, completa descrição/unidade pelo catálogo,
// recalcula o que der para reconciliar e preenche Incertos/Avisos.
//
// A função nunca inventa valor: quando deduz algo, marca o campo como incerto
// e registra o aviso correspondente.
func Valida(c *dominio.Cupom, ctx Contexto) Resultado {
	validaEmitente(c)
	validaCliente(c, ctx)
	validaNumero(c)
	validaData(c, ctx)
	for i := range c.Itens {
		validaItem(c, &c.Itens[i], ctx)
	}
	validaSoma(c)

	// Regra 5 do prompt de leitura: número e data sempre vão para revisão
	// humana, porque são os campos que mais erram em cupom apagado.
	c.MarcaIncerto(dominio.CampoNumero, dominio.CampoData)

	return avalia(c)
}

func validaEmitente(c *dominio.Cupom) {
	cnpj, avisos := ValidaCNPJEmitente(c.CNPJEmitente)
	c.CNPJEmitente = cnpj
	c.Avisa(avisos...)
}

func validaCliente(c *dominio.Cupom, ctx Contexto) {
	cnpj, incerto, avisos := ValidaCNPJCliente(c.CNPJCliente, ctx.CNPJConhecidos)
	c.CNPJCliente = cnpj
	c.Avisa(avisos...)
	if incerto {
		c.MarcaIncerto(dominio.CampoCNPJCli)
	}
	if strings.TrimSpace(c.Cliente) == "" {
		c.MarcaIncerto(dominio.CampoCliente)
		c.Avisa("Nome do cliente não foi lido.")
	}
}

func validaNumero(c *dominio.Cupom) {
	c.Numero = strings.TrimSpace(c.Numero)
	if c.Numero == "" {
		c.Avisa("Número do cupom não foi lido.")
		return
	}
	if dominio.SoDigitos(c.Numero) != c.Numero {
		c.Avisa(fmt.Sprintf("Número do cupom %q tem caracteres que não são dígitos.", c.Numero))
	}
}

func validaData(c *dominio.Cupom, ctx Contexto) {
	if c.Data.Zero() {
		c.Avisa("Data do cupom não foi lida.")
		return
	}
	if !c.Data.Valida() {
		c.Avisa(fmt.Sprintf("Data %s não existe no calendário.", c.Data.Formatada()))
		return
	}
	if ctx.MesReferencia == "" {
		return
	}
	// Ano fora da janela do mês de referência é suspeito: no protótipo o OCR
	// leu 2025 em cupons de 2026.
	if !dentroDaJanela(c.Data.Competencia(), ctx.MesReferencia) {
		c.MarcaIncerto(dominio.CampoData)
		c.Avisa(fmt.Sprintf("Data %s está fora de %s ± 1 mês. O ano/mês foi lido certo?",
			c.Data.Formatada(), ctx.MesReferencia))
	}
}

// dentroDaJanela aceita a competência do cupom se ela estiver no mês de
// referência ou nos meses imediatamente anterior/posterior.
func dentroDaJanela(competencia, referencia string) bool {
	ref, err := dominio.ParseData(referencia + "-01")
	if err != nil {
		return true // referência malformada não deve reprovar o cupom
	}
	for _, delta := range []int{-1, 0, 1} {
		t := ref.Time().AddDate(0, delta, 0)
		if dominio.DataDeTime(t).Competencia() == competencia {
			return true
		}
	}
	return false
}

// validaItem aplica as regras 2 e 4: catálogo, aritmética e reconciliação.
func validaItem(c *dominio.Cupom, it *dominio.Item, ctx Contexto) {
	aplicaCatalogo(c, it, ctx)

	if it.Qtd == 0 && it.Vun == 0 && it.Vtot == 0 {
		it.MarcaIncerto(dominio.CampoQtd, dominio.CampoVun, dominio.CampoVtot)
		c.Avisa(fmt.Sprintf("Item %d (%s) está sem quantidade e sem valor — preencha olhando a foto.",
			it.Ordem, descricaoCurta(it)))
		return
	}

	esperado := it.Qtd.Multiplicar(it.Vun)
	if (esperado - it.Vtot).Abs() <= Tolerancia {
		return // aritmética fecha
	}

	// Tenta reconciliar deduzindo o único campo que falta.
	if reconciliaItem(c, it, esperado) {
		return
	}

	it.MarcaIncerto(dominio.CampoQtd, dominio.CampoVun, dominio.CampoVtot)
	c.Avisa(fmt.Sprintf("Item %d (%s): %s × %s = %s, mas o valor do item lido é %s.",
		it.Ordem, descricaoCurta(it), it.Qtd, it.Vun, esperado, it.Vtot))
}

// reconciliaItem deduz o campo ilegível quando exatamente um dos três está
// zerado e os outros dois bastam para calculá-lo. Só aceita o resultado se
// ele for consistente; o campo deduzido fica marcado como incerto.
func reconciliaItem(c *dominio.Cupom, it *dominio.Item, esperado dominio.Valor) bool {
	switch {
	case it.Vtot == 0 && it.Qtd != 0 && it.Vun != 0:
		it.Vtot = esperado
		it.MarcaIncerto(dominio.CampoVtot)
		c.Avisa(fmt.Sprintf("Item %d (%s): valor total ilegível, deduzido %s × %s = %s.",
			it.Ordem, descricaoCurta(it), it.Qtd, it.Vun, it.Vtot))
		return true

	case it.Vun == 0 && it.Qtd != 0 && it.Vtot != 0:
		vun := dominio.Valor(arredondaDiv(int64(it.Vtot)*1000, int64(it.Qtd)))
		if it.Qtd.Multiplicar(vun).Abs()-it.Vtot.Abs() > Tolerancia {
			return false
		}
		it.Vun = vun
		it.MarcaIncerto(dominio.CampoVun)
		c.Avisa(fmt.Sprintf("Item %d (%s): valor unitário ilegível, deduzido %s ÷ %s = %s.",
			it.Ordem, descricaoCurta(it), it.Vtot, it.Qtd, it.Vun))
		return true

	case it.Qtd == 0 && it.Vun != 0 && it.Vtot != 0:
		qtd := dominio.Qtd(arredondaDiv(int64(it.Vtot)*1000, int64(it.Vun)))
		// Só aceita se a conta voltar a fechar exatamente.
		if (qtd.Multiplicar(it.Vun) - it.Vtot).Abs() > Tolerancia {
			return false
		}
		// Em UND a quantidade tem de ser inteira; em KG pode ser fracionada.
		if it.Unidade == dominio.UND && qtd%1000 != 0 {
			return false
		}
		it.Qtd = qtd
		it.MarcaIncerto(dominio.CampoQtd)
		c.Avisa(fmt.Sprintf("Item %d (%s): quantidade ilegível, deduzida %s ÷ %s = %s.",
			it.Ordem, descricaoCurta(it), it.Vtot, it.Vun, it.Qtd))
		return true
	}
	return false
}

func arredondaDiv(num, den int64) int64 {
	if den == 0 {
		return 0
	}
	if (num < 0) != (den < 0) {
		return -((-num + den/2) / den)
	}
	return (num + den/2) / den
}

// validaSoma aplica a regra 3: Σ vtot == total.
func validaSoma(c *dominio.Cupom) {
	soma := c.SomaItens()

	if len(c.Itens) == 0 {
		c.Avisa("Cupom sem itens.")
		return
	}

	if c.Total == 0 {
		// Regra 7: total zero sempre fica pendente. A soma é oferecida como
		// sugestão, mas o total NÃO é sobrescrito — quem decide é o operador.
		c.MarcaIncerto(dominio.CampoTotal)
		c.Avisa(fmt.Sprintf("Total do cupom não foi lido. A soma dos itens é %s — confirme na foto.", soma))
		return
	}

	dif := soma - c.Total
	if dif.Abs() <= Tolerancia {
		return
	}

	c.MarcaIncerto(dominio.CampoTotal)
	c.Avisa(fmt.Sprintf("A soma dos itens (%s) difere do total lido (%s) em %s.",
		soma, c.Total, dif.Abs()))

	// Se exatamente um item estiver ilegível, a diferença o explica: sugere
	// o valor deduzido sem aplicá-lo.
	if idx, ok := unicoItemIncerto(c); ok {
		it := &c.Itens[idx]
		deduzido := it.Vtot - dif
		c.Avisa(fmt.Sprintf("Se o item %d (%s) valer %s, o cupom fecha em %s.",
			it.Ordem, descricaoCurta(it), deduzido, c.Total))
	}
}

// unicoItemIncerto devolve o índice do único item com valor incerto.
func unicoItemIncerto(c *dominio.Cupom) (int, bool) {
	idx := -1
	for i := range c.Itens {
		it := &c.Itens[i]
		if it.TemIncerto(dominio.CampoVtot) || it.TemIncerto(dominio.CampoQtd) || it.TemIncerto(dominio.CampoVun) {
			if idx >= 0 {
				return 0, false // mais de um: a diferença não é atribuível
			}
			idx = i
		}
	}
	return idx, idx >= 0
}

// avalia monta o veredito da regra 6.8.
func avalia(c *dominio.Cupom) Resultado {
	var bloqueios []string

	if !CNPJValido(c.CNPJCliente) {
		bloqueios = append(bloqueios, "CNPJ do cliente inválido.")
	}
	if len(c.Itens) == 0 {
		bloqueios = append(bloqueios, "O cupom não tem nenhum item.")
	}
	if c.Total == 0 {
		bloqueios = append(bloqueios, "O total do cupom está zerado.")
	}
	for i := range c.Itens {
		it := &c.Itens[i]
		if (it.Qtd.Multiplicar(it.Vun) - it.Vtot).Abs() > Tolerancia {
			bloqueios = append(bloqueios, fmt.Sprintf(
				"Item %d (%s): quantidade × valor unitário não fecha com o valor do item.",
				it.Ordem, descricaoCurta(it)))
		}
	}
	if len(c.Itens) > 0 && c.Total != 0 {
		if dif := c.SomaItens() - c.Total; dif.Abs() > Tolerancia {
			bloqueios = append(bloqueios, fmt.Sprintf(
				"A soma dos itens (%s) não fecha com o total (%s).", c.SomaItens(), c.Total))
		}
	}

	return Resultado{PodeConfirmar: len(bloqueios) == 0, Bloqueios: bloqueios}
}

func descricaoCurta(it *dominio.Item) string {
	if it.Codigo != "" {
		return "cód. " + it.Codigo
	}
	if it.Produto != "" {
		return it.Produto
	}
	return "sem código"
}
