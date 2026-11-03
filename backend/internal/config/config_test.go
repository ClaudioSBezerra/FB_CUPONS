package config

import (
	"net/url"
	"strings"
	"testing"
)

func TestMontarDSNEscapaSenha(t *testing.T) {
	// Senhas que quebram uma URL montada por interpolação de string. A do
	// meio foi a que derrubou o primeiro deploy no Coolify.
	casos := []struct {
		nome  string
		senha string
	}{
		{"simples", "senha123"},
		{"com espaço", "minha senha secreta"},
		{"com arroba", "se@nha"},
		{"com barra", "se/nha"},
		{"com dois-pontos", "se:nha"},
		{"com interrogação", "se?nha"},
		{"com cerquilha", "se#nha"},
		{"base64 com + e =", "aB3+xY9/zQ=="},
		{"tudo junto", "a b@c/d:e?f#g+h=i%j"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			dsn := montarDSN("postgres", "5432", "cupons", c.senha, "cupons", "disable")

			if err := conferirDSN(dsn); err != nil {
				t.Fatalf("DSN gerada é inválida: %v", err)
			}

			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("url.Parse falhou: %v", err)
			}
			senha, temSenha := u.User.Password()
			if !temSenha {
				t.Fatal("a DSN saiu sem senha")
			}
			// O ponto do teste: a senha volta exatamente como entrou.
			if senha != c.senha {
				t.Errorf("senha = %q, quer %q", senha, c.senha)
			}
			if u.User.Username() != "cupons" {
				t.Errorf("usuário = %q", u.User.Username())
			}
			if u.Host != "postgres:5432" {
				t.Errorf("host = %q", u.Host)
			}
			if u.Path != "/cupons" {
				t.Errorf("banco = %q", u.Path)
			}
			if u.Query().Get("sslmode") != "disable" {
				t.Errorf("sslmode = %q", u.Query().Get("sslmode"))
			}
		})
	}
}

func TestMontarDSNHostIPv6(t *testing.T) {
	dsn := montarDSN("::1", "5432", "cupons", "x", "cupons", "disable")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "[::1]:5432" {
		t.Errorf("host IPv6 = %q, quer [::1]:5432", u.Host)
	}
}

func TestConferirDSN(t *testing.T) {
	casos := []struct {
		nome string
		dsn  string
		erro bool
	}{
		{"url válida", "postgres://u:p@host:5432/db?sslmode=disable", false},
		{"postgresql://", "postgresql://u:p@host:5432/db", false},
		{"senha com espaço cru", "postgres://u:minha senha@host:5432/db", true},
		{"esquema errado", "mysql://u:p@host/db", true},
		{"sem host", "postgres:///db", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := conferirDSN(c.dsn)
			if (err != nil) != c.erro {
				t.Fatalf("conferirDSN(%q) erro = %v, quer erro = %v", c.dsn, err, c.erro)
			}
			// A mensagem precisa dizer o que fazer, não só que falhou.
			if c.erro && err != nil && strings.Contains(c.dsn, " ") {
				if !strings.Contains(err.Error(), "DB_SENHA") {
					t.Errorf("mensagem não aponta a saída: %v", err)
				}
			}
		})
	}
}

func TestCarregarMontaDSNDasPartes(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_USUARIO", "cupons")
	t.Setenv("DB_SENHA", "senha com espaço e @")
	t.Setenv("DB_NOME", "cupons")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))

	c, err := Carregar()
	if err != nil {
		t.Fatalf("Carregar falhou: %v", err)
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	senha, _ := u.User.Password()
	if senha != "senha com espaço e @" {
		t.Errorf("senha = %q", senha)
	}
}

func TestCarregarPrefereDATABASEURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://a:b@outro:5432/outro")
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_SENHA", "ignorada")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))

	c, err := Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.DatabaseURL, "outro") {
		t.Errorf("DATABASE_URL explícita foi ignorada: %q", c.DatabaseURL)
	}
}

func TestCarregarRecusaJWTCurto(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@host:5432/db")
	t.Setenv("JWT_SECRET", "curto")

	if _, err := Carregar(); err == nil {
		t.Error("JWT_SECRET curto deveria ser recusado")
	}
}

func TestLeituraHabilitada(t *testing.T) {
	if (Config{AnthropicAPIKey: ""}).LeituraHabilitada() {
		t.Error("sem chave, a leitura não está habilitada")
	}
	if !(Config{AnthropicAPIKey: "sk-ant-x"}).LeituraHabilitada() {
		t.Error("com chave, a leitura está habilitada")
	}
}
