package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// --- Clientes ---

// ListarClientes devolve os clientes; incluirInativos=false traz só ativos.
func (s *Store) ListarClientes(ctx context.Context, incluirInativos bool) ([]dominio.Cliente, error) {
	sql := `SELECT id, nome, cnpj, ativo, obs FROM clientes`
	if !incluirInativos {
		sql += ` WHERE ativo`
	}
	sql += ` ORDER BY nome`

	linhas, err := s.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []dominio.Cliente
	for linhas.Next() {
		var c dominio.Cliente
		if err := linhas.Scan(&c.ID, &c.Nome, &c.CNPJ, &c.Ativo, &c.Obs); err != nil {
			return nil, err
		}
		c.CNPJ = strings.TrimSpace(c.CNPJ) // CHAR(14) vem com padding
		out = append(out, c)
	}
	return out, linhas.Err()
}

// ClientePorCNPJ busca pelo CNPJ (só dígitos).
func (s *Store) ClientePorCNPJ(ctx context.Context, cnpj string) (dominio.Cliente, error) {
	var c dominio.Cliente
	err := s.pool.QueryRow(ctx,
		`SELECT id, nome, cnpj, ativo, obs FROM clientes WHERE cnpj = $1`,
		dominio.SoDigitos(cnpj),
	).Scan(&c.ID, &c.Nome, &c.CNPJ, &c.Ativo, &c.Obs)
	if err != nil {
		return dominio.Cliente{}, semLinhas(err)
	}
	c.CNPJ = strings.TrimSpace(c.CNPJ)
	return c, nil
}

// CriarCliente insere um cliente novo.
func (s *Store) CriarCliente(ctx context.Context, c dominio.Cliente) (dominio.Cliente, error) {
	cnpj := dominio.SoDigitos(c.CNPJ)
	err := s.pool.QueryRow(ctx,
		`INSERT INTO clientes (nome, cnpj, ativo, obs) VALUES ($1, $2, $3, $4) RETURNING id`,
		strings.TrimSpace(c.Nome), cnpj, c.Ativo, c.Obs,
	).Scan(&c.ID)
	if ehViolacaoUnicidade(err) {
		return dominio.Cliente{}, &ErroConflito{Msg: fmt.Sprintf(
			"já existe um cliente com o CNPJ %s", dominio.FormataCNPJ(cnpj))}
	}
	if err != nil {
		return dominio.Cliente{}, err
	}
	c.CNPJ = cnpj
	return c, nil
}

// AtualizarCliente grava nome, CNPJ, situação e observação.
func (s *Store) AtualizarCliente(ctx context.Context, c dominio.Cliente) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE clientes SET nome = $2, cnpj = $3, ativo = $4, obs = $5 WHERE id = $1`,
		c.ID, strings.TrimSpace(c.Nome), dominio.SoDigitos(c.CNPJ), c.Ativo, c.Obs)
	if ehViolacaoUnicidade(err) {
		return &ErroConflito{Msg: fmt.Sprintf("o CNPJ %s já pertence a outro cliente",
			dominio.FormataCNPJ(c.CNPJ))}
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErroNaoEncontrado
	}
	return nil
}

// --- Produtos ---

// ListarProdutos devolve o catálogo inteiro, ordenado por descrição.
func (s *Store) ListarProdutos(ctx context.Context) ([]dominio.Produto, error) {
	linhas, err := s.pool.Query(ctx,
		`SELECT id, COALESCE(codigo, ''), descricao, unidade,
		        COALESCE(preco_ref::text, ''), confirmado
		   FROM produtos ORDER BY descricao, codigo`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []dominio.Produto
	for linhas.Next() {
		var p dominio.Produto
		var preco string
		if err := linhas.Scan(&p.ID, &p.Codigo, &p.Descricao, &p.Unidade, &preco, &p.Confirmado); err != nil {
			return nil, err
		}
		if preco != "" {
			v, err := lerValor(preco)
			if err != nil {
				return nil, fmt.Errorf("preço de referência inválido no produto %d: %w", p.ID, err)
			}
			p.PrecoRef = &v
		}
		out = append(out, p)
	}
	return out, linhas.Err()
}

// CriarProduto insere um produto no catálogo. Código vazio é gravado como
// NULL, para que vários produtos sem código legível possam coexistir.
func (s *Store) CriarProduto(ctx context.Context, p dominio.Produto) (dominio.Produto, error) {
	var preco *string
	if p.PrecoRef != nil {
		t := valorTexto(*p.PrecoRef)
		preco = &t
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO produtos (codigo, descricao, unidade, preco_ref, confirmado)
		 VALUES (NULLIF($1, ''), $2, $3, $4::numeric, $5) RETURNING id`,
		strings.TrimSpace(p.Codigo), strings.TrimSpace(p.Descricao), p.Unidade, preco, p.Confirmado,
	).Scan(&p.ID)
	if ehViolacaoUnicidade(err) {
		return dominio.Produto{}, &ErroConflito{Msg: fmt.Sprintf("já existe produto com o código %s", p.Codigo)}
	}
	if err != nil {
		return dominio.Produto{}, err
	}
	return p, nil
}

