package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// --- Fotos ---

// Foto é a imagem de um cupom guardada no banco.
type Foto struct {
	ID              int64
	SHA256          string
	Mime            string
	Largura, Altura int
	Bytes           int
	Conteudo        []byte
}

// SHA256Hex calcula o hash usado para deduplicar imagens.
func SHA256Hex(dados []byte) string {
	h := sha256.Sum256(dados)
	return hex.EncodeToString(h[:])
}

// FotoPorSHA busca uma imagem já importada. Devolve ErroNaoEncontrado se não
// existir — é assim que a importação detecta "já importada".
func (s *Store) FotoPorSHA(ctx context.Context, sha string) (Foto, error) {
	var f Foto
	err := s.pool.QueryRow(ctx,
		`SELECT id, sha256, mime, largura, altura, bytes FROM cupom_fotos WHERE sha256 = $1`, sha,
	).Scan(&f.ID, &f.SHA256, &f.Mime, &f.Largura, &f.Altura, &f.Bytes)
	if err != nil {
		return Foto{}, semLinhas(err)
	}
	return f, nil
}

// InserirFoto grava a imagem; se o SHA já existir, devolve a existente.
func (s *Store) InserirFoto(ctx context.Context, f Foto) (Foto, error) {
	if f.SHA256 == "" {
		f.SHA256 = SHA256Hex(f.Conteudo)
	}
	f.Bytes = len(f.Conteudo)

	err := s.pool.QueryRow(ctx,
		`INSERT INTO cupom_fotos (sha256, mime, largura, altura, bytes, conteudo)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (sha256) DO UPDATE SET sha256 = EXCLUDED.sha256
		 RETURNING id`,
		f.SHA256, f.Mime, f.Largura, f.Altura, f.Bytes, f.Conteudo,
	).Scan(&f.ID)
	if err != nil {
		return Foto{}, err
	}
	return f, nil
}

