package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
)

// TestHeaderClockAndSwitch: o cabeçalho mostra o relógio em Brasília, o
// seletor com homologação ativa e o link de produção, e o olho de ocultar.
func TestHeaderClockAndSwitch(t *testing.T) {
	if got := headerClock(time.Date(2026, 10, 30, 1, 30, 0, 0, time.UTC)); got != "30 DE OUT. · 01:30" {
		t.Fatalf("relógio: %q", got)
	}
	if got := headerClock(time.Date(2026, 5, 1, 9, 5, 0, 0, time.UTC)); got != "1 DE MAIO · 09:05" {
		t.Fatalf("relógio de maio: %q", got)
	}
	f := newFixture(t, nil, nil)
	body := f.get("/invoices").Body.String()
	for _, want := range []string{"29 DE OUT. · 22:52", `env-homologation" aria-current="true">HOMOLOGAÇÃO`, `href="/environment/production"`, `aria-label="Ocultar valores"`} {
		if !strings.Contains(body, want) {
			t.Errorf("cabeçalho sem %q", want)
		}
	}
}

// TestEnvironmentSwitch: produção só com o CNPJ redigitado; voltar para
// homologação não pede nada; sem emitente, a troca manda configurar antes.
func TestEnvironmentSwitch(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	if body := f.get("/environment/production").Body.String(); !strings.Contains(body, "digite o CNPJ de <strong>EXEMPLO TECNOLOGIA LTDA</strong>") {
		t.Fatal("tela de confirmação sem o nome do emitente")
	}
	for _, typed := range []string{"", "56.789.012/0001-00"} {
		response := f.post("/environment/production", url.Values{"confirm_cnpj": {typed}})
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "não confere") {
			t.Errorf("CNPJ %q: %d", typed, response.Code)
		}
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvHomologation {
		t.Fatal("produção ligada sem confirmação")
	}
	if response := f.post("/environment/production", url.Values{"confirm_cnpj": {"11.222.333/0001-81"}}); response.Code != http.StatusSeeOther {
		t.Fatalf("confirmado: %d", response.Code)
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvProduction {
		t.Fatal("produção não foi gravada")
	}
	if body := f.get("/invoices").Body.String(); !strings.Contains(body, `env-production" aria-current="true">PRODUÇÃO`) || !strings.Contains(body, `action="/environment/homologation"`) {
		t.Fatal("cabeçalho não mostra produção ativa")
	}
	back := f.post("/environment/homologation", url.Values{"back": {"/templates"}})
	if back.Code != http.StatusSeeOther || back.Header().Get("Location") != "/templates" {
		t.Fatalf("voltar: %d %s", back.Code, back.Header().Get("Location"))
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvHomologation {
		t.Fatal("não voltou para homologação")
	}

	empty := newFixture(t, nil, memory.New(domain.Issuer{}))
	if response := empty.get("/environment/production"); response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "/settings") {
		t.Fatalf("sem emitente: %d", response.Code)
	}
	for _, path := range []string{"/environment/homologation", "/environment/production"} {
		if response := empty.post(path, url.Values{}); response.Code != http.StatusNotFound {
			t.Errorf("%s sem emitente: %d", path, response.Code)
		}
	}
}
