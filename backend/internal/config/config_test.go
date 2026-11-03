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
				if !strings.Contains(err.Error(), "DB_PASSWORD") {
					t.Errorf("mensagem não aponta a saída: %v", err)
				}
			}
		})
	}
}

func TestCarregarMontaDSNDasPartes(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_USER", "cupons")
	t.Setenv("DB_PASSWORD", "senha com espaço e @")
	t.Setenv("DB_NAME", "cupons")
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
	t.Setenv("DB_PASSWORD", "ignorada")
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

func TestCarregarAcumulaTodosOsProblemas(t *testing.T) {
	// O ponto: um deploy deve revelar TUDO o que está errado, não um problema
	// por reinício. Aqui faltam banco, JWT e CORS de produção ao mesmo tempo.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("AMBIENTE", "producao")
	t.Setenv("CORS_ORIGENS", "")

	_, err := Carregar()
	if err == nil {
		t.Fatal("deveria recusar a configuração")
	}
	msg := err.Error()
	for _, esperado := range []string{"3 problema(s)", "DB_PASSWORD", "JWT_SECRET", "CORS_ORIGENS", "1.", "2.", "3."} {
		if !strings.Contains(msg, esperado) {
			t.Errorf("mensagem não contém %q:\n%s", esperado, msg)
		}
	}
}

func TestSenhaDoBancoVaziaSoEhProblemaEmProducao(t *testing.T) {
	// Em desenvolvimento, autenticação sem senha é legítima.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("AMBIENTE", "desenvolvimento")

	if _, err := Carregar(); err != nil {
		t.Errorf("em desenvolvimento, senha de banco vazia não deveria impedir: %v", err)
	}

	t.Setenv("AMBIENTE", "producao")
	t.Setenv("CORS_ORIGENS", "https://cupom.fbtax.cloud")
	_, err := Carregar()
	if err == nil {
		t.Fatal("em produção, DB_PASSWORD vazia deveria ser apontada")
	}
	if !strings.Contains(err.Error(), "DB_PASSWORD") {
		t.Errorf("mensagem não aponta DB_PASSWORD: %v", err)
	}
}

func TestSenhaAdminNaoImpedeASubida(t *testing.T) {
	// Senha de bootstrap curta é problema de conveniência, não de serviço:
	// a API tem de subir e o cmd/api sorteia uma senha.
	t.Setenv("DATABASE_URL", "postgres://u:p@host:5432/db")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("ADMIN_EMAIL", "alguem@exemplo.com")
	t.Setenv("ADMIN_SENHA", "123")

	c, err := Carregar()
	if err != nil {
		t.Fatalf("senha de admin curta não deveria impedir a subida: %v", err)
	}
	if c.SenhaAdminUtilizavel() {
		t.Error("senha de 3 caracteres não deveria ser considerada utilizável")
	}

	t.Setenv("ADMIN_SENHA", "senhaforte123")
	c, err = Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if !c.SenhaAdminUtilizavel() {
		t.Error("senha de 13 caracteres deveria ser utilizável")
	}
}

func TestAmbienteAceitaENVIRONMENT(t *testing.T) {
	// Os outros projetos FB usam ENVIRONMENT=production.
	t.Setenv("DATABASE_URL", "postgres://u:p@host:5432/db")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("CORS_ORIGENS", "https://cupom.fbtax.cloud")
	t.Setenv("AMBIENTE", "")
	t.Setenv("ENVIRONMENT", "production")

	c, err := Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Producao() {
		t.Errorf("ENVIRONMENT=production deveria dar produção, veio %q", c.Ambiente)
	}

	// AMBIENTE explícito tem precedência.
	t.Setenv("AMBIENTE", "desenvolvimento")
	c, err = Carregar()
	if err != nil {
		t.Fatal(err)
	}
	if c.Producao() {
		t.Error("AMBIENTE explícito deveria ter precedência sobre ENVIRONMENT")
	}
}

func TestJWTSecretCurtoEhFatal(t *testing.T) {
	// Este sim é impedimento real: chave curta é sessão forjável.
	t.Setenv("DATABASE_URL", "postgres://u:p@host:5432/db")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 31))

	_, err := Carregar()
	if err == nil {
		t.Fatal("JWT_SECRET de 31 caracteres deveria ser recusada")
	}
	if !strings.Contains(err.Error(), "openssl rand") {
		t.Errorf("a mensagem deveria ensinar a gerar a chave: %v", err)
	}
}
