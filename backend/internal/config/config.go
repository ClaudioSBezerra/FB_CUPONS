// Package config lê a configuração do ambiente. Segredos existem apenas em
// variáveis de ambiente — nunca em arquivo versionado.
package config

import (
	"fmt"
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

	origens := texto("CORS_ORIGENS", "")
	for _, o := range strings.Split(origens, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.OrigensCORS = append(c.OrigensCORS, o)
		}
	}

	var faltando []string
	if c.DatabaseURL == "" {
		faltando = append(faltando, "DATABASE_URL")
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
