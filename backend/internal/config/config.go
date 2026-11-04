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
	// dbSenhaVazia registra que a DSN foi montada das partes e DB_PASSWORD
	// veio vazia. Em produção isso é variável esquecida, não autenticação
	// "trust", e precisa ser apontado antes de a conexão falhar.
	dbSenhaVazia bool

	// Autenticação
	JWTSecret     string
	SessaoDuracao time.Duration
	AdminEmail    string
	AdminSenha    string

	// Leitura via Claude
	AnthropicAPIKey string
	// AnthropicBaseURL permite apontar para um gateway compatível com a API
	// Messages (OmniRoute, OpenRouter em modo Anthropic, proxy corporativo).
	// Vazio usa api.anthropic.com.
	AnthropicBaseURL string
	// RecursosAnthropic liga os recursos exclusivos da API da Anthropic:
	// raciocínio adaptativo, saída estruturada por schema e cache de prompt.
	// Um backend apenas COMPATÍVEL costuma recusá-los. Por isso o padrão
	// acompanha o endereço: ligado em api.anthropic.com, desligado quando
	// ANTHROPIC_BASE_URL aponta para outro lugar.
	RecursosAnthropic bool
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
		Ambiente:          ambiente(),
		UploadMaxMB:       int64(numero("UPLOAD_MAX_MB", 50)),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		SessaoDuracao:     time.Duration(numero("SESSAO_HORAS", 12)) * time.Hour,
		AdminEmail:        texto("ADMIN_EMAIL", ""),
		AdminSenha:        os.Getenv("ADMIN_SENHA"),
		AnthropicAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicBaseURL:  texto("ANTHROPIC_BASE_URL", ""),
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
		c.dbSenhaVazia = os.Getenv("DB_PASSWORD") == ""
		c.DatabaseURL = montarDSN(
			texto("DB_HOST", "postgres"),
			texto("DB_PORT", "5432"),
			texto("DB_USER", "cupons"),
			os.Getenv("DB_PASSWORD"),
			texto("DB_NAME", "cupons"),
			texto("DB_SSLMODE", "disable"),
		)
	}

	c.RecursosAnthropic = booleano("LEITURA_RECURSOS_ANTHROPIC", c.AnthropicBaseURL == "")

	origens := texto("CORS_ORIGENS", "")
	for _, o := range strings.Split(origens, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.OrigensCORS = append(c.OrigensCORS, o)
		}
	}

	if err := c.validar(); err != nil {
		return Config{}, err
	}

	return c, nil
}

// Producao informa se a API está rodando em produção.
func (c Config) Producao() bool { return c.Ambiente == "producao" }

// LeituraHabilitada informa se há credencial para chamar a API do Claude.
// Sem ela o sistema sobe normalmente: cadastros, revisão manual, cobrança e
// Excel continuam funcionando, só a leitura automática fica indisponível.
func (c Config) LeituraHabilitada() bool { return c.AnthropicAPIKey != "" }

// validar confere a configuração inteira e devolve TODOS os problemas de uma
// vez, numerados.
//
// Antes, cada problema derrubava a subida isoladamente: o operador corrigia um,
// redeployava, descobria o seguinte. Em deploy de produção isso custa uma
// rodada de build por erro. Aqui só entram os impedimentos reais de servir —
// o que é conveniência (criar o admin inicial, ler cupons) vira aviso no log e
// não impede a API de subir.
func (c Config) validar() error {
	var problemas []string

	if c.DatabaseURL == "" {
		problemas = append(problemas,
			"não há como conectar ao banco: defina DATABASE_URL, ou DB_HOST/DB_USER/DB_PASSWORD/DB_NAME")
	} else if err := conferirDSN(c.DatabaseURL); err != nil {
		problemas = append(problemas, err.Error())
	}

	if c.dbSenhaVazia && c.Producao() {
		problemas = append(problemas,
			"DB_PASSWORD está vazia. Em produção o banco exige senha — confira a variável no painel")
	}

	switch {
	case c.JWTSecret == "":
		problemas = append(problemas,
			"JWT_SECRET não definida. Gere com: openssl rand -hex 64")
	case len(c.JWTSecret) < MinimoJWTSecret:
		problemas = append(problemas, fmt.Sprintf(
			"JWT_SECRET tem %d caracteres e precisa de no mínimo %d. Gere com: openssl rand -hex 64",
			len(c.JWTSecret), MinimoJWTSecret))
	}

	if c.Producao() && len(c.OrigensCORS) == 0 {
		problemas = append(problemas,
			"em produção defina CORS_ORIGENS (ex.: https://cupom.fbtax.cloud)")
	}

	if len(problemas) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "a configuração tem %d problema(s) que impedem a API de subir:", len(problemas))
	for i, p := range problemas {
		fmt.Fprintf(&b, "\n  %d. %s", i+1, p)
	}
	return fmt.Errorf("%s", b.String())
}

// MinimoJWTSecret é o tamanho mínimo aceito para a chave de assinatura das
// sessões. Abaixo disso a chave é adivinhável por força bruta.
const MinimoJWTSecret = 32

// MinimoSenha é o tamanho mínimo de senha de usuário.
const MinimoSenha = 8

// SenhaAdminUtilizavel informa se ADMIN_SENHA serve para criar o usuário
// inicial. Quando não serve, a subida NÃO falha: uma senha é sorteada e
// anunciada no log (ver cmd/api). Derrubar uma API cujo banco está de pé por
// causa da senha de bootstrap seria trocar um problema pequeno por um grande.
func (c Config) SenhaAdminUtilizavel() bool {
	return len(c.AdminSenha) >= MinimoSenha
}

// ambiente aceita AMBIENTE (deste projeto) e ENVIRONMENT (nome usado nos
// outros projetos FB), para a configuração poder ser copiada entre eles.
func ambiente() string {
	if v := texto("AMBIENTE", ""); v != "" {
		return v
	}
	switch strings.ToLower(texto("ENVIRONMENT", "")) {
	case "production", "producao", "produção":
		return "producao"
	case "":
		return "desenvolvimento"
	default:
		return "desenvolvimento"
	}
}

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
			"defina DB_HOST, DB_PORT, DB_USER, DB_PASSWORD e DB_NAME, que a API escapa a senha sozinha", err)
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

// booleano lê uma variável de ambiente como sim/não, aceitando as grafias
// usuais. Vazia devolve o padrão.
func booleano(chave string, padrao bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(chave))) {
	case "":
		return padrao
	case "1", "true", "sim", "t", "yes", "on":
		return true
	default:
		return false
	}
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
