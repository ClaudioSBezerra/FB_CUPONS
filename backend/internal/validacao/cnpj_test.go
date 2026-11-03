package validacao

import (
	"strings"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
)

const (
	cnpjMaternidade = "37678845000736"
	cnpjDonaIris    = "37678845000302"
	cnpjArroz       = "02709992000156"
)

func conhecidos() []string {
	return []string{cnpjMaternidade, cnpjDonaIris, cnpjArroz}
}

func TestCNPJValido(t *testing.T) {
	casos := []struct {
		nome string
		cnpj string
		quer bool
	}{
		{"emitente ITD/STRAVAGANZA", dominio.CNPJEmitente, true},
		{"emitente formatado", "29.220.487/0001-08", true},
		{"maternidade célia câmara", cnpjMaternidade, true},
		{"dona íris", cnpjDonaIris, true},
		{"arroz cristal", cnpjArroz, true},
		{"dv1 errado", "37678845000726", false},
		{"dv2 errado", "37678845000737", false},
		{"13 dígitos", "3767884500073", false},
		{"15 dígitos", "376788450007361", false},
		{"vazio", "", false},
		{"com letras", "3767884500073A", false},
		{"todos zeros", "00000000000000", false},
		{"todos iguais passa módulo 11 mas não é CNPJ", "11111111111111", false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := CNPJValido(c.cnpj); got != c.quer {
				t.Errorf("CNPJValido(%q) = %v, quer %v", c.cnpj, got, c.quer)
			}
		})
	}
}

func TestDistanciaHamming(t *testing.T) {
	// Regra crítica: os dois CNPJs conhecidos diferem em exatamente 3 dígitos,
	// que é o próprio limite de sugestão. Se esta distância mudar, a regra de
	// empate precisa ser revista.
	if d := DistanciaHamming(cnpjMaternidade, cnpjDonaIris); d != 3 {
		t.Fatalf("distância Maternidade↔Dona Íris = %d, quer 3", d)
	}
	if d := DistanciaHamming("123", "12345"); d != -1 {
		t.Errorf("tamanhos diferentes = %d, quer -1", d)
	}
	if d := DistanciaHamming(cnpjMaternidade, cnpjMaternidade); d != 0 {
		t.Errorf("iguais = %d, quer 0", d)
	}
}

func TestSugereCNPJ(t *testing.T) {
	casos := []struct {
		nome       string
		lido       string
		querOK     bool
		querCNPJ   string
		querEmpate bool
	}{
		{
			nome:     "um dígito trocado na maternidade",
			lido:     "37678845000735", // DV quebrado, 1 dígito do real
			querOK:   true,
			querCNPJ: cnpjMaternidade,
		},
		{
			nome: "equidistante dos dois conhecidos não sugere nada",
			// Distância 2 para cada um dos dois conhecidos: é o empate que o
			// CLAUDE.md §2 proíbe resolver automaticamente.
			lido:       "37678845000709",
			querOK:     false,
			querEmpate: true,
		},
		{
			nome:   "longe de todo mundo",
			lido:   "11222333000181",
			querOK: false,
		},
		{
			nome:   "tamanho inválido",
			lido:   "376788",
			querOK: false,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s, ok := SugereCNPJ(c.lido, conhecidos())
			if ok != c.querOK {
				t.Fatalf("SugereCNPJ(%q) ok = %v, quer %v (sugestão %+v)", c.lido, ok, c.querOK, s)
			}
			if c.querOK && s.CNPJ != c.querCNPJ {
				t.Errorf("sugeriu %q, quer %q", s.CNPJ, c.querCNPJ)
			}
			if c.querEmpate && !s.Empate {
				t.Errorf("esperava empate sinalizado, veio %+v", s)
			}
		})
	}
}

func TestSugereCNPJNaoSugereConhecidoComDVQuebrado(t *testing.T) {
	// Um cadastro com CNPJ inválido jamais pode virar sugestão.
	if s, ok := SugereCNPJ("37678845000735", []string{"37678845000799"}); ok {
		t.Errorf("sugeriu um conhecido com DV inválido: %+v", s)
	}
}

func TestValidaCNPJCliente(t *testing.T) {
	t.Run("válido não gera incerteza", func(t *testing.T) {
		cnpj, incerto, avisos := ValidaCNPJCliente("37.678.845/0003-02", conhecidos())
		if cnpj != cnpjDonaIris || incerto || len(avisos) != 0 {
			t.Errorf("= %q, %v, %v", cnpj, incerto, avisos)
		}
	})

	t.Run("inválido mantém o lido e marca incerto", func(t *testing.T) {
		// Nunca sobrescrever o lido: a correção é decisão humana.
		cnpj, incerto, avisos := ValidaCNPJCliente("37678845000735", conhecidos())
		if cnpj != "37678845000735" {
			t.Errorf("CNPJ gravado = %q, quer o lido inalterado", cnpj)
		}
		if !incerto {
			t.Error("deveria ser incerto")
		}
		if len(avisos) < 2 {
			t.Errorf("esperava aviso de DV + sugestão, veio %v", avisos)
		}
	})

	t.Run("empate não sugere nada", func(t *testing.T) {
		_, incerto, avisos := ValidaCNPJCliente("37678845000709", conhecidos())
		if !incerto {
			t.Error("deveria ser incerto")
		}
		for _, a := range avisos {
			if strings.Contains(a, "Sugestão") {
				t.Errorf("não deveria sugerir em caso de empate: %q", a)
			}
		}
	})

	t.Run("não lido", func(t *testing.T) {
		_, incerto, avisos := ValidaCNPJCliente("", conhecidos())
		if !incerto || len(avisos) == 0 {
			t.Errorf("= %v, %v", incerto, avisos)
		}
	})
}

func TestValidaCNPJEmitente(t *testing.T) {
	t.Run("exato", func(t *testing.T) {
		cnpj, avisos := ValidaCNPJEmitente("29.220.487/0001-08")
		if cnpj != dominio.CNPJEmitente || len(avisos) != 0 {
			t.Errorf("= %q, %v", cnpj, avisos)
		}
	})

	t.Run("divergência leve é ajustada com aviso", func(t *testing.T) {
		cnpj, avisos := ValidaCNPJEmitente("29220487000107")
		if cnpj != dominio.CNPJEmitente {
			t.Errorf("= %q, quer ajuste para o oficial", cnpj)
		}
		if len(avisos) == 0 {
			t.Error("ajuste silencioso: precisa avisar")
		}
	})

	t.Run("outro emitente é mantido e avisado", func(t *testing.T) {
		cnpj, avisos := ValidaCNPJEmitente("11222333000181")
		if cnpj != "11222333000181" {
			t.Errorf("= %q, quer manter o lido", cnpj)
		}
		if len(avisos) == 0 {
			t.Error("precisa avisar que o emitente é outro")
		}
	})

	t.Run("não lido assume o oficial", func(t *testing.T) {
		cnpj, avisos := ValidaCNPJEmitente("")
		if cnpj != dominio.CNPJEmitente || len(avisos) == 0 {
			t.Errorf("= %q, %v", cnpj, avisos)
		}
	})
}
