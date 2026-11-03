package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Percent é um percentual em centésimos de ponto (6,00% = 600), inteiro pela
// mesma razão que Money: sem float, 0,1 + 0,2 não vira 0,30000000000000004
// num campo que vai para o XML fiscal.
type Percent int64

// MaxPercent é o maior valor que o XSD aceita no TSDec2V2: dois dígitos
// inteiros e duas casas, ou seja, 99,99%.
const MaxPercent Percent = 9999

// ErrInvalidPercent recusa texto que não é um percentual de 0 a 99,99 com no
// máximo duas casas.
var ErrInvalidPercent = errors.New("percentual inválido: use de 0 a 99,99, com até duas casas")

// ParsePercent lê o percentual como a pessoa digita: "6", "6,5", "6,00" ou
// "6.00", com ou sem "%". Vazio é zero (campo não preenchido).
func ParsePercent(text string) (Percent, error) {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "%"))
	if text == "" {
		return 0, nil
	}
	text = strings.Replace(text, ",", ".", 1)
	whole, fraction, hasFraction := strings.Cut(text, ".")
	if whole == "" || len(whole) > 2 || !isDigits(whole, len(whole)) {
		return 0, ErrInvalidPercent
	}
	if hasFraction && (fraction == "" || len(fraction) > 2 || !isDigits(fraction, len(fraction))) {
		return 0, ErrInvalidPercent
	}
	// Uma casa ("6,5") vale cinquenta centésimos, não cinco.
	for len(fraction) < 2 {
		fraction += "0"
	}
	units, _ := strconv.ParseInt(whole, 10, 64)
	hundredths, _ := strconv.ParseInt(fraction, 10, 64)
	return Percent(units*100 + hundredths), nil
}

// FormatDecimal escreve no formato do XSD (TSDec2V2): "0", "0.50" ou "6.00".
// O padrão do esquema não aceita "0.00" nem zero à esquerda ("06.00").
func (p Percent) FormatDecimal() string {
	if p == 0 {
		return "0"
	}
	return fmt.Sprintf("%d.%02d", p/100, p%100)
}

// InputValue é o que o campo do formulário mostra: "6,00", ou vazio quando
// ainda não foi informado.
func (p Percent) InputValue() string {
	if p == 0 {
		return ""
	}
	return fmt.Sprintf("%d,%02d", p/100, p%100)
}
