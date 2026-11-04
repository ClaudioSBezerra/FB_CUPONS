package leitura

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
)

// ClienteOpenAI lê cupons pela API de Chat Completions da OpenAI.
//
// Existe porque a OpenAI não fala a API Messages: o schema das mensagens, o
// bloco de imagem e o formato de saída estruturada são todos diferentes.
// Trocar ANTHROPIC_BASE_URL não resolveria.
//
// É HTTP direto, sem SDK, de propósito: usamos um único endpoint e um schema
// que já existe (SchemaJSON), então um SDK inteiro acrescentaria superfície de
// dependência sem reduzir o que há para manter. O formato de saída
// estruturada é o mesmo que a OpenAI documenta, com strict: true.
type ClienteOpenAI struct {
	http      *http.Client
	chave     string
	baseURL   string
	modelo    string
	maxTokens int64
	// estruturado liga response_format com json_schema. Desligue se o backend
	// (um gateway, por exemplo) não suportar.
	estruturado bool
}

// OpcoesOpenAI configura o leitor.
type OpcoesOpenAI struct {
	APIKey string
	// BaseURL sem o caminho. Vazio usa https://api.openai.com/v1.
	BaseURL string
	// Modelo precisa ter VISÃO. Um modelo só de texto não falha de forma
	// óbvia: responde como se não houvesse foto, e a cobrança sai inventada.
	Modelo string
	// MaxTokens da resposta. 0 usa 16000.
	MaxTokens int64
	Timeout   time.Duration
	// Estruturado liga a saída por schema. Padrão: ligado.
	Estruturado bool
}

// ModeloOpenAIPadrao é um modelo com visão e saída estruturada. Confira a
// página de modelos da sua conta: pode haver opção mais nova e melhor.
const ModeloOpenAIPadrao = "gpt-4o"

// BaseURLOpenAIPadrao é o endereço oficial, já com o /v1.
const BaseURLOpenAIPadrao = "https://api.openai.com/v1"

// NovoClienteOpenAI monta o leitor.
func NovoClienteOpenAI(opc OpcoesOpenAI) *ClienteOpenAI {
	timeout := opc.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	base := strings.TrimSuffix(strings.TrimSpace(opc.BaseURL), "/")
	if base == "" {
		base = BaseURLOpenAIPadrao
	}
	modelo := opc.Modelo
	if modelo == "" {
		modelo = ModeloOpenAIPadrao
	}
	maxTokens := opc.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16000
	}

	return &ClienteOpenAI{
		http:        &http.Client{Timeout: timeout},
		chave:       opc.APIKey,
		baseURL:     base,
		modelo:      modelo,
		maxTokens:   maxTokens,
		estruturado: opc.Estruturado,
	}
}

// Modelo informa qual modelo este leitor usa.
func (c *ClienteOpenAI) Modelo() string { return c.modelo }

// BaseURL informa para onde as requisições vão, para aparecer no log.
func (c *ClienteOpenAI) BaseURL() string { return c.baseURL }

// ComModelo devolve uma cópia usando outro modelo — é o que o endpoint de
// reprocessamento usa para insistir num cupom difícil.
func (c *ClienteOpenAI) ComModelo(modelo string) *ClienteOpenAI {
	copia := *c
	if modelo != "" {
		copia.modelo = modelo
	}
	return &copia
}

// --- formato da requisição ---

type pedidoOpenAI struct {
	Model          string           `json:"model"`
	MaxTokens      int64            `json:"max_completion_tokens,omitempty"`
	Messages       []mensagemOpenAI `json:"messages"`
	ResponseFormat *formatoOpenAI   `json:"response_format,omitempty"`
}

type mensagemOpenAI struct {
	Role string `json:"role"`
	// Content é string na mensagem de sistema e lista de partes na do usuário.
	Content any `json:"content"`
}

type parteTexto struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type parteImagem struct {
	Type     string         `json:"type"`
	ImageURL imagemURLParte `json:"image_url"`
}

type imagemURLParte struct {
	URL string `json:"url"`
	// Detail "high" preserva resolução; cupom térmico apagado depende disso.
	Detail string `json:"detail,omitempty"`
}

type formatoOpenAI struct {
	Type       string        `json:"type"`
	JSONSchema schemaNomeado `json:"json_schema"`
}

