package auth

import (
	"strings"
	"testing"
)

func TestSenhaAleatoria(t *testing.T) {
	vistas := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := SenhaAleatoria()
		if err != nil {
			t.Fatal(err)
		}
		if len(s) < 16 {
			t.Fatalf("senha curta demais: %d caracteres", len(s))
		}
		if vistas[s] {
			t.Fatalf("senha repetida em %d sorteios: %q", i, s)
		}
		vistas[s] = true

		// Sem caracteres ambíguos: a senha é ditada ou copiada do log.
		for _, proibido := range []string{"0", "O", "1", "l", "I", "o"} {
			if strings.Contains(s, proibido) {
				t.Errorf("senha %q contém caractere ambíguo %q", s, proibido)
			}
		}
		// E serve para o hash.
		if _, err := HashSenha(s); err != nil {
			t.Errorf("senha sorteada não passa no HashSenha: %v", err)
		}
	}
}

func TestHashSenhaRecusaCurta(t *testing.T) {
	if _, err := HashSenha("1234567"); err == nil {
		t.Error("7 caracteres deveria ser recusado")
	}
	if _, err := HashSenha("12345678"); err != nil {
		t.Errorf("8 caracteres deveria passar: %v", err)
	}
}

func TestConferirSenha(t *testing.T) {
	hash, err := HashSenha("senhaCorreta123")
	if err != nil {
		t.Fatal(err)
	}
	if err := ConferirSenha(hash, "senhaCorreta123"); err != nil {
		t.Errorf("senha correta foi recusada: %v", err)
	}
	if err := ConferirSenha(hash, "senhaErrada123"); err == nil {
		t.Error("senha errada foi aceita")
	}
	// A mensagem não deve revelar se o usuário existe.
	if err := ConferirSenha(hash, "x"); err != ErroCredenciais {
		t.Errorf("erro = %v, quer ErroCredenciais genérico", err)
	}
}
