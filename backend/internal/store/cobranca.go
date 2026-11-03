package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// LinhaCobranca é uma linha da cobrança mensal: um produto de um cliente.
type LinhaCobranca struct {
	Mes         string          `json:"mes"`
	ClienteID   int64           `json:"cliente_id"`
	Cliente     string          `json:"cliente"`
	CNPJCliente string          `json:"cnpj_cliente"`
	Codigo      string          `json:"codigo"`
	Produto     string          `json:"produto"`
	Unidade     dominio.Unidade `json:"unidade"`
	QtdTotal    dominio.Qtd     `json:"qtd_total"`
	NumCupons   int             `json:"num_cupons"`
	ValorTotal  dominio.Valor   `json:"valor_total"`
}

// Cobranca é o resultado completo de GET /api/cobranca.
type Cobranca struct {
	Mes       string          `json:"mes"`
	Linhas    []LinhaCobranca `json:"linhas"`
	Subtotais []Subtotal      `json:"subtotais"`
	Total     dominio.Valor   `json:"total"`
	// Pendentes conta cupons que ainda não entraram por não estarem
	// confirmados — a tela avisa quantos faltam.
	Pendentes  int `json:"pendentes"`
	Rejeitados int `json:"rejeitados"`
}

// Subtotal é o total de um cliente no mês.
type Subtotal struct {
	ClienteID   int64         `json:"cliente_id"`
	Cliente     string        `json:"cliente"`
	CNPJCliente string        `json:"cnpj_cliente"`
	NumCupons   int           `json:"num_cupons"`
	Total       dominio.Valor `json:"total"`
}

// Cobrancar agrega os cupons CONFIRMADOS do mês por cliente e produto.
//
// Somente status 'confirmado' entra (CLAUDE.md §9). Pendentes ficam de fora e
// são contados em Pendentes para a tela avisar.
//
// O agrupamento é por (cliente, produto, unidade) — e não por código —
// porque nos dados reais há itens sem código legível que precisam aparecer na
// cobrança, e porque o mesmo produto pode ter sido lido com código ilegível
// em um cupom e legível em outro.
func (s *Store) Cobrancar(ctx context.Context, mes string, clienteID int64) (Cobranca, error) {
	if mes == "" {
		return Cobranca{}, fmt.Errorf("informe o mês da cobrança no formato AAAA-MM")
	}

	args := []any{mes + "-01"}
	cond := []string{
		"c.status = 'confirmado'",
		"date_trunc('month', c.data) = date_trunc('month', $1::date)",
	}
	if clienteID > 0 {
		args = append(args, clienteID)
		cond = append(cond, fmt.Sprintf("c.cliente_id = $%d", len(args)))
	}

	sql := `
	SELECT COALESCE(c.cliente_id, 0),
	       c.cliente_nome,
	       COALESCE(c.cnpj_cliente, ''),
	       i.codigo,
	       i.produto,
	       i.unidade,
	       sum(i.qtd)::text,
	       count(DISTINCT c.id),
	       sum(i.vtot)::text
	  FROM cupons c
	  JOIN cupom_itens i ON i.cupom_id = c.id
	 WHERE ` + strings.Join(cond, " AND ") + `
	 GROUP BY 1, 2, 3, 4, 5, 6
	 ORDER BY c.cliente_nome, sum(i.vtot) DESC, i.produto`

	linhas, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return Cobranca{}, err
	}
	defer linhas.Close()

	cob := Cobranca{Mes: mes, Linhas: []LinhaCobranca{}, Subtotais: []Subtotal{}}
	porCliente := map[int64]*Subtotal{}

	for linhas.Next() {
		var l LinhaCobranca
		var qtd, valor string
		if err := linhas.Scan(&l.ClienteID, &l.Cliente, &l.CNPJCliente, &l.Codigo,
			&l.Produto, &l.Unidade, &qtd, &l.NumCupons, &valor); err != nil {
			return Cobranca{}, err
		}
		if l.QtdTotal, err = lerQtd(qtd); err != nil {
			return Cobranca{}, fmt.Errorf("quantidade agregada inválida em %q: %w", l.Produto, err)
		}
		if l.ValorTotal, err = lerValor(valor); err != nil {
			return Cobranca{}, fmt.Errorf("valor agregado inválido em %q: %w", l.Produto, err)
		}
		l.Mes = mes
		l.CNPJCliente = strings.TrimSpace(l.CNPJCliente)
		cob.Linhas = append(cob.Linhas, l)

		st, ok := porCliente[l.ClienteID]
		if !ok {
			st = &Subtotal{ClienteID: l.ClienteID, Cliente: l.Cliente, CNPJCliente: l.CNPJCliente}
			porCliente[l.ClienteID] = st
			cob.Subtotais = append(cob.Subtotais, Subtotal{}) // posição reservada
		}
		st.Total += l.ValorTotal
		cob.Total += l.ValorTotal
	}
	if err := linhas.Err(); err != nil {
		return Cobranca{}, err
	}

	// Número de cupons confirmados por cliente, contado sobre os cupons (não
	// sobre os itens, que repetiriam o mesmo cupom).
	cob.Subtotais = cob.Subtotais[:0]
	vistos := map[int64]bool{}
	for _, l := range cob.Linhas {
		if vistos[l.ClienteID] {
			continue
		}
		vistos[l.ClienteID] = true
		st := porCliente[l.ClienteID]
		n, err := s.contarCuponsConfirmados(ctx, mes, l.ClienteID)
		if err != nil {
			return Cobranca{}, err
		}
		st.NumCupons = n
		cob.Subtotais = append(cob.Subtotais, *st)
	}

	contagem, err := s.ContarPorStatus(ctx, mes, clienteID)
	if err != nil {
		return Cobranca{}, err
	}
	cob.Pendentes = contagem[dominio.StatusPendente]
	cob.Rejeitados = contagem[dominio.StatusRejeitado]

	return cob, nil
}

