package simulated

import (
	"context"
	"strings"
	"testing"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestBarueriIsRejectedWithE0039 reproduz a resposta real da SEFIN de 28/10/2026.
func TestBarueriIsRejectedWithE0039(t *testing.T) {
	result, err := Emitter{}.Emit(context.Background(), ports.EmitRequest{Issuer: domain.Issuer{CityCode: "3505708"}})
	if err != nil || result.Status != domain.StatusRejected || result.ErrorCode != "E0039" || !result.Simulated {
		t.Fatalf("resultado: %+v, %v", result, err)
	}
}

// TestOtherCityIsAuthorizedWithFakeKey: a chave fictícia começa com SIM e nunca
// tem o formato de uma chave real (50 dígitos).
func TestOtherCityIsAuthorizedWithFakeKey(t *testing.T) {
	request := ports.EmitRequest{Issuer: domain.Issuer{CityCode: "3550308"}, Invoice: domain.Invoice{DPSNumber: 202629}}
	result, err := Emitter{}.Emit(context.Background(), request)
	if err != nil || result.Status != domain.StatusAuthorized || result.NFSeNumber != "202629" {
		t.Fatalf("resultado: %+v, %v", result, err)
	}
	if !strings.HasPrefix(result.AccessKey, "SIM") {
		t.Fatalf("chave sem prefixo SIM: %s", result.AccessKey)
	}
}
