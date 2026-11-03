// Package textsearch é a busca das telas: por trecho, sem diferenciar acento
// nem maiúscula. Fica separado para memória e SQLite usarem a mesma regra.
package textsearch

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Matches diz se algum dos campos contém a busca ("ipe" acha "IPÊ"). Busca
// vazia casa tudo.
func Matches(query string, fields ...string) bool {
	needle := Fold(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(Fold(field), needle) {
			return true
		}
	}
	return false
}

// Fold tira acento (decompõe e remove as marcas) e passa para minúsculas.
func Fold(text string) string {
	stripped, _, _ := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), text)
	return strings.ToLower(stripped)
}
