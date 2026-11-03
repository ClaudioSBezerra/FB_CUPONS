package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// Importacao é um lote de upload (um ou mais PDFs/imagens).
type Importacao struct {
	ID           int64           `json:"id"`
	Arquivos     []string        `json:"arquivos"`
	TotalPaginas int             `json:"total_paginas"`
	Lidas        int             `json:"lidas"`
	ComPendencia int             `json:"com_pendencia"`
	Duplicadas   int             `json:"duplicadas"`
	Erros        int             `json:"erros"`
	Status       string          `json:"status"`
	Detalhe      []DetalhePagina `json:"detalhe"`
	CriadoEm     time.Time       `json:"criado_em"`
	FinalizadoEm *time.Time      `json:"finalizado_em"`
}

// DetalhePagina é o resultado de uma página do lote, mostrado na tela de
// Importar.
type DetalhePagina struct {
	Arquivo  string         `json:"arquivo"`
	Pagina   int            `json:"pagina"`
	Situacao string         `json:"situacao"` // "lido", "duplicado", "erro"
	CupomID  int64          `json:"cupom_id,omitempty"`
	Numero   string         `json:"numero,omitempty"`
	Cliente  string         `json:"cliente,omitempty"`
	Total    *dominio.Valor `json:"total,omitempty"`
	Avisos   []string       `json:"avisos,omitempty"`
	Erro     string         `json:"erro,omitempty"`
}

// CriarImportacao abre o lote antes de processar as páginas.
func (s *Store) CriarImportacao(ctx context.Context, arquivos []string, totalPaginas int, usuarioID *int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO importacoes (arquivos, total_paginas, usuario_id)
		 VALUES ($1, $2, $3) RETURNING id`,
		arquivos, totalPaginas, usuarioID).Scan(&id)
	return id, err
}

// FinalizarImportacao grava o resultado consolidado do lote.
func (s *Store) FinalizarImportacao(ctx context.Context, imp Importacao) error {
	detalhe, err := json.Marshal(imp.Detalhe)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE importacoes
		    SET total_paginas = $2, lidas = $3, com_pendencia = $4, duplicadas = $5,
		        erros = $6, status = $7, detalhe = $8, finalizado_em = now()
		  WHERE id = $1`,
		imp.ID, imp.TotalPaginas, imp.Lidas, imp.ComPendencia, imp.Duplicadas,
		imp.Erros, imp.Status, detalhe)
	return err
}

// ImportacaoPorID devolve o lote, para a tela acompanhar o andamento.
func (s *Store) ImportacaoPorID(ctx context.Context, id int64) (Importacao, error) {
	var imp Importacao
	var detalhe []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, arquivos, total_paginas, lidas, com_pendencia, duplicadas, erros,
		        status, detalhe, criado_em, finalizado_em
		   FROM importacoes WHERE id = $1`, id,
	).Scan(&imp.ID, &imp.Arquivos, &imp.TotalPaginas, &imp.Lidas, &imp.ComPendencia,
		&imp.Duplicadas, &imp.Erros, &imp.Status, &detalhe, &imp.CriadoEm, &imp.FinalizadoEm)
	if err != nil {
		return Importacao{}, semLinhas(err)
	}
	if len(detalhe) > 0 {
		if err := json.Unmarshal(detalhe, &imp.Detalhe); err != nil {
			return Importacao{}, err
		}
	}
	if imp.Detalhe == nil {
		imp.Detalhe = []DetalhePagina{}
	}
	return imp, nil
}

// RegistroLeitura é uma entrada do log de auditoria/custo.
type RegistroLeitura struct {
	FotoID        *int64
	CupomID       *int64
	ImportacaoID  *int64
	Modelo        string
	TokensEntrada int64
	TokensSaida   int64
	Duracao       time.Duration
	RespostaBruta json.RawMessage
	Erro          string
}

// RegistrarLeitura grava uma chamada ao Claude.
func (s *Store) RegistrarLeitura(ctx context.Context, r RegistroLeitura) error {
	var bruta *string
	if len(r.RespostaBruta) > 0 && json.Valid(r.RespostaBruta) {
		t := string(r.RespostaBruta)
		bruta = &t
	}
	var erro *string
	if r.Erro != "" {
		erro = &r.Erro
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO leituras_log (foto_id, cupom_id, importacao_id, modelo,
		                           tokens_in, tokens_out, duracao_ms, resposta_bruta, erro)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`,
		r.FotoID, r.CupomID, r.ImportacaoID, r.Modelo,
		r.TokensEntrada, r.TokensSaida, r.Duracao.Milliseconds(), bruta, erro)
	return err
}

// UsoDiario é o consumo de API de um dia.
type UsoDiario struct {
	Dia           string `json:"dia"`
	Leituras      int    `json:"leituras"`
	TokensEntrada int64  `json:"tokens_entrada"`
	TokensSaida   int64  `json:"tokens_saida"`
	Erros         int    `json:"erros"`
}

// UsoPorDia alimenta a tela de Admin e o limite de custo.
func (s *Store) UsoPorDia(ctx context.Context, dias int) ([]UsoDiario, error) {
	if dias <= 0 {
		dias = 30
	}
	linhas, err := s.pool.Query(ctx,
		`SELECT to_char(date_trunc('day', criado_em), 'YYYY-MM-DD'),
		        count(*), COALESCE(sum(tokens_in), 0), COALESCE(sum(tokens_out), 0),
		        count(*) FILTER (WHERE erro IS NOT NULL)
		   FROM leituras_log
		  WHERE criado_em >= now() - make_interval(days => $1)
		  GROUP BY 1 ORDER BY 1 DESC`, dias)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	out := []UsoDiario{}
	for linhas.Next() {
		var u UsoDiario
		if err := linhas.Scan(&u.Dia, &u.Leituras, &u.TokensEntrada, &u.TokensSaida, &u.Erros); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, linhas.Err()
}

// LeiturasHoje conta as chamadas feitas no dia, para o limite diário.
func (s *Store) LeiturasHoje(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM leituras_log
		  WHERE criado_em >= date_trunc('day', now()) AND erro IS NULL`).Scan(&n)
	return n, err
}
