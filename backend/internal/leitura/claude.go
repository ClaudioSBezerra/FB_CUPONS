package leitura

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
)

// Leitor lê um cupom a partir de uma imagem. A interface existe para que os
// testes e a CI rodem com um leitor gravado, sem gastar API.
type Leitor interface {
	Ler(ctx context.Context, img imagem.Imagem, fonte string, ref Referencias) (Resultado, error)
}

// Resultado é a leitura de uma página, antes das validações de negócio.
type Resultado struct {
	Cupom dominio.Cupom
	// Bruto é a resposta JSON do modelo, gravada em leituras_log para auditoria.
	Bruto json.RawMessage
	// Modelo, TokensEntrada e TokensSaida alimentam o controle de custo.
	Modelo        string
	TokensEntrada int64
	TokensSaida   int64
	Duracao       time.Duration
}

// ClienteClaude é o Leitor de produção, falando com a API Messages.
type ClienteClaude struct {
	api       anthropic.Client
	modelo    string
	baseURL   string
	maxTokens int64
	esforco   anthropic.OutputConfigEffort
}

// Opcoes configura o cliente de leitura.
type Opcoes struct {
	// APIKey vazia deixa o SDK resolver a credencial (ANTHROPIC_API_KEY ou
	// perfil OAuth em ~/.config/anthropic).
	APIKey string
	// BaseURL aponta para um gateway compatível com a API Messages. Vazio
	// usa api.anthropic.com.
	BaseURL string
	// Modelo padrão das leituras. Vazio usa ModeloPadrao.
	Modelo string
	// Esforco controla profundidade de raciocínio e custo: low, medium, high,
	// xhigh, max. Vazio usa "high".
	Esforco string
	// MaxTokens da resposta. 0 usa 16000, que acomoda um cupom de 30 itens.
	MaxTokens int64
	// Timeout de cada chamada. 0 usa 3 minutos.
	Timeout time.Duration
}

const (
	// ModeloPadrao é o modelo usado na leitura normal.
	ModeloPadrao = "claude-opus-5"
	// ModeloReforcado é o modelo do "reprocessar com modelo mais forte",
	// para cupons que a leitura normal não conseguiu fechar.
	ModeloReforcado = "claude-fable-5"
)

// NovoClienteClaude monta o leitor de produção.
func NovoClienteClaude(opc Opcoes) *ClienteClaude {
	var opts []option.RequestOption
	if opc.APIKey != "" {
		opts = append(opts, option.WithAPIKey(opc.APIKey))
	}
	if opc.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(opc.BaseURL))
	}
	timeout := opc.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	opts = append(opts, option.WithRequestTimeout(timeout))

	modelo := opc.Modelo
	if modelo == "" {
		modelo = ModeloPadrao
	}
	maxTokens := opc.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16000
	}
	esforco := anthropic.OutputConfigEffort(opc.Esforco)
	if opc.Esforco == "" {
		esforco = anthropic.OutputConfigEffortHigh
	}

	base := opc.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}

	return &ClienteClaude{
		api:       anthropic.NewClient(opts...),
		modelo:    modelo,
		baseURL:   base,
		maxTokens: maxTokens,
		esforco:   esforco,
	}
}

// ComModelo devolve uma cópia do leitor usando outro modelo — é o que o
// endpoint de reprocessamento usa para insistir num cupom difícil.
func (c *ClienteClaude) ComModelo(modelo string) *ClienteClaude {
	copia := *c
	if modelo != "" {
		copia.modelo = modelo
	}
	return &copia
}

// Modelo informa qual modelo este leitor usa.
func (c *ClienteClaude) Modelo() string { return c.modelo }

// BaseURL informa para onde as requisições vão, para aparecer no log.
func (c *ClienteClaude) BaseURL() string { return c.baseURL }

// Ler envia a imagem ao Claude e devolve o cupom lido.
func (c *ClienteClaude) Ler(ctx context.Context, img imagem.Imagem, fonte string, ref Referencias) (Resultado, error) {
	if len(img.Dados) == 0 {
		return Resultado{}, errors.New("imagem vazia")
	}

	b64 := base64.StdEncoding.EncodeToString(img.Dados)
	inicio := time.Now()

	// O prompt de sistema (catálogo + clientes) é idêntico em todas as páginas
	// do lote: marcá-lo para cache reduz o custo de um PDF de 20 páginas.
	adaptativo := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.modelo),
		MaxTokens: c.maxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         MontarSistema(ref),
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Thinking: anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptativo},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: c.esforco,
			Format: anthropic.JSONOutputFormatParam{Schema: SchemaJSON()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64(img.Mime, b64),
				anthropic.NewTextBlock(instrucaoPagina(fonte)),
			),
		},
	})
	if err != nil {
		return Resultado{}, c.traduzErro(err)
	}

	res := Resultado{
		Modelo:        c.modelo,
		TokensEntrada: resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens,
		TokensSaida:   resp.Usage.OutputTokens,
		Duracao:       time.Since(inicio),
	}

	// Classificador de segurança pode recusar a requisição com HTTP 200.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return res, fmt.Errorf("a leitura foi recusada pelo modelo (%s): %s",
			resp.StopDetails.Category, resp.StopDetails.Explanation)
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return res, fmt.Errorf("a resposta da leitura foi truncada no limite de %d tokens; "+
			"o cupom tem itens demais para a configuração atual", c.maxTokens)
	}

	var texto strings.Builder
	for _, bloco := range resp.Content {
		if t, ok := bloco.AsAny().(anthropic.TextBlock); ok {
			texto.WriteString(t.Text)
		}
	}
	bruto := strings.TrimSpace(texto.String())
	if bruto == "" {
		return res, errors.New("o modelo não devolveu nenhum texto")
	}
	res.Bruto = json.RawMessage(bruto)

	cupom, err := Interpretar([]byte(bruto), fonte)
	if err != nil {
		return res, err
	}
	res.Cupom = cupom
	return res, nil
}

// traduzErro transforma o erro da API em mensagem útil ao operador, mantendo
// o original encadeado para os logs.
func (c *ClienteClaude) traduzErro(err error) error {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) {
		return fmt.Errorf("falha ao falar com a API do Claude: %w", err)
	}
	switch apierr.StatusCode {
	case 401, 403:
		return fmt.Errorf("a credencial foi recusada por %s — confira ANTHROPIC_API_KEY. "+
			"Se a chave é de um gateway (OmniRoute, OpenRouter, proxy), defina também "+
			"ANTHROPIC_BASE_URL com o endereço dele: %w", c.baseURL, err)
	case 404:
		return fmt.Errorf("modelo de leitura não encontrado — confira CLAUDE_MODEL: %w", err)
	case 413:
		return fmt.Errorf("a imagem do cupom é grande demais para a API: %w", err)
	case 429:
		return fmt.Errorf("limite de requisições da API do Claude atingido; tente novamente em alguns instantes: %w", err)
	default:
		if apierr.StatusCode >= 500 {
			return fmt.Errorf("a API do Claude está indisponível (HTTP %d): %w", apierr.StatusCode, err)
		}
		return fmt.Errorf("a API do Claude recusou a leitura (HTTP %d): %w", apierr.StatusCode, err)
	}
}
