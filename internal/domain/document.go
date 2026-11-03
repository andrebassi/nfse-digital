package domain

import (
	"errors"
	"strings"
)

// DocumentKind diz se o documento é de pessoa física (CPF) ou jurídica (CNPJ).
type DocumentKind int

const (
	// KindCPF é o documento de pessoa física, 11 dígitos.
	KindCPF DocumentKind = iota + 1
	// KindCNPJ é o documento de pessoa jurídica, 14 dígitos.
	KindCNPJ
)

// ErrInvalidDocument indica CPF ou CNPJ com tamanho, caractere ou dígito verificador errado.
var ErrInvalidDocument = errors.New("CPF/CNPJ inválido")

// Document é um CPF ou CNPJ já validado, guardado só com os dígitos.
type Document struct {
	digits string
	kind   DocumentKind
}

// ParseDocument valida um CPF ou CNPJ com ou sem máscara. Aceita só dígitos,
// ponto, barra, hífen e espaço: qualquer outro caractere indica digitação
// errada, e descartá-lo em silêncio validaria o documento errado.
func ParseDocument(text string) (Document, error) {
	return parseDocument(text)
}

// ConfirmsCNPJ diz se o texto redigitado é o mesmo CNPJ do emitente, com ou
// sem pontuação. É a confirmação da troca para produção (story 4.1): exige
// lembrar e digitar o documento, o que um clique errado não faz. A quebra de
// linha do Enter no terminal é cortada antes de conferir.
func ConfirmsCNPJ(cnpj, typed string) bool {
	document, err := parseDocument(strings.TrimSpace(typed))
	return err == nil && document.Kind() == KindCNPJ && document.Digits() == cnpj
}

// parseDocument é o corpo de ParseDocument, separado para ConfirmsCNPJ.
func parseDocument(text string) (Document, error) {
	var digits strings.Builder
	for _, char := range text {
		switch {
		case char >= '0' && char <= '9':
			digits.WriteRune(char)
		case strings.ContainsRune("./- ", char):
		default:
			return Document{}, ErrInvalidDocument
		}
	}
	value := digits.String()

	// Sequência repetida ("11111111111") passa no dígito verificador, mas não existe.
	if value == "" || strings.Count(value, value[:1]) == len(value) {
		return Document{}, ErrInvalidDocument
	}

	switch len(value) {
	case 11:
		if checkDigits(value[:9], []int{10, 9, 8, 7, 6, 5, 4, 3, 2}, []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2}) != value[9:] {
			return Document{}, ErrInvalidDocument
		}
		return Document{digits: value, kind: KindCPF}, nil
	case 14:
		if checkDigits(value[:12], []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}, []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) != value[12:] {
			return Document{}, ErrInvalidDocument
		}
		return Document{digits: value, kind: KindCNPJ}, nil
	default:
		return Document{}, ErrInvalidDocument
	}
}

// checkDigits calcula os dois dígitos verificadores pelo módulo 11, com os
// pesos de cada documento (CPF e CNPJ diferem só nos pesos).
func checkDigits(base string, firstWeights, secondWeights []int) string {
	first := mod11(base, firstWeights)
	second := mod11(base+string(rune('0'+first)), secondWeights)
	return string([]rune{rune('0' + first), rune('0' + second)})
}

// mod11 aplica a regra da Receita: resto menor que 2 vira dígito 0.
func mod11(digits string, weights []int) int {
	sum := 0
	for i, weight := range weights {
		sum += int(digits[i]-'0') * weight
	}
	rest := sum % 11
	if rest < 2 {
		return 0
	}
	return 11 - rest
}

// Digits devolve o documento só com números, como vai no XML da DPS.
func (d Document) Digits() string { return d.digits }

// Kind diz se o documento é CPF ou CNPJ.
func (d Document) Kind() DocumentKind { return d.kind }

// String formata com a máscara oficial: 000.000.000-00 ou 00.000.000/0000-00.
func (d Document) String() string {
	v := d.digits
	if d.kind == KindCPF {
		return v[:3] + "." + v[3:6] + "." + v[6:9] + "-" + v[9:]
	}
	return v[:2] + "." + v[2:5] + "." + v[5:8] + "/" + v[8:12] + "-" + v[12:]
}

// FormatDocument formata dígitos já guardados; devolve o texto original quando
// ele não é um documento válido (tomador no exterior, por exemplo).
func FormatDocument(digits string) string {
	document, err := ParseDocument(digits)
	if err != nil {
		return digits
	}
	return document.String()
}