// AtualizarProduto grava as alterações feitas pela tela de Cadastros.
func (s *Store) AtualizarProduto(ctx context.Context, p dominio.Produto) error {
	var preco *string
	if p.PrecoRef != nil {
		t := valorTexto(*p.PrecoRef)
		preco = &t
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE produtos
		    SET codigo = NULLIF($2, ''), descricao = $3, unidade = $4,
		        preco_ref = $5::numeric, confirmado = $6
		  WHERE id = $1`,
		p.ID, strings.TrimSpace(p.Codigo), strings.TrimSpace(p.Descricao), p.Unidade, preco, p.Confirmado)
	if ehViolacaoUnicidade(err) {
		return &ErroConflito{Msg: fmt.Sprintf("o código %s já pertence a outro produto", p.Codigo)}
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErroNaoEncontrado
	}
	return nil
}

// GarantirProduto cadastra um produto visto num cupom que ainda não está no
// catálogo, com confirmado = FALSE. Produto sem código NÃO é cadastrado
// automaticamente: sem chave para deduplicar, cada leitura criaria uma linha
// nova e entupiria o catálogo.
func (s *Store) GarantirProduto(ctx context.Context, codigo, descricao string, un dominio.Unidade) error {
	codigo = strings.TrimSpace(codigo)
	if codigo == "" {
		return nil
	}
	if un != dominio.UND && un != dominio.KG {
		un = dominio.UND
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO produtos (codigo, descricao, unidade, confirmado)
		 VALUES ($1, $2, $3, FALSE)
		 ON CONFLICT (codigo) WHERE codigo IS NOT NULL DO NOTHING`,
		codigo, strings.TrimSpace(descricao), un)
	return err
}

// CatalogoMemoria é um snapshot do catálogo que implementa validacao.Catalogo.
// Carregar uma vez por lote evita uma consulta por item.
type CatalogoMemoria struct {
	porCodigo map[string]dominio.Produto
	todos     []dominio.Produto
}

// CarregarCatalogo monta o snapshot.
func (s *Store) CarregarCatalogo(ctx context.Context) (*CatalogoMemoria, error) {
	produtos, err := s.ListarProdutos(ctx)
	if err != nil {
		return nil, err
	}
	c := &CatalogoMemoria{porCodigo: make(map[string]dominio.Produto, len(produtos)), todos: produtos}
	for _, p := range produtos {
		if p.Codigo != "" {
			c.porCodigo[p.Codigo] = p
		}
	}
	return c, nil
}

// PorCodigo atende validacao.Catalogo.
func (c *CatalogoMemoria) PorCodigo(codigo string) (dominio.Produto, bool) {
	p, ok := c.porCodigo[codigo]
	return p, ok
}

// Todos atende validacao.Catalogo.
func (c *CatalogoMemoria) Todos() []dominio.Produto { return c.todos }

// CatalogoDe monta um snapshot a partir de uma lista de produtos já em
// memória — usado pelos testes e por quem já tem o catálogo carregado.
func CatalogoDe(produtos []dominio.Produto) *CatalogoMemoria {
	c := &CatalogoMemoria{porCodigo: make(map[string]dominio.Produto, len(produtos)), todos: produtos}
	for _, p := range produtos {
		if p.Codigo != "" {
			c.porCodigo[p.Codigo] = p
		}
	}
	return c
}
