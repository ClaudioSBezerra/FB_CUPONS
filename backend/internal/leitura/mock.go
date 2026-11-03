package leitura

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
)

// LeitorGravado é um Leitor que devolve respostas pré-gravadas, na ordem em
// que foram registradas. Serve para a CI e para os testes de integração
// rodarem sem gastar API — CLAUDE.md §11.
type LeitorGravado struct {
	mu        sync.Mutex
	respostas []json.RawMessage
	indice    int
	// Chamadas guarda as fontes lidas, para o teste conferir a ordem.
	Chamadas []string
}

// NovoLeitorGravado monta o leitor com as respostas JSON na ordem de uso.
func NovoLeitorGravado(respostas ...json.RawMessage) *LeitorGravado {
	return &LeitorGravado{respostas: respostas}
}

// Ler devolve a próxima resposta gravada.
func (l *LeitorGravado) Ler(_ context.Context, _ imagem.Imagem, fonte string, _ Referencias) (Resultado, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.Chamadas = append(l.Chamadas, fonte)
	if l.indice >= len(l.respostas) {
		return Resultado{}, fmt.Errorf("leitor gravado esgotado: %d respostas para %d chamadas",
			len(l.respostas), l.indice+1)
	}
	bruto := l.respostas[l.indice]
	l.indice++

	cupom, err := Interpretar(bruto, fonte)
	if err != nil {
		return Resultado{}, err
	}
	return Resultado{
		Cupom:         cupom,
		Bruto:         bruto,
		Modelo:        "gravado",
		TokensEntrada: 0,
		TokensSaida:   0,
		Duracao:       time.Millisecond,
	}, nil
}

// Modelo identifica o leitor nos logs.
func (l *LeitorGravado) Modelo() string { return "gravado" }
