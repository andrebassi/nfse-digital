package domain

import (
	"testing"
	"time"
)

// TestCertificateExpiry cobre válido, perto de vencer e vencido, com o
// vencimento real do A1 do dono (02/12/2026 13:03 GMT) como referência de forma.
func TestCertificateExpiry(t *testing.T) {
	notAfter := time.Date(2026, 12, 2, 13, 3, 2, 0, time.UTC)
	info := CertificateInfo{NotAfter: notAfter}
	cases := []struct {
		now           time.Time
		expired, soon bool
		days          int
	}{
		{time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC), false, false, 34},
		{time.Date(2026, 11, 3, 13, 3, 3, 0, time.UTC), false, true, 28},
		{time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC), true, false, 0},
	}
	for _, c := range cases {
		if info.Expired(c.now) != c.expired || info.ExpiresSoon(c.now) != c.soon || info.DaysLeft(c.now) != c.days {
			t.Errorf("%s: vencido %v, perto %v, dias %d", c.now, info.Expired(c.now), info.ExpiresSoon(c.now), info.DaysLeft(c.now))
		}
	}
}

// TestSplitICPCommonName separa razão social e CNPJ e recusa o que não é CNPJ.
func TestSplitICPCommonName(t *testing.T) {
	cases := map[string][2]string{
		"EXEMPLO TECNOLOGIA LTDA:11222333000181": {"EXEMPLO TECNOLOGIA LTDA", "11222333000181"},
		"SEM CNPJ":                               {"SEM CNPJ", ""},
		"NOME:123":                               {"NOME:123", ""},
		"PESSOA:52998224725":                     {"PESSOA:52998224725", ""},
	}
	for input, want := range cases {
		name, cnpj := SplitICPCommonName(input)
		if name != want[0] || cnpj != want[1] {
			t.Errorf("SplitICPCommonName(%q) = %q, %q", input, name, cnpj)
		}
	}
}
