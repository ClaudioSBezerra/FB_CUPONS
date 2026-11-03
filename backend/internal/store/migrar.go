package store

import (
	"database/sql"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // registra o driver "pgx" no database/sql
)

// iofsFS é o contrato que o golang-migrate espera do sistema de arquivos.
type iofsFS = fs.FS

// openStdlib abre uma conexão database/sql usando o driver pgx — é o que o
// golang-migrate precisa, e o pool pgxpool não serve para isso.
func openStdlib(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("não foi possível abrir conexão para migration: %w", err)
	}
	return db, nil
}
