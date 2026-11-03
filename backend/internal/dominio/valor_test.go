package dominio

import "testing"

func TestParseValor(t *testing.T) {
	casos := []struct {
		entrada string
		quer    Valor
		erro    bool
	}{
		{"1.234,56", 123456, false}, // pt-BR
		{"1234.56", 123456, false},  // ponto decimal
		{"1,234.56", 123456, false}, // en-US
		{"R$ 1.234,56", 123456, false},
		{"435,00", 43500, false},
		{"435", 43500, false},
		{"0,01", 1, false},
		{"-12,34", -1234, false},
		{"9,99", 999, false},
		{"7.225,77", 722577, false},
		{"11.500,46", 1150046, false},
		{"1,234", 0, true}, // 3 casas em valor monetário
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, c := range casos {
		got, err := ParseValor(c.entrada)
		if (err != nil) != c.erro {
			t.Errorf("ParseValor(%q) erro = %v, quer erro = %v", c.entrada, err, c.erro)
			continue
		}
		if !c.erro && got != c.quer {
			t.Errorf("ParseValor(%q) = %d, quer %d", c.entrada, got, c.quer)
		}
	}
}

func TestParseQtd(t *testing.T) {
	casos := []struct {
		entrada string
		quer    Qtd
		erro    bool
	}{
		{"120", 120000, false},
		{"1,340", 1340, false},
		{"0,88", 880, false},
		{"0,71", 710, false},
		{"1.340", 1340, false}, // ponto como decimal (en-US)
		{"12", 12000, false},
		{"1,2345", 0, true}, // 4 casas
	}
	for _, c := range casos {
		got, err := ParseQtd(c.entrada)
		if (err != nil) != c.erro {
			t.Errorf("ParseQtd(%q) erro = %v, quer erro = %v", c.entrada, err, c.erro)
			continue
		}
		if !c.erro && got != c.quer {
			t.Errorf("ParseQtd(%q) = %d, quer %d", c.entrada, got, c.quer)
		}
	}
}

func TestMultiplicar(t *testing.T) {
	// Os três casos de KG do xlsx de referência, que exigem arredondamento.
	casos := []struct {
		qtd, vun, quer string
	}{
		{"120", "1,00", "120,00"},
		{"110", "1,50", "165,00"},
		{"2", "9,99", "19,98"},
		{"1,340", "45,99", "61,63"}, // 61,6266
		{"0,88", "45,99", "40,47"},  // 40,4712
		{"0,71", "35,99", "25,55"},  // 25,5529
		{"75", "3,30", "247,50"},
		{"15", "3,70", "55,50"},
		{"0", "3,30", "0,00"},
	}
	for _, c := range casos {
		q, err := ParseQtd(c.qtd)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ParseValor(c.vun)
		if err != nil {
			t.Fatal(err)
		}
		quer, err := ParseValor(c.quer)
		if err != nil {
			t.Fatal(err)
		}
		if got := q.Multiplicar(v); got != quer {
			t.Errorf("%s × %s = %s, quer %s", c.qtd, c.vun, got, c.quer)
		}
	}
}

func TestValorString(t *testing.T) {
	casos := []struct {
		v    Valor
		quer string
	}{
		{0, "0,00"},
		{1, "0,01"},
		{43500, "435,00"},
		{722577, "7.225,77"},
		{1150046, "11.500,46"},
		{1872623, "18.726,23"},
		{-1234, "-12,34"},
		{-50, "-0,50"},
	}
	for _, c := range casos {
		if got := c.v.String(); got != c.quer {
			t.Errorf("Valor(%d).String() = %q, quer %q", c.v, got, c.quer)
		}
	}
}

func TestQtdString(t *testing.T) {
	casos := []struct {
		q    Qtd
		quer string
	}{
		{120000, "120"},
		{1340, "1,34"},
		{880, "0,88"},
		{2480000, "2.480"},
		{0, "0"},
	}
	for _, c := range casos {
		if got := c.q.String(); got != c.quer {
			t.Errorf("Qtd(%d).String() = %q, quer %q", c.q, got, c.quer)
		}
	}
}

func TestJSONPreservaCentavos(t *testing.T) {
	casos := []struct {
		v    Valor
		quer string
	}{
		{722577, "7225.77"},
		{43500, "435.00"},
		{1, "0.01"},
		{-50, "-0.50"}, // o sinal não pode desaparecer quando a parte inteira é 0
		{0, "0.00"},
	}
	for _, c := range casos {
		b, err := c.v.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.quer {
			t.Errorf("Valor(%d) em JSON = %s, quer %s", c.v, b, c.quer)
		}
		var volta Valor
		if err := volta.UnmarshalJSON(b); err != nil {
			t.Fatal(err)
		}
		if volta != c.v {
			t.Errorf("ida e volta de %d deu %d", c.v, volta)
		}
	}
}

func TestDataSemFuso(t *testing.T) {
	// No protótipo um bug de timezone fez datas aparecerem um dia antes.
	d, err := ParseData("2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	if d.String() != "2026-10-16" {
		t.Errorf("= %q", d.String())
	}
	if d.Formatada() != "16/10/2026" {
		t.Errorf("= %q", d.Formatada())
	}
	if d.Competencia() != "2026-10" {
		t.Errorf("= %q", d.Competencia())
	}

	d2, err := ParseData("16/10/2026")
	if err != nil {
		t.Fatal(err)
	}
	if d2 != d {
		t.Errorf("DD/MM/AAAA deu %v, AAAA-MM-DD deu %v", d2, d)
	}

	if NovaData(2026, 2, 31).Valida() {
		t.Error("31/02/2026 deveria ser inválida")
	}
	if !NovaData(2026, 10, 16).Valida() {
		t.Error("16/10/2026 deveria ser válida")
	}
}

func TestFormataCNPJ(t *testing.T) {
	if got := FormataCNPJ("29220487000108"); got != "29.220.487/0001-08" {
		t.Errorf("= %q", got)
	}
	if got := FormataCNPJ("123"); got != "123" {
		t.Errorf("= %q, quer devolver a entrada", got)
	}
	if got := SoDigitos("37.678.845/0003-02"); got != "37678845000302" {
		t.Errorf("= %q", got)
	}
}
