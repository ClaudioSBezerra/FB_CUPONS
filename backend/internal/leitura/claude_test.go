package leitura

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
)

// servidorFalso captura o corpo da requisição, para o teste conferir o que o
// cliente realmente envia.
func servidorFalso(t *testing.T, capturado *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(corpo, &m); err != nil {
			t.Errorf("corpo não é JSON: %v", err)
		}
		*capturado = m

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
		  "id":"msg_1","type":"message","role":"assistant","model":"teste",
		  "stop_reason":"end_turn",
		  "content":[{"type":"text","text":"{\"numero\":\"1\",\"data\":\"2026-10-01\",\"cnpj_cliente\":\"37678845000302\",\"cliente\":\"DONA ÍRIS\",\"total\":10.0,\"itens\":[],\"incertos\":[],\"avisos\":[]}"}],
		  "usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}
		}`))
	}))
}

func imagemDeTeste() imagem.Imagem {
	return imagem.Imagem{Mime: "image/jpeg", Dados: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}}
}

func TestRecursosAnthropicLigados(t *testing.T) {
	var corpo map[string]any
	srv := servidorFalso(t, &corpo)
	defer srv.Close()

	c := NovoClienteClaude(Opcoes{
		APIKey: "k", BaseURL: srv.URL, Modelo: "claude-opus-5",
		RecursosAnthropic: true,
	})
	if _, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{}); err != nil {
		t.Fatal(err)
	}

	if _, tem := corpo["thinking"]; !tem {
		t.Error("com os recursos ligados, thinking deveria ir na requisição")
	}
	if _, tem := corpo["output_config"]; !tem {
		t.Error("com os recursos ligados, output_config deveria ir na requisição")
	}
	sistema, _ := corpo["system"].([]any)
	if len(sistema) == 0 {
		t.Fatal("system vazio")
	}
	bloco, _ := sistema[0].(map[string]any)
	if _, tem := bloco["cache_control"]; !tem {
		t.Error("com os recursos ligados, o system deveria ir marcado para cache")
	}
}

func TestRecursosAnthropicDesligados(t *testing.T) {
	// É o caso de um backend apenas compatível, como o endpoint Anthropic do
	// DeepSeek: os recursos exclusivos iriam ser recusados.
	var corpo map[string]any
	srv := servidorFalso(t, &corpo)
	defer srv.Close()

	c := NovoClienteClaude(Opcoes{
		APIKey: "k", BaseURL: srv.URL, Modelo: "deepseek-flash",
		RecursosAnthropic: false,
	})
	res, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{})
	if err != nil {
		t.Fatal(err)
	}

	for _, campo := range []string{"thinking", "output_config"} {
		if _, tem := corpo[campo]; tem {
			t.Errorf("com os recursos desligados, %q não deveria ir na requisição", campo)
		}
	}
	sistema, _ := corpo["system"].([]any)
	if len(sistema) > 0 {
		if bloco, _ := sistema[0].(map[string]any); bloco != nil {
			if _, tem := bloco["cache_control"]; tem {
				t.Error("com os recursos desligados, o system não deveria ser marcado para cache")
			}
		}
	}

	// O essencial continua: imagem enviada e JSON interpretado.
	mensagens, _ := corpo["messages"].([]any)
	if len(mensagens) != 1 {
		t.Fatalf("mensagens = %d", len(mensagens))
	}
	primeira, _ := mensagens[0].(map[string]any)
	conteudo, _ := primeira["content"].([]any)
	achouImagem := false
	for _, b := range conteudo {
		if bloco, ok := b.(map[string]any); ok && bloco["type"] == "image" {
			achouImagem = true
		}
	}
	if !achouImagem {
		t.Error("a imagem não foi enviada")
	}
	if res.Cupom.CNPJCliente != "37678845000302" {
		t.Errorf("a resposta não foi interpretada: %+v", res.Cupom)
	}
	if res.Modelo != "deepseek-flash" {
		t.Errorf("modelo registrado = %q", res.Modelo)
	}
}

func TestModeloERegistradoMesmoNaFalha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	c := NovoClienteClaude(Opcoes{APIKey: "k", BaseURL: srv.URL, Modelo: "deepseek-flash"})
	res, err := c.Ler(context.Background(), imagemDeTeste(), "p.1", Referencias{})
	if err == nil {
		t.Fatal("deveria falhar com 401")
	}
	if res.Modelo != "deepseek-flash" {
		t.Errorf("modelo na falha = %q, quer deepseek-flash — é metade do diagnóstico", res.Modelo)
	}
	// A mensagem precisa dizer PARA ONDE a credencial foi recusada.
	if !contemTexto(err.Error(), srv.URL) {
		t.Errorf("a mensagem não diz o endereço: %v", err)
	}
	if !contemTexto(err.Error(), "ANTHROPIC_BASE_URL") {
		t.Errorf("a mensagem não sugere a variável do endereço: %v", err)
	}
}

func contemTexto(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
