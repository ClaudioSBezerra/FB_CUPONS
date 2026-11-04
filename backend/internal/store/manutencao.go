package store

import (
	"context"
	"fmt"
)

// ContagemApagada diz quantas linhas de cada tabela foram removidas.
type ContagemApagada struct {
	Cupons      int64 `json:"cupons"`
	Itens       int64 `json:"itens"`
	Fotos       int64 `json:"fotos"`
	Importacoes int64 `json:"importacoes"`
	Leituras    int64 `json:"leituras"`
}

// ZerarMovimento apaga TODO o movimento — cupons, itens, fotos, importações e
// o log de leituras — preservando os cadastros.
//
// O que fica de pé, de propósito:
//
//   - clientes e produtos: são cadastro curado por humano, inclusive os
//     produtos marcados "a confirmar". Apagá-los desfaria o seed e o trabalho
//     de conferência de nome com o cliente.
//   - usuarios: apagar trancaria o operador fora do sistema.
//
// As fotos entram na limpeza porque a importação deduplica por SHA-256: sem
// removê-las, reimportar o mesmo PDF seria recusado como "já importada" e o
// teste não exercitaria a leitura.
//
// Roda em transação: ou apaga tudo, ou nada.
func (s *Store) ZerarMovimento(ctx context.Context) (ContagemApagada, error) {
	var c ContagemApagada

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)

	// Ordem importa por causa das chaves estrangeiras.
	passos := []struct {
		nome  string
		sql   string
		conta *int64
	}{
		{"leituras_log", `DELETE FROM leituras_log`, &c.Leituras},
		{"cupom_itens", `DELETE FROM cupom_itens`, &c.Itens},
		{"cupons", `DELETE FROM cupons`, &c.Cupons},
		{"importacoes", `DELETE FROM importacoes`, &c.Importacoes},
		{"cupom_fotos", `DELETE FROM cupom_fotos`, &c.Fotos},
	}
	for _, p := range passos {
		tag, err := tx.Exec(ctx, p.sql)
		if err != nil {
			return ContagemApagada{}, fmt.Errorf("falha ao limpar %s: %w", p.nome, err)
		}
		*p.conta = tag.RowsAffected()
	}

	// Sequências voltam ao início, para os ids do teste começarem em 1.
	for _, seq := range []string{
		"cupons_id_seq", "cupom_itens_id_seq", "cupom_fotos_id_seq",
		"importacoes_id_seq", "leituras_log_id_seq",
	} {
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH 1", seq)); err != nil {
			return ContagemApagada{}, fmt.Errorf("falha ao reiniciar a sequência %s: %w", seq, err)
		}
	}

	return c, tx.Commit(ctx)
}
