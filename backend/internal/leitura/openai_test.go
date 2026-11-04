package leitura

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

const respostaJSONCupom = `{"numero":"1340923","data":"2026-10-16","cnpj_cliente":"37678845000736","cliente":"MATERNIDADE CÉLIA CÂMARA","total":120.00,"itens":[{"cod":"20771","prod":"PÃO FRANCÊS 50G","un":"UND","qtd":120,"vun":1.0,"vtot":120.0,"incertos":[]}],"incertos":[],"avisos":[]}`

func servidorOpenAI(t *testing.T, capturado *map[string]any, caminho *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*caminho = r.URL.Path
		corpo, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(corpo, &m); err != nil {
			t.Errorf("corpo não é JSON: %v", err)
		}
		*capturado = m

		if r.Header.Get("Authorization") != "Bearer chave-de-teste" {
			t.Errorf("cabeçalho de autorização = %q", r.Header.Get("Authorization"))
		}

		resposta := map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]any{"content": respostaJSONCupom},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1200, "completion_tokens": 180},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resposta)
	}))
}

func TestOpenAIEnviaImagemComoDataURL(t *testing.T) {
	var corpo map[string]any
	var caminho string
	srv := servidorOpenAI(t, &corpo, &caminho)
	defer srv.Close()

	c := NovoClienteOpenAI(OpcoesOpenAI{
		APIKey: "chave-de-teste", BaseURL: srv.URL, Modelo: "gpt-4o", Estruturado: true,
	})
	res, err := c.Ler(context.Background(), imagemDeTeste(), "cupom.pdf p.1", Referencias{})
	if err != nil {
		t.Fatal(err)
	}

	if caminho != "/chat/completions" {
		t.Errorf("caminho = %q, quer /chat/completions", caminho)
	}
	if corpo["model"] != "gpt-4o" {
		t.Errorf("model = %v", corpo["model"])
	}

	// A imagem tem de ir como data URL, com detail high: cupom térmico
	// apagado depende de resolução.
	mensagens, _ := corpo["messages"].([]any)
	if len(mensagens) != 2 {
		t.Fatalf("mensagens = %d, quer 2 (system + user)", len(mensagens))
	}
	sistema, _ := mensagens[0].(map[string]any)
	if sistema["role"] != "system" {
		t.Errorf("primeira mensagem = %v, quer system", sistema["role"])
	}
	if texto, _ := sistema["content"].(string); !strings.Contains(texto, "NUNCA invente") {
		t.Error("o prompt de sistema não foi enviado")
	}

	usuario, _ := mensagens[1].(map[string]any)
	partes, _ := usuario["content"].([]any)
	achouImagem := false
	for _, p := range partes {
		parte, _ := p.(map[string]any)
		if parte["type"] != "image_url" {
			continue
		}
		achouImagem = true
		u, _ := parte["image_url"].(map[string]any)
		url, _ := u["url"].(string)
		if !strings.HasPrefix(url, "data:image/jpeg;base64,") {
			t.Errorf("a imagem não foi como data URL: %q", primeirosBytes([]byte(url), 40))
		}
		if u["detail"] != "high" {
			t.Errorf("detail = %v, quer high", u["detail"])
		}
	}
	if !achouImagem {
		t.Error("a imagem não foi enviada")
	}

	// Saída estruturada no formato da OpenAI.
	formato, _ := corpo["response_format"].(map[string]any)
	if formato == nil || formato["type"] != "json_schema" {
		t.Fatalf("response_format = %v", corpo["response_format"])
	}
	schema, _ := formato["json_schema"].(map[string]any)
	if schema["strict"] != true {
		t.Errorf("strict = %v, quer true", schema["strict"])
	}
	if schema["name"] == nil || schema["name"] == "" {
		t.Error("o schema precisa de nome na OpenAI")
	}

	// E o resultado foi interpretado no domínio.
	if res.Cupom.Numero != "1340923" {
		t.Errorf("numero = %q", res.Cupom.Numero)
	}
	if res.Cupom.Total != dominio.Valor(12000) {
		t.Errorf("total = %s", res.Cupom.Total)
	}
	if res.TokensEntrada != 1200 || res.TokensSaida != 180 {
		t.Errorf("tokens = %d/%d, quer 1200/180", res.TokensEntrada, res.TokensSaida)
	}
	if res.Modelo != "gpt-4o" {
		t.Errorf("modelo = %q", res.Modelo)
	}
}