type schemaNomeado struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type respostaOpenAI struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// Ler envia a imagem e devolve o cupom lido.
func (c *ClienteOpenAI) Ler(ctx context.Context, img imagem.Imagem, fonte string, ref Referencias) (Resultado, error) {
	if len(img.Dados) == 0 {
		return Resultado{}, fmt.Errorf("imagem vazia")
	}

	// O modelo entra no resultado antes da chamada: numa falha, saber qual
	// modelo foi tentado é metade do diagnóstico.
	res := Resultado{Modelo: c.modelo}
	inicio := time.Now()

	dataURL := "data:" + img.Mime + ";base64," + base64.StdEncoding.EncodeToString(img.Dados)

	pedido := pedidoOpenAI{
		Model:     c.modelo,
		MaxTokens: c.maxTokens,
		Messages: []mensagemOpenAI{
			{Role: "system", Content: MontarSistema(ref)},
			{Role: "user", Content: []any{
				parteImagem{Type: "image_url", ImageURL: imagemURLParte{URL: dataURL, Detail: "high"}},
				parteTexto{Type: "text", Text: instrucaoPagina(fonte)},
			}},
		},
	}
	if c.estruturado {
		pedido.ResponseFormat = &formatoOpenAI{
			Type: "json_schema",
			JSONSchema: schemaNomeado{
				Name:   "cupom_fiscal",
				Strict: true,
				Schema: SchemaJSON(),
			},
		}
	}

	corpo, err := json.Marshal(pedido)
	if err != nil {
		return res, fmt.Errorf("não foi possível montar a requisição: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(corpo))
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.chave)

	resp, err := c.http.Do(req)
	if err != nil {
		res.Duracao = time.Since(inicio)
		return res, fmt.Errorf("falha ao falar com a API da OpenAI em %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	bruto, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		res.Duracao = time.Since(inicio)
		return res, fmt.Errorf("não foi possível ler a resposta: %w", err)
	}
	res.Duracao = time.Since(inicio)

	var r respostaOpenAI
	if err := json.Unmarshal(bruto, &r); err != nil {
		return res, fmt.Errorf("a resposta da API (HTTP %d) não é JSON: %s",
			resp.StatusCode, primeirosBytes(bruto, 300))
	}

	res.TokensEntrada = r.Usage.PromptTokens
	res.TokensSaida = r.Usage.CompletionTokens

	if resp.StatusCode != http.StatusOK {
		return res, c.traduzErro(resp.StatusCode, r, bruto)
	}
	if len(r.Choices) == 0 {
		return res, fmt.Errorf("a API não devolveu nenhuma resposta")
	}

	escolha := r.Choices[0]
	if escolha.Message.Refusal != "" {
		return res, fmt.Errorf("a leitura foi recusada pelo modelo: %s", escolha.Message.Refusal)
	}
	if escolha.FinishReason == "length" {
		return res, fmt.Errorf("a resposta foi truncada no limite de %d tokens; "+
			"o cupom tem itens demais para a configuração atual", c.maxTokens)
	}
	if strings.TrimSpace(escolha.Message.Content) == "" {
		return res, fmt.Errorf("o modelo não devolveu nenhum texto (finish_reason=%q)", escolha.FinishReason)
	}

	res.Bruto = json.RawMessage(escolha.Message.Content)
	cupom, err := Interpretar([]byte(escolha.Message.Content), fonte)
	if err != nil {
		return res, err
	}
	res.Cupom = cupom
	return res, nil
}

// traduzErro transforma a falha da API em mensagem útil ao operador.
func (c *ClienteOpenAI) traduzErro(status int, r respostaOpenAI, bruto []byte) error {
	detalhe := primeirosBytes(bruto, 400)
	if r.Error != nil && r.Error.Message != "" {
		detalhe = r.Error.Message
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("a credencial foi recusada por %s — confira OPENAI_API_KEY: %s",
			c.baseURL, detalhe)
	case http.StatusNotFound:
		return fmt.Errorf("modelo %q não encontrado em %s — confira OPENAI_MODEL na página de "+
			"modelos da sua conta: %s", c.modelo, c.baseURL, detalhe)
	case http.StatusRequestEntityTooLarge:
		return fmt.Errorf("a imagem do cupom é grande demais para a API: %s", detalhe)
	case http.StatusTooManyRequests:
		return fmt.Errorf("limite de requisições da OpenAI atingido; tente novamente em "+
			"alguns instantes ou reduza LEITURA_CONCORRENCIA: %s", detalhe)
	case http.StatusBadRequest:
		// O caso mais comum aqui é modelo sem visão ou schema recusado.
		return fmt.Errorf("a API recusou a requisição (HTTP 400). Se o modelo %q não "+
			"aceita imagem, troque por um com visão; se o problema for a saída "+
			"estruturada, desligue com LEITURA_ESTRUTURADA=0. Detalhe: %s", c.modelo, detalhe)
	default:
		if status >= 500 {
			return fmt.Errorf("a API da OpenAI está indisponível (HTTP %d): %s", status, detalhe)
		}
		return fmt.Errorf("a API recusou a leitura (HTTP %d): %s", status, detalhe)
	}
}

func primeirosBytes(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