// ConteudoFoto devolve os bytes da imagem, para a tela de revisão.
func (s *Store) ConteudoFoto(ctx context.Context, id int64) (mime string, dados []byte, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT mime, conteudo FROM cupom_fotos WHERE id = $1`, id).Scan(&mime, &dados)
	if err != nil {
		return "", nil, semLinhas(err)
	}
	return mime, dados, nil
}

// --- Cupons ---

// Filtro são os parâmetros de GET /api/cupons.
type Filtro struct {
	Mes       string // "AAAA-MM"; vazio = todos
	ClienteID int64  // 0 = todos
	Status    dominio.Status
	Numero    string
	Limite    int
	Offset    int
}

const selecaoCupom = `
	SELECT c.id, c.numero, COALESCE(c.data::text, ''), c.cliente_id,
	       COALESCE(c.cnpj_cliente, ''), c.cliente_nome, c.emitente_cnpj,
	       c.total::text, c.status, c.incertos, c.avisos, c.fonte, c.obs,
	       c.foto_id, c.criado_em
	  FROM cupons c`

// ListarCupons aplica o filtro e devolve os cupons SEM os itens (a lista da
// tela não precisa deles).
func (s *Store) ListarCupons(ctx context.Context, f Filtro) ([]dominio.Cupom, error) {
	var cond []string
	var args []any

	add := func(expr string, arg any) {
		args = append(args, arg)
		cond = append(cond, fmt.Sprintf(expr, len(args)))
	}
	if f.Mes != "" {
		add("date_trunc('month', c.data) = date_trunc('month', $%d::date)", f.Mes+"-01")
	}
	if f.ClienteID > 0 {
		add("c.cliente_id = $%d", f.ClienteID)
	}
	if f.Status != "" {
		add("c.status = $%d", string(f.Status))
	}
	if f.Numero != "" {
		add("c.numero ILIKE $%d", "%"+f.Numero+"%")
	}

	sql := selecaoCupom
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY c.data NULLS LAST, c.numero"

	limite := f.Limite
	if limite <= 0 || limite > 500 {
		limite = 500
	}
	args = append(args, limite)
	sql += fmt.Sprintf(" LIMIT $%d", len(args))
	if f.Offset > 0 {
		args = append(args, f.Offset)
		sql += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	linhas, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []dominio.Cupom
	for linhas.Next() {
		c, err := lerLinhaCupom(linhas)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, linhas.Err()
}

// CupomPorID devolve o cupom com os itens.
func (s *Store) CupomPorID(ctx context.Context, id int64) (dominio.Cupom, error) {
	linha := s.pool.QueryRow(ctx, selecaoCupom+` WHERE c.id = $1`, id)
	c, err := lerLinhaCupom(linha)
	if err != nil {
		return dominio.Cupom{}, semLinhas(err)
	}
	c.Itens, err = s.itensDoCupom(ctx, id)
	if err != nil {
		return dominio.Cupom{}, err
	}
	return c, nil
}

// escaneavel abstrai pgx.Row e pgx.Rows, que têm o mesmo Scan.
type escaneavel interface {
	Scan(dest ...any) error
}

func lerLinhaCupom(l escaneavel) (dominio.Cupom, error) {
	var c dominio.Cupom
	var data, total string
	var criado time.Time

	err := l.Scan(&c.ID, &c.Numero, &data, &c.ClienteID, &c.CNPJCliente, &c.Cliente,
		&c.CNPJEmitente, &total, &c.Status, &c.Incertos, &c.Avisos, &c.Fonte, &c.Obs,
		&c.FotoID, &criado)
	if err != nil {
		return dominio.Cupom{}, err
	}

	c.CNPJCliente = strings.TrimSpace(c.CNPJCliente)
	c.CNPJEmitente = strings.TrimSpace(c.CNPJEmitente)
	c.CriadoEm = criado

	if data != "" {
		d, err := dominio.ParseData(data)
		if err != nil {
			return dominio.Cupom{}, fmt.Errorf("data inválida no cupom %d: %w", c.ID, err)
		}
		c.Data = d
	}
	if c.Total, err = lerValor(total); err != nil {
		return dominio.Cupom{}, fmt.Errorf("total inválido no cupom %d: %w", c.ID, err)
	}
	if c.Incertos == nil {
		c.Incertos = []string{}
	}
	if c.Avisos == nil {
		c.Avisos = []string{}
	}
	return c, nil
}

func (s *Store) itensDoCupom(ctx context.Context, cupomID int64) ([]dominio.Item, error) {
	linhas, err := s.pool.Query(ctx,
		`SELECT id, ordem, codigo, produto, unidade, qtd::text, vun::text, vtot::text, incertos
		   FROM cupom_itens WHERE cupom_id = $1 ORDER BY ordem`, cupomID)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	itens := []dominio.Item{}
	for linhas.Next() {
		var it dominio.Item
		var qtd, vun, vtot string
		if err := linhas.Scan(&it.ID, &it.Ordem, &it.Codigo, &it.Produto, &it.Unidade,
			&qtd, &vun, &vtot, &it.Incertos); err != nil {
			return nil, err
		}
		if it.Qtd, err = lerQtd(qtd); err != nil {
			return nil, fmt.Errorf("quantidade inválida no item %d: %w", it.ID, err)
		}
		if it.Vun, err = lerValor(vun); err != nil {
			return nil, fmt.Errorf("valor unitário inválido no item %d: %w", it.ID, err)
		}
		if it.Vtot, err = lerValor(vtot); err != nil {
			return nil, fmt.Errorf("valor total inválido no item %d: %w", it.ID, err)
		}
		if it.Incertos == nil {
			it.Incertos = []string{}
		}
		itens = append(itens, it)
	}
	return itens, linhas.Err()
}

// ResultadoGravacao conta o que aconteceu ao gravar um cupom.
type ResultadoGravacao struct {
	Cupom      dominio.Cupom
	Criado     bool
	Atualizado bool
	// Ignorado indica que o cupom já existia confirmado e não foi tocado.
	Ignorado bool
}

// SalvarCupom insere ou atualiza um cupom e seus itens, em transação.
//
// Regra 6.6: reimportar o mesmo (numero, cnpj_cliente) atualiza apenas se o
// existente estiver pendente. Se já estiver confirmado, nada é sobrescrito e
// Ignorado volta true, para a UI mostrar conflito.
func (s *Store) SalvarCupom(ctx context.Context, c dominio.Cupom, importacaoID *int64) (ResultadoGravacao, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ResultadoGravacao{}, err
	}
	defer tx.Rollback(ctx)

	// Procura um cupom já existente pela chave de negócio ou pelo id do legado.
	var existenteID int64
	var existenteStatus string
	achou := false

	if c.Numero != "" && c.CNPJCliente != "" {
		err := tx.QueryRow(ctx,
			`SELECT id, status FROM cupons WHERE numero = $1 AND cnpj_cliente = $2`,
			c.Numero, c.CNPJCliente).Scan(&existenteID, &existenteStatus)
		switch {
		case err == nil:
			achou = true
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return ResultadoGravacao{}, err
		}
	}

	if achou && existenteStatus == string(dominio.StatusConfirmado) {
		// Não sobrescreve cupom já conferido por um humano.
		c.ID = existenteID
		return ResultadoGravacao{Cupom: c, Ignorado: true}, tx.Commit(ctx)
	}

	clienteID := c.ClienteID
	if clienteID == nil && c.CNPJCliente != "" {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM clientes WHERE cnpj = $1`, c.CNPJCliente).Scan(&id)
		if err == nil {
			clienteID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ResultadoGravacao{}, err
		}
	}

	var dataArg *string
	if !c.Data.Zero() {
		d := c.Data.String()
		dataArg = &d
	}
	var cnpjArg *string
	if c.CNPJCliente != "" {
		cnpjArg = &c.CNPJCliente
	}

	res := ResultadoGravacao{}
	if achou {
		_, err = tx.Exec(ctx,
			`UPDATE cupons
			    SET numero = $2, data = $3::date, cliente_id = $4, cnpj_cliente = $5,
			        cliente_nome = $6, emitente_cnpj = $7, total = $8::numeric,
			        status = $9, incertos = $10, avisos = $11, fonte = $12, obs = $13,
			        foto_id = COALESCE($14, foto_id),
			        importacao_id = COALESCE($15, importacao_id),
			        atualizado_em = now()
			  WHERE id = $1`,
			existenteID, c.Numero, dataArg, clienteID, cnpjArg, c.Cliente, c.CNPJEmitente,
			valorTexto(c.Total), string(c.Status), c.Incertos, c.Avisos, c.Fonte, c.Obs,
			c.FotoID, importacaoID)
		if err != nil {
			return ResultadoGravacao{}, err
		}
		c.ID = existenteID
		res.Atualizado = true

		if _, err := tx.Exec(ctx, `DELETE FROM cupom_itens WHERE cupom_id = $1`, existenteID); err != nil {
			return ResultadoGravacao{}, err
		}
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO cupons (numero, data, cliente_id, cnpj_cliente, cliente_nome,
			                     emitente_cnpj, total, status, incertos, avisos, fonte, obs,
			                     foto_id, importacao_id)
			 VALUES ($1, $2::date, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13, $14)
			 RETURNING id`,
			c.Numero, dataArg, clienteID, cnpjArg, c.Cliente, c.CNPJEmitente,
			valorTexto(c.Total), string(c.Status), c.Incertos, c.Avisos, c.Fonte, c.Obs,
			c.FotoID, importacaoID).Scan(&c.ID)
		if ehViolacaoUnicidade(err) {
			return ResultadoGravacao{}, &ErroConflito{Msg: fmt.Sprintf(
				"o cupom nº %s deste cliente já existe", c.Numero)}
		}
		if err != nil {
			return ResultadoGravacao{}, err
		}
		res.Criado = true
	}

	if err := inserirItens(ctx, tx, c.ID, c.Itens); err != nil {
		return ResultadoGravacao{}, err
	}

	c.ClienteID = clienteID
	res.Cupom = c
	return res, tx.Commit(ctx)
}

func inserirItens(ctx context.Context, tx pgx.Tx, cupomID int64, itens []dominio.Item) error {
	for i, it := range itens {
		ordem := it.Ordem
		if ordem <= 0 {
			ordem = i + 1
		}
		un := it.Unidade
		if un != dominio.UND && un != dominio.KG {
			un = dominio.UND
		}
		incertos := it.Incertos
		if incertos == nil {
			incertos = []string{}
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO cupom_itens (cupom_id, ordem, codigo, produto, unidade, qtd, vun, vtot, incertos)
			 VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9)`,
			cupomID, ordem, it.Codigo, it.Produto, un,
			qtdTexto(it.Qtd), valorTexto(it.Vun), valorTexto(it.Vtot), incertos)
		if err != nil {
			return fmt.Errorf("item %d: %w", ordem, err)
		}
	}
	return nil
}

