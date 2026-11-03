package domain

import (
	"testing"
	"time"
)

// TestConfirmsCNPJ: o CNPJ redigitado confirma com ou sem pontuação; outro
// CNPJ, CPF, texto vazio ou dígito verificador errado não confirmam.
func TestConfirmsCNPJ(t *testing.T) {
	cases := map[string]bool{
		"11222333000181":     true,
		"11.222.333/0001-81": true,
		"11222333000181\n":   true,
		"56789012000100":     false,
		"529.982.247-25":     false,
		"":                   false,
		"11222333000182":     false,
	}
	for typed, want := range cases {
		if got := ConfirmsCNPJ("11222333000181", typed); got != want {
			t.Errorf("ConfirmsCNPJ(%q) = %v, quer %v", typed, got, want)
		}
	}
}

// TestPortalAuthorized: autorizado só com a data gravada; zero é "não".
func TestPortalAuthorized(t *testing.T) {
	if (CertificateStatus{}).PortalAuthorized() {
		t.Error("estado vazio não pode estar autorizado")
	}
	if !(CertificateStatus{PortalAuthorizedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}).PortalAuthorized() {
		t.Error("com data gravada deveria estar autorizado")
	}
}
