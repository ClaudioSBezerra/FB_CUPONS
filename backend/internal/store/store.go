// Package store concentra o acesso ao PostgreSQL.
//
// Valores monetários e quantidades atravessam a fronteira do banco como
// TEXTO, convertido com $n::numeric na ida e com ::text na volta. Passar por
// float64 em qualquer ponto reintroduziria o erro de arredondamento que o
// tipo dominio.Valor existe para evitar.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

// ErroNaoEncontrado é devolvido quando o registro pedido não existe.
var ErroNaoEncontrado = errors.New("registro não encontrado")

// ErroConflito indica violação de unicidade — tipicamente reimportação de um
// cupom já confirmado.
type ErroConflito struct{ Msg string }

func (e *ErroConflito) Error() string { return e.Msg }

// Store é o repositório de dados da aplicação.
type Store struct {
	pool *pgxpool.Pool
}

// Abrir conecta ao Postgres e confere a conexão.
func Abrir(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("não foi possível conectar ao banco: %w", err)
	}

	ctxPing, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(ctxPing); err != nil {
		pool.Close()
		return nil, fmt.Errorf("o banco não respondeu: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Fechar encerra o pool.
func (s *Store) Fechar() { s.pool.Close() }

// Ping confere se o banco responde — usado no /healthz.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Pool expõe o pool para quem precisa de transação própria.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// MigrarFS aplica as migrations pendentes.
func MigrarFS(dsn string, fsys iofsFS, caminho string) error {
	fonte, err := iofs.New(fsys, caminho)
	if err != nil {
		return fmt.Errorf("não foi possível ler as migrations: %w", err)
	}

	db, err := openStdlib(dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("driver de migration: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", fonte, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("falha ao aplicar as migrations: %w", err)
	}
	return nil
}

// ehViolacaoUnicidade identifica o erro 23505 do Postgres.
func ehViolacaoUnicidade(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// semLinhas normaliza pgx.ErrNoRows em ErroNaoEncontrado.
func semLinhas(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErroNaoEncontrado
	}
	return err
}

// valorTexto devolve o valor no formato que o Postgres aceita em ::numeric
// (ponto decimal, sem separador de milhar).
func valorTexto(v dominio.Valor) string {
	b, _ := v.MarshalJSON()
	return string(b)
}

// qtdTexto idem para quantidade, com 3 decimais.
func qtdTexto(q dominio.Qtd) string {
	b, _ := q.MarshalJSON()
	return string(b)
}

// lerValor converte o texto vindo de ::text em dominio.Valor.
func lerValor(s string) (dominio.Valor, error) {
	if s == "" {
		return 0, nil
	}
	return dominio.ParseValor(s)
}

// lerQtd idem para quantidade.
func lerQtd(s string) (dominio.Qtd, error) {
	if s == "" {
		return 0, nil
	}
	return dominio.ParseQtd(s)
}
