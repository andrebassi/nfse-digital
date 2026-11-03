// Package domain concentra as regras do emissor que não dependem de I/O:
// dinheiro, documentos, estados da nota, indicadores e calendário de agendamento.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Money é um valor em centavos de real. Inteiro de propósito: float64 erra
// centavo em soma de nota fiscal, e a SEFIN compara valor com duas casas exatas.
type Money int64

// ErrInvalidMoney indica texto que não pode ser lido como valor em reais sem ambiguidade.
var ErrInvalidMoney = errors.New("valor inválido")

// brazilianPattern casa o formato brasileiro: milhar com ponto opcional e até
// duas casas depois da vírgula ("19.000,00", "19000,5", "1.000").
var brazilianPattern = regexp.MustCompile(`^(\d{1,3}(\.\d{3})+|\d+)(,\d{1,2})?$`)

// decimalPointPattern casa número com ponto decimal e sem milhar ("19000.00").
// "1.000" não entra aqui (três casas): é milhar no formato brasileiro.
var decimalPointPattern = regexp.MustCompile(`^\d+\.\d{1,2}$`)

// maxIntegerDigits limita a parte inteira a 15 dígitos: acima disso o valor em
// centavos estoura int64, e nenhuma nota real chega perto.
const maxIntegerDigits = 15

// ParseBRL lê um valor digitado em reais. Aceita "19000.00", "19000,00",
// "19.000,00" e "R$ 19.000,00"; recusa vazio, negativo, letras e formato
// ambíguo como "1.000.00".
func ParseBRL(text string) (Money, error) {
	value := strings.TrimSpace(text)
	value = strings.TrimSpace(strings.TrimPrefix(value, "R$"))
	if value == "" {
		return 0, ErrInvalidMoney
	}

	var integer, fraction string
	switch {
	case decimalPointPattern.MatchString(value):
		integer, fraction, _ = strings.Cut(value, ".")
	case brazilianPattern.MatchString(value):
		integer, fraction, _ = strings.Cut(value, ",")
		integer = strings.ReplaceAll(integer, ".", "")
	default:
		return 0, fmt.Errorf("%w: %q", ErrInvalidMoney, text)
	}

	integer = strings.TrimLeft(integer, "0")
	if len(integer) > maxIntegerDigits {
		return 0, fmt.Errorf("%w: valor alto demais", ErrInvalidMoney)
	}
	// Uma casa decimal ("10,5") vale dez centavos por dígito.
	fraction = (fraction + "00")[:2]

	// O erro é ignorado de propósito: os padrões acima garantem só dígitos, e
	// 15 dígitos inteiros + 2 de centavos (18) cabem sempre em int64 (19).
	cents, _ := strconv.ParseInt("0"+integer+fraction, 10, 64)
	return Money(cents), nil
}

// FormatBRL devolve o valor no formato de exibição brasileiro, "R$ 19.000,00".
func (m Money) FormatBRL() string {
	sign := ""
	cents := int64(m)
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	integer := strconv.FormatInt(cents/100, 10)

	// Insere o ponto de milhar da direita para a esquerda.
	var grouped strings.Builder
	for i, digit := range integer {
		if i > 0 && (len(integer)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	return fmt.Sprintf("%sR$ %s,%02d", sign, grouped.String(), cents%100)
}

// InputValue devolve o valor no formato aceito de volta pelo ParseBRL e mais
// legível num campo de formulário ("19.000,00", sem o prefixo R$).
func (m Money) InputValue() string {
	return strings.TrimPrefix(m.FormatBRL(), "R$ ")
}