func (s *Store) contarCuponsConfirmados(ctx context.Context, mes string, clienteID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM cupons
		  WHERE status = 'confirmado'
		    AND date_trunc('month', data) = date_trunc('month', $1::date)
		    AND COALESCE(cliente_id, 0) = $2`,
		mes+"-01", clienteID).Scan(&n)
	return n, err
}

// ItemExportacao é uma linha da aba "Itens" do Excel.
type ItemExportacao struct {
	Status      dominio.Status
	Data        dominio.Data
	Numero      string
	Cliente     string
	CNPJCliente string
	Codigo      string
	Produto     string
	Unidade     dominio.Unidade
	Qtd         dominio.Qtd
	Vun         dominio.Valor
	Vtot        dominio.Valor
}

// ItensDoMes devolve um registro por item de cupom, para a aba "Itens".
// Inclui pendentes, com o status em cada linha — assim o Excel mostra o que
// ainda falta conferir, como no arquivo de referência do protótipo.
func (s *Store) ItensDoMes(ctx context.Context, mes string, clienteID int64) ([]ItemExportacao, error) {
	args := []any{mes + "-01"}
	cond := []string{
		"c.status <> 'rejeitado'",
		"date_trunc('month', c.data) = date_trunc('month', $1::date)",
	}
	if clienteID > 0 {
		args = append(args, clienteID)
		cond = append(cond, fmt.Sprintf("c.cliente_id = $%d", len(args)))
	}

	linhas, err := s.pool.Query(ctx, `
		SELECT c.status, COALESCE(c.data::text, ''), c.numero, c.cliente_nome,
		       COALESCE(c.cnpj_cliente, ''), i.codigo, i.produto, i.unidade,
		       i.qtd::text, i.vun::text, i.vtot::text
		  FROM cupons c
		  JOIN cupom_itens i ON i.cupom_id = c.id
		 WHERE `+strings.Join(cond, " AND ")+`
		 ORDER BY c.cliente_nome, c.data, c.numero, i.ordem`, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []ItemExportacao
	for linhas.Next() {
		var e ItemExportacao
		var data, qtd, vun, vtot string
		if err := linhas.Scan(&e.Status, &data, &e.Numero, &e.Cliente, &e.CNPJCliente,
			&e.Codigo, &e.Produto, &e.Unidade, &qtd, &vun, &vtot); err != nil {
			return nil, err
		}
		if data != "" {
			if d, err := dominio.ParseData(data); err == nil {
				e.Data = d
			}
		}
		if e.Qtd, err = lerQtd(qtd); err != nil {
			return nil, err
		}
		if e.Vun, err = lerValor(vun); err != nil {
			return nil, err
		}
		if e.Vtot, err = lerValor(vtot); err != nil {
			return nil, err
		}
		e.CNPJCliente = strings.TrimSpace(e.CNPJCliente)
		out = append(out, e)
	}
	return out, linhas.Err()
}

// CupomExportacao é uma linha da aba "Cupons" do Excel.
type CupomExportacao struct {
	Status      dominio.Status
	Data        dominio.Data
	Numero      string
	Cliente     string
	CNPJCliente string
	NumItens    int
	Total       dominio.Valor
	Obs         string
}

// CuponsDoMes devolve os cupons do mês para a aba "Cupons".
func (s *Store) CuponsDoMes(ctx context.Context, mes string, clienteID int64) ([]CupomExportacao, error) {
	args := []any{mes + "-01"}
	cond := []string{
		"c.status <> 'rejeitado'",
		"date_trunc('month', c.data) = date_trunc('month', $1::date)",
	}
	if clienteID > 0 {
		args = append(args, clienteID)
		cond = append(cond, fmt.Sprintf("c.cliente_id = $%d", len(args)))
	}

	linhas, err := s.pool.Query(ctx, `
		SELECT c.status, COALESCE(c.data::text, ''), c.numero, c.cliente_nome,
		       COALESCE(c.cnpj_cliente, ''),
		       (SELECT count(*) FROM cupom_itens i WHERE i.cupom_id = c.id),
		       c.total::text, c.obs
		  FROM cupons c
		 WHERE `+strings.Join(cond, " AND ")+`
		 ORDER BY c.cliente_nome, c.data, c.numero`, args...)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	var out []CupomExportacao
	for linhas.Next() {
		var e CupomExportacao
		var data, total string
		if err := linhas.Scan(&e.Status, &data, &e.Numero, &e.Cliente, &e.CNPJCliente,
			&e.NumItens, &total, &e.Obs); err != nil {
			return nil, err
		}
		if data != "" {
			if d, err := dominio.ParseData(data); err == nil {
				e.Data = d
			}
		}
		if e.Total, err = lerValor(total); err != nil {
			return nil, err
		}
		e.CNPJCliente = strings.TrimSpace(e.CNPJCliente)
		out = append(out, e)
	}
	return out, linhas.Err()
}
