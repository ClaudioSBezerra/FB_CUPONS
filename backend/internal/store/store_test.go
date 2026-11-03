package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestExplicarErroConexao(t *testing.T) {
	casos := []struct {
		nome      string
		err       error
		querTexto []string
	}{
		{
			// O caso que custou um deploy: volume antigo guarda a senha da
			// primeira inicialização, e trocar POSTGRES_PASSWORD não muda nada.
			nome: "senha recusada explica o volume antigo",
			err:  &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "cupons"`},
			querTexto: []string{
				"POSTGRES_PASSWORD só tem efeito na PRIMEIRA inicialização",
				"ALTER USER",
				"apagar o volume",
			},
		},
		{
			nome:      "banco inexistente aponta DB_NOME",
			err:       &pgconn.PgError{Code: "3D000", Message: `database "errado" does not exist`},
			querTexto: []string{"não existe", "DB_NOME"},
		},
		{
			nome:      "usuário sem autorização aponta DB_USUARIO",
			err:       &pgconn.PgError{Code: "28000", Message: "no pg_hba.conf entry"},
			querTexto: []string{"DB_USUARIO"},
		},
		{
			nome:      "erro desconhecido não inventa explicação",
			err:       errors.New("connection refused"),
			querTexto: []string{"o banco não respondeu", "connection refused"},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := ExplicarErroConexao(c.err)
			if got == nil {
				t.Fatal("devolveu nil")
			}
			for _, esperado := range c.querTexto {
				if !strings.Contains(got.Error(), esperado) {
					t.Errorf("mensagem não contém %q:\n%v", esperado, got)
				}
			}
			// O erro original tem de continuar encadeado, para o log.
			if !errors.Is(got, c.err) {
				t.Errorf("o erro original foi perdido: %v", got)
			}
		})
	}
}

func TestExplicarErroConexaoEnvolvido(t *testing.T) {
	// Na prática o erro vem embrulhado pelo driver.
	original := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
	envolvido := fmt.Errorf("failed to connect: %w", original)

	got := ExplicarErroConexao(envolvido)
	if !strings.Contains(got.Error(), "PRIMEIRA inicialização") {
		t.Errorf("não reconheceu o 28P01 embrulhado: %v", got)
	}
}

func TestEhViolacaoUnicidade(t *testing.T) {
	if !ehViolacaoUnicidade(&pgconn.PgError{Code: "23505"}) {
		t.Error("23505 é violação de unicidade")
	}
	if ehViolacaoUnicidade(&pgconn.PgError{Code: "23503"}) {
		t.Error("23503 é chave estrangeira, não unicidade")
	}
	if ehViolacaoUnicidade(errors.New("outro")) {
		t.Error("erro comum não é violação de unicidade")
	}
}

func TestValorEQtdAtravessamOBancoComoTexto(t *testing.T) {
	// A fronteira com o Postgres é texto com ponto decimal: se isto mudar,
	// o ::numeric recebe formato errado e os centavos somem.
	casos := []struct {
		entrada string
		quer    string
	}{
		{"7.225,77", "7225.77"},
		{"0,01", "0.01"},
		{"18.726,23", "18726.23"},
	}
	for _, c := range casos {
		v, err := lerValor(c.quer)
		if err != nil {
			t.Fatal(err)
		}
		if got := valorTexto(v); got != c.quer {
			t.Errorf("valorTexto(%s) = %q, quer %q", c.entrada, got, c.quer)
		}
	}

	q, err := lerQtd("1.340")
	if err != nil {
		t.Fatal(err)
	}
	if got := qtdTexto(q); got != "1.340" {
		t.Errorf("qtdTexto = %q, quer 1.340", got)
	}
}

func TestCondCompetenciaUsaIntervaloSemiaberto(t *testing.T) {
	// date_trunc() na coluna impediria o índice e dependeria do fuso.
	cond := fmt.Sprintf(condCompetencia("c.data"), 1)
	if strings.Contains(cond, "date_trunc") {
		t.Errorf("a condição voltou a usar date_trunc: %s", cond)
	}
	for _, esperado := range []string{"c.data >= $1::date", "c.data < ($1::date + INTERVAL '1 month')"} {
		if !strings.Contains(cond, esperado) {
			t.Errorf("condição %q não contém %q", cond, esperado)
		}
	}
}

func TestDifereEmUmDigito(t *testing.T) {
	// Caso real: 1342024 (Maternidade) vs 1342014 (Dona Íris).
	if !difereEmUmDigito("1342024", "1342014") {
		t.Error("1342024 e 1342014 diferem em um dígito")
	}
	if difereEmUmDigito("1342024", "1342024") {
		t.Error("iguais não diferem em um dígito")
	}
	if difereEmUmDigito("1342024", "1341914") {
		t.Error("dois dígitos de diferença não conta")
	}
	if difereEmUmDigito("1342024", "134202") {
		t.Error("tamanhos diferentes não comparam")
	}
}
