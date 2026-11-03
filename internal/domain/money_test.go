package domain

import (
	"errors"
	"testing"
)

// TestParseBRLAcceptsKnownFormats trava os formatos que a tela aceita, incluindo
// os que a tela mostra ("R$ 19.000,00") e o que a API usa ("19000.00").
func TestParseBRLAcceptsKnownFormats(t *testing.T) {
	cases := map[string]Money{
		"19000.00":           1900000,
		"19000,00":           1900000,
		"19.000,00":          1900000,
		"R$ 19.000,00":       1900000,
		"  R$19.000,00  ":    1900000,
		"10,5":               1050,
		"1.000":              100000,
		"0":                  0,
		"22885":              2288500,
		"1.234.567,89":       123456789,
		"000123,40":          12340,
		"999999999999999.99": 99999999999999999,
	}
	for input, want := range cases {
		got, err := ParseBRL(input)
		if err != nil || got != want {
			t.Errorf("ParseBRL(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
}

// TestParseBRLRejectsHostileInput garante que entrada ambígua ou estranha vira
// erro, em vez de um valor errado silencioso numa nota fiscal.
func TestParseBRLRejectsHostileInput(t *testing.T) {
	for _, input := range []string{"", "   ", "R$", "-10,00", "abc", "1.000.00", "10,555", "NaN", "1e3", "12.34.56", "1000000000000000,00"} {
		if _, err := ParseBRL(input); !errors.Is(err, ErrInvalidMoney) {
			t.Errorf("ParseBRL(%q) err = %v; want ErrInvalidMoney", input, err)
		}
	}
}

// TestFormatBRL confere o milhar, os centavos e o sinal.
func TestFormatBRL(t *testing.T) {
	cases := map[Money]string{
		0:         "R$ 0,00",
		5:         "R$ 0,05",
		1900000:   "R$ 19.000,00",
		123456789: "R$ 1.234.567,89",
		-1050:     "-R$ 10,50",
		100000:    "R$ 1.000,00",
		99900:     "R$ 999,00",
	}
	for value, want := range cases {
		if got := value.FormatBRL(); got != want {
			t.Errorf("Money(%d).FormatBRL() = %q; want %q", value, got, want)
		}
	}
}

// TestInputValueRoundTrip garante que o valor mostrado no formulário volta
// igual quando o dono salva sem mexer.
func TestInputValueRoundTrip(t *testing.T) {
	for _, value := range []Money{0, 1, 1900000, 2288500, 123456789} {
		back, err := ParseBRL(value.InputValue())
		if err != nil || back != value {
			t.Errorf("round trip de %d deu %d, %v", value, back, err)
		}
	}
}