// MudarStatus confirma ou rejeita um cupom.
func (s *Store) MudarStatus(ctx context.Context, id int64, status dominio.Status, obs string, usuarioID *int64) error {
	var confirmadoEm *time.Time
	var confirmadoPor *int64
	if status == dominio.StatusConfirmado {
		agora := time.Now()
		confirmadoEm, confirmadoPor = &agora, usuarioID
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE cupons
		    SET status = $2,
		        obs = CASE WHEN $3 = '' THEN obs ELSE $3 END,
		        confirmado_em = $4, confirmado_por = $5, atualizado_em = now()
		  WHERE id = $1`,
		id, string(status), obs, confirmadoEm, confirmadoPor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErroNaoEncontrado
	}
	return nil
}

// ContarPorStatus devolve quantos cupons há em cada status no mês/cliente —
// é o que a tela de Cobrança usa para avisar "faltam N a conferir".
func (s *Store) ContarPorStatus(ctx context.Context, mes string, clienteID int64) (map[dominio.Status]int, error) {
	var cond []string
	var args []any
	if mes != "" {
		args = append(args, mes+"-01")
		cond = append(cond, fmt.Sprintf("date_trunc('month', data) = date_trunc('month', $%d::date)", len(args)))
	}
	if clienteID > 0 {
		args = append(args, clienteID)
		cond = append(cond, fmt.Sprintf("cliente_id = $%d", len(args)))
	}
	sql := `SELECT status, count(*) FROM cupons`
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " GROUP BY status"

	linhas, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	out := map[dominio.Status]int{}
	for linhas.Next() {
		var st string
		var n int
		if err := linhas.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[dominio.Status(st)] = n
	}
	return out, linhas.Err()
}

// NumerosParecidos procura, no mesmo mês e em OUTRO cliente, cupons com
// número a uma edição de distância do informado.
//
// Existe por causa do caso real levantado no CLAUDE.md §10: o cupom 1342024
// (Maternidade) e o 1342014 (Dona Íris) diferem em um dígito e geraram
// suspeita de duplicidade. Não são duplicata — são clientes diferentes — mas
// a semelhança merece um aviso na revisão.
//
// A comparação é feita em Go: depender de fuzzystrmatch no banco tornaria a
// extensão um pré-requisito de deploy por causa de um aviso de conforto.
func (s *Store) NumerosParecidos(ctx context.Context, numero, cnpjCliente, mes string) ([]dominio.Cupom, error) {
	if numero == "" || mes == "" {
		return nil, nil
	}
	linhas, err := s.pool.Query(ctx,
		`SELECT c.id, c.numero, COALESCE(c.data::text, ''), c.cliente_nome, COALESCE(c.cnpj_cliente, '')
		   FROM cupons c
		  WHERE date_trunc('month', c.data) = date_trunc('month', $1::date)
		    AND c.numero <> ''
		    AND c.numero <> $2
		    AND COALESCE(c.cnpj_cliente, '') <> $3
		    AND length(c.numero) = length($2)`,
		mes+"-01", numero, cnpjCliente)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []dominio.Cupom
	for linhas.Next() {
		var c dominio.Cupom
		var data string
		if err := linhas.Scan(&c.ID, &c.Numero, &data, &c.Cliente, &c.CNPJCliente); err != nil {
			return nil, err
		}
		if difereEmUmDigito(c.Numero, numero) {
			if data != "" {
				if d, err := dominio.ParseData(data); err == nil {
					c.Data = d
				}
			}
			c.CNPJCliente = strings.TrimSpace(c.CNPJCliente)
			out = append(out, c)
		}
	}
	return out, linhas.Err()
}

// difereEmUmDigito informa se dois números de mesmo tamanho diferem em
// exatamente uma posição.
func difereEmUmDigito(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
			if n > 1 {
				return false
			}
		}
	}
	return n == 1
}
