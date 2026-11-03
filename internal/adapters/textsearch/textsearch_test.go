package textsearch

import "testing"

// TestMatches acha sem acento e sem maiúscula, casa tudo com busca vazia e
// recusa o que não contém o trecho.
func TestMatches(t *testing.T) {
	cases := []struct {
		query  string
		fields []string
		want   bool
	}{
		{"ipe", []string{"IPÊ INFORMÁTICA LTDA"}, true},
		{"INFORMATICA", []string{"x", "Ipê Informática"}, true},
		{"  ", []string{"qualquer"}, true},
		{"zzz", []string{"IPÊ"}, false},
		{"ipe", nil, false},
	}
	for _, c := range cases {
		if got := Matches(c.query, c.fields...); got != c.want {
			t.Errorf("Matches(%q, %v) = %v", c.query, c.fields, got)
		}
	}
}
