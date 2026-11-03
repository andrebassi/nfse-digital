package domain

import (
	"errors"
	"regexp"
	"testing"
)

// TestParsePercent: aceita o que a pessoa digita (vírgula, ponto, "%", uma
// casa) e recusa o que o XSD não aceitaria — três dígitos inteiros, três
// casas, sinal, texto.
func TestParsePercent(t *testing.T) {
	valid := map[string]Percent{
		"": 0, "  ": 0, "0": 0, "6": 600, "6,00": 600, "6.00": 600, "6,5": 650, "15,52%": 1552, " 99,99 ": 9999, "0,07": 7,
	}
	for text, want := range valid {
		if got, err := ParsePercent(text); err != nil || got != want {
			t.Errorf("%q: %d, %v (esperado %d)", text, got, err, want)
		}
	}
	for _, text := range []string{"100", "6,123", "-1", "abc", "6,", ",5", "1.2.3", "6,a"} {
		if _, err := ParsePercent(text); !errors.Is(err, ErrInvalidPercent) {
			t.Errorf("%q passou: %v", text, err)
		}
	}
}

// TestPercentFormat: o XML sai no padrão exato do TSDec2V2 — que recusa
// "0.00" e zero à esquerda — e o campo do formulário mostra vírgula.
func TestPercentFormat(t *testing.T) {
	// Padrão copiado do XSD (tiposSimples_v1.01.xsd, TSDec2V2), ancorado.
	pattern := regexp.MustCompile(`^(0|0\.[0-9]{2}|[1-9]{1}[0-9]{0,1}(\.[0-9]{2})?)$`)
	cases := map[Percent]string{0: "0", 7: "0.07", 50: "0.50", 600: "6.00", 1552: "15.52", 9999: "99.99"}
	for value, want := range cases {
		got := value.FormatDecimal()
		if got != want || !pattern.MatchString(got) {
			t.Errorf("%d: %q (esperado %q, casa no XSD: %v)", value, got, want, pattern.MatchString(got))
		}
	}
	if Percent(600).InputValue() != "6,00" || Percent(0).InputValue() != "" {
		t.Error("valor do campo do formulário")
	}
}
