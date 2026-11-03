// Package config lê a configuração do ambiente. Segredos existem apenas em
// variáveis de ambiente — nunca em arquivo versionado.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração completa da API.
type Config struct {
	// Servidor
	Porta       string
	Ambiente    string // "producao" ou "desenvolvimento"
	OrigensCORS []string
	UploadMaxMB int64

	// Banco
	DatabaseURL string

	// Autenticação
	JWTSecret     string
	SessaoDuracao time.Duration
	AdminEmail    string
	AdminSenha    string

	// Leitura via Claude
	AnthropicAPIKey   string
	ClaudeModelo      string
	ClaudeModeloForte string
	ClaudeEsforco     string
	ClaudeTimeout     time.Duration
	Concorrencia      int
	LadoMaxImagem     int
	LimiteLeiturasDia int

	// PDF
	Pdftoppm string
	DPI      int
}

// Carregar monta a configuração e valida o que é obrigatório.
func Carregar() (Config, error) {
	c := Config{
		Porta:             texto("PORTA", "8080"),
		Ambiente:          texto("AMBIENTE", "desenvolvimento"),
		UploadMaxMB:       int64(numero("UPLOAD_MAX_MB", 50)),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		SessaoDuracao:     time.Duration(numero("SESSAO_HORAS", 12)) * time.Hour,
		AdminEmail:        texto("ADMIN_EMAIL", ""),
		AdminSenha:        os.Getenv("ADMIN_SENHA"),
		AnthropicAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		ClaudeModelo:      texto("CLAUDE_MODEL", "claude-opus-5"),
		ClaudeModeloForte: texto("CLAUDE_MODEL_FORTE", "claude-fable-5"),
		ClaudeEsforco:     texto("CLAUDE_ESFORCO", "high"),
		ClaudeTimeout:     time.Duration(numero("CLAUDE_TIMEOUT_S", 180)) * time.Second,
		Concorrencia:      numero("LEITURA_CONCORRENCIA", 3),
		LadoMaxImagem:     numero("LEITURA_LADO_MAX", 1568),
		LimiteLeiturasDia: numero("LIMITE_LEITURAS_DIA", 0),
		Pdftoppm:          texto("PDFTOPPM", "pdftoppm"),
		DPI:               numero("PDF_DPI", 300),
	}

	// DATABASE_URL completa tem precedência; sem ela, monta-se a partir das
	// partes, com a senha percent-encoded por net/url.
	if c.DatabaseURL == "" {
		c.DatabaseURL = montarDSN(
			texto("DB_HOST", "postgres"),
			texto("DB_PORTA", "5432"),
			texto("DB_USUARIO", "cupons"),
			os.Getenv("DB_SENHA"),
			texto("DB_NOME", "cupons"),
			texto("DB_SSLMODE", "disable"),
		)
	}

	origens := texto("CORS_ORIGENS", "")
	for _, o := range strings.Split(origens, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.OrigensCORS = append(c.OrigensCORS, o)
		}
	}

	var faltando []string
	if c.DatabaseURL == "" {
		faltando = append(faltando, "DATABASE_URL (ou DB_HOST/DB_USUARIO/DB_SENHA/DB_NOME)")
	}
	if c.JWTSecret == "" {
		faltando = append(faltando, "JWT_SECRET")
	}
	if len(faltando) > 0 {
		return Config{}, fmt.Errorf("variáveis de ambiente obrigatórias não definidas: %s",
			strings.Join(faltando, ", "))
	}
	if len(c.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET precisa ter pelo menos 32 caracteres (tem %d)", len(c.JWTSecret))
	}
	// Confere a DSN aqui, onde é possível explicar o que fazer. Sem isto, uma
	// senha com espaço ou "@" só falha lá dentro do driver, com mensagem que
	// não diz onde arrumar.
	if err := conferirDSN(c.DatabaseURL); err != nil {
		return Config{}, err
	}

	if c.Producao() && len(c.OrigensCORS) == 0 {
		return Config{}, fmt.Errorf("em produção, defina CORS_ORIGENS (ex.: https://cupom.fbtax.cloud)")
	}

	return c, nil
}

// Producao informa se a API está rodando em produção.
func (c Config) Producao() bool { return c.Ambiente == "producao" }

// LeituraHabilitada informa se há credencial para chamar a API do Claude.
// Sem ela o sistema sobe normalmente: cadastros, revisão manual, cobrança e
// Excel continuam funcionando, só a leitura automática fica indisponível.
func (c Config) LeituraHabilitada() bool { return c.AnthropicAPIKey != "" }

// montarDSN monta a URL de conexão a partir das partes.
//
// A senha passa por url.UserPassword, que faz o percent-encoding. Montar a
// URL colando a senha com interpolação de string — como o compose fazia —
// quebra com senha contendo espaço, "@", "/", ":", "?" ou "#", e o erro só
// aparece no driver, longe de onde se configura.
func montarDSN(host, porta, usuario, senha, banco, sslmode string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(usuario, senha),
		Host:     net.JoinHostPort(host, porta),
		Path:     "/" + banco,
		RawQuery: url.Values{"sslmode": {sslmode}}.Encode(),
	}
	return u.String()
}

// conferirDSN valida a URL de conexão e explica o conserto quando a senha não
// está escapada.
func conferirDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("DATABASE_URL não é uma URL válida: %w\n"+
			"Se a senha tem espaço ou um dos caracteres @ / : ? #, não monte a URL à mão: "+
			"defina DB_HOST, DB_PORTA, DB_USUARIO, DB_SENHA e DB_NOME, que a API escapa a senha sozinha", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("DATABASE_URL deve começar com postgres:// (veio %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("DATABASE_URL sem host")
	}
	return nil
}

func texto(chave, padrao string) string {
	if v := strings.TrimSpace(os.Getenv(chave)); v != "" {
		return v
	}
	return padrao
}

func numero(chave string, padrao int) int {
	v := strings.TrimSpace(os.Getenv(chave))
	if v == "" {
		return padrao
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return padrao
	}
	return n
}
