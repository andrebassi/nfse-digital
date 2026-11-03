package domain

import (
	"errors"
	"testing"
)

// TestParseDocumentAcceptsValidDocuments usa os CNPJs fictícios do protótipo
// (dígito verificador válido) e o CPF de exemplo público 529.982.247-25.
func TestParseDocumentAcceptsValidDocuments(t *testing.T) {
	cases := map[string]string{
		"11.222.333/0001-81": "11.222.333/0001-81",
		"12345678000195":     "12.345.678/0001-95",
		"23.456.789/0001-95": "23.456.789/0001-95",
		"34567890000130":     "34.567.890/0001-30",
		"45 678 901 0001 75": "45.678.901/0001-75",
		"56.789.012/0001-00": "56.789.012/0001-00",
		"529.982.247-25":     "529.982.247-25",
	}
	for input, want := range cases {
		document, err := ParseDocument(input)
		if err != nil || document.String() != want {
			t.Errorf("ParseDocument(%q) = %q, %v; want %q", input, document.String(), err, want)
		}
	}
}

// TestParseDocumentKindAndDigits confere o tipo e a forma sem máscara que vai no XML.
func TestParseDocumentKindAndDigits(t *testing.T) {
	cnpj, _ := ParseDocument("56.789.012/0001-00")
	cpf, _ := ParseDocument("529.982.247-25")
	if cnpj.Kind() != KindCNPJ || cnpj.Digits() != "56789012000100" {
		t.Errorf("CNPJ: kind %v digits %q", cnpj.Kind(), cnpj.Digits())
	}
	if cpf.Kind() != KindCPF || cpf.Digits() != "52998224725" {
		t.Errorf("CPF: kind %v digits %q", cpf.Kind(), cpf.Digits())
	}
}

// TestParseDocumentRejectsInvalid cobre dígito errado, tamanho errado,
// sequência repetida e caractere estranho no meio.
func TestParseDocumentRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "   ", "56.789.012/0001-01", "529.982.247-26", "11111111111", "00000000000000", "1234", "56789012000100x", "56.789.012/0001-0B", "123456789012"} {
		if _, err := ParseDocument(input); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("ParseDocument(%q) err = %v; want ErrInvalidDocument", input, err)
		}
	}
}

// TestCheckDigitWithSmallRemainder exercita o ramo do resto menor que 2, que
// vira dígito 0; o CNPJ fictício 56.789.012/0001-00 tem os dois dígitos assim.
func TestCheckDigitWithSmallRemainder(t *testing.T) {
	if got := mod11("567890120001", []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}); got != 0 {
		t.Fatalf("primeiro dígito = %d; want 0", got)
	}
	if got := mod11("5678901200010", []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}); got != 0 {
		t.Fatalf("segundo dígito = %d; want 0", got)
	}
}

// TestFormatDocument formata documento válido e devolve cru o que não é.
func TestFormatDocument(t *testing.T) {
	if got := FormatDocument("56789012000100"); got != "56.789.012/0001-00" {
		t.Errorf("got %q", got)
	}
	if got := FormatDocument("NIF-PT-123"); got != "NIF-PT-123" {
		t.Errorf("got %q", got)
	}
}