func TestOpenAISemSaidaEstruturada(t *testing.T) {
	// Para gateway que não suporta json_schema.
	var corpo map[string]any
	var caminho string
	srv := servidorOpenAI(t, &corpo, &caminho)
	defer srv.Close()

	c := NovoClienteOpenAI(OpcoesOpenAI{
		APIKey: "chave-de-teste", BaseURL: srv.URL, Modelo: "gpt-4o", Estruturado: false,
	})
	if _, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{}); err != nil {
		t.Fatal(err)
	}
	if _, tem := corpo["response_format"]; tem {
		t.Error("com a saída estruturada desligada, response_format não deveria ir")
	}
}

func TestOpenAITraduzErros(t *testing.T) {
	casos := []struct {
		nome      string
		status    int
		corpo     string
		querTexto []string
	}{
		{
			nome:      "credencial recusada aponta a variável",
			status:    http.StatusUnauthorized,
			corpo:     `{"error":{"message":"Incorrect API key provided"}}`,
			querTexto: []string{"OPENAI_API_KEY", "Incorrect API key"},
		},
		{
			nome:      "modelo inexistente aponta a página de modelos",
			status:    http.StatusNotFound,
			corpo:     `{"error":{"message":"The model does not exist"}}`,
			querTexto: []string{"OPENAI_MODEL", "modelos da sua conta"},
		},
		{
			nome:      "400 sugere modelo sem visão e o desligamento do schema",
			status:    http.StatusBadRequest,
			corpo:     `{"error":{"message":"Invalid content type image_url"}}`,
			querTexto: []string{"não aceita imagem", "LEITURA_ESTRUTURADA=0"},
		},
		{
			nome:      "429 sugere reduzir a concorrência",
			status:    http.StatusTooManyRequests,
			corpo:     `{"error":{"message":"Rate limit reached"}}`,
			querTexto: []string{"LEITURA_CONCORRENCIA"},
		},
		{
			nome:      "500 diz que é indisponibilidade",
			status:    http.StatusInternalServerError,
			corpo:     `{"error":{"message":"server error"}}`,
			querTexto: []string{"indisponível"},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(caso.status)
				_, _ = w.Write([]byte(caso.corpo))
			}))
			defer srv.Close()

			c := NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k", BaseURL: srv.URL, Modelo: "gpt-4o"})
			res, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{})
			if err == nil {
				t.Fatal("deveria falhar")
			}
			if res.Modelo != "gpt-4o" {
				t.Errorf("modelo na falha = %q — é metade do diagnóstico", res.Modelo)
			}
			for _, esperado := range caso.querTexto {
				if !strings.Contains(err.Error(), esperado) {
					t.Errorf("a mensagem não contém %q:\n%v", esperado, err)
				}
			}
		})
	}
}

func TestOpenAIRecusaERespostaTruncada(t *testing.T) {
	t.Run("recusa do modelo", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"refusal":"não posso"},"finish_reason":"stop"}],"usage":{}}`))
		}))
		defer srv.Close()
		c := NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k", BaseURL: srv.URL})
		if _, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{}); err == nil ||
			!strings.Contains(err.Error(), "recusada") {
			t.Errorf("erro = %v, quer mencionar recusa", err)
		}
	})

	t.Run("resposta truncada não vira cupom pela metade", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"numero\":\"13"},"finish_reason":"length"}],"usage":{}}`))
		}))
		defer srv.Close()
		c := NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k", BaseURL: srv.URL})
		_, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{})
		if err == nil || !strings.Contains(err.Error(), "truncada") {
			t.Errorf("erro = %v, quer avisar truncamento", err)
		}
	})
}

func TestOpenAIBaseURLPadraoENormalizacao(t *testing.T) {
	c := NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k"})
	if c.BaseURL() != BaseURLOpenAIPadrao {
		t.Errorf("padrão = %q", c.BaseURL())
	}
	// Barra no fim não pode gerar // no caminho.
	c = NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k", BaseURL: "https://gw.exemplo.com/v1/"})
	if c.BaseURL() != "https://gw.exemplo.com/v1" {
		t.Errorf("barra final não foi removida: %q", c.BaseURL())
	}
}

func TestOpenAIComModelo(t *testing.T) {
	c := NovoClienteOpenAI(OpcoesOpenAI{APIKey: "k", Modelo: "gpt-4o"})
	outro := c.ComModelo("o3")
	if outro.Modelo() != "o3" {
		t.Errorf("ComModelo = %q", outro.Modelo())
	}
	if c.Modelo() != "gpt-4o" {
		t.Error("ComModelo alterou o cliente original")
	}
}
