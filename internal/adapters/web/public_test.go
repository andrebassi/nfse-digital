package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestSignUpClosed: e-mail fora da lista do servidor volta à tela com o
// motivo e nenhuma conta é criada.
func TestSignUpClosed(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.server.service.SignUpEmails = map[string]bool{"dona@exemplo.example": true}
	form := url.Values{"name": {"Estranho"}, "email": {"estranho@exemplo.example"}, "password": {"senha-de-teste-longa"}}
	response := f.post("/signup", form)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "cadastro fechado") {
		t.Fatalf("cadastro fora da lista: %d", response.Code)
	}
	if _, err := f.repo.AccountByEmail(t.Context(), "estranho@exemplo.example"); err == nil {
		t.Fatal("conta criada apesar da recusa")
	}
}

// publicHost é o domínio de teste do deploy público.
const publicHost = "nfse.exemplo.example"

// TestUsePublicHost: com host público, o domínio passa no portão, um domínio
// qualquer continua recusado, os cookies saem Secure (HTTPS no proxy) e a
// passkey e as Integrações usam https://host. Sem host público, nada disso
// muda: cookie sem Secure, que o navegador aceita em http://127.0.0.1.
func TestUsePublicHost(t *testing.T) {
	f := newFixture(t, nil, nil)
	toggle := func(host string) *httptest.ResponseRecorder {
		form := url.Values{"csrf": {f.server.csrf}, "back": {"/invoices"}}
		request := httptest.NewRequest(http.MethodPost, "/preferences/hide-values", strings.NewReader(form.Encode()))
		request.Host = host
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(f.session)
		recorder := httptest.NewRecorder()
		f.handler.ServeHTTP(recorder, request)
		return recorder
	}
	if cookie := toggle("127.0.0.1:8735").Result().Cookies(); len(cookie) != 1 || cookie[0].Secure {
		t.Fatalf("local: cookie %+v", cookie)
	}
	if toggle(publicHost).Code != http.StatusForbidden {
		t.Fatal("host público aceito antes de UsePublicHost")
	}
	// Local, o header do proxy é ignorado: seria forjável por quem pede.
	forged := httptest.NewRequest(http.MethodPost, "/login", nil)
	forged.Header.Set("Fly-Client-IP", "203.0.113.9")
	if ip := f.server.clientIP(forged); ip != "" {
		t.Fatalf("local aceitou Fly-Client-IP: %q", ip)
	}

	if err := f.server.UsePublicHost(publicHost); err != nil {
		t.Fatal(err)
	}
	response := toggle(publicHost)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("host público: %d", response.Code)
	}
	if cookie := response.Result().Cookies(); len(cookie) != 1 || !cookie[0].Secure {
		t.Fatalf("público: cookie sem Secure %+v", cookie)
	}
	if toggle("atacante.example.com").Code != http.StatusForbidden {
		t.Fatal("domínio qualquer passou no portão")
	}
	if f.server.baseURL != "https://"+publicHost {
		t.Fatalf("baseURL: %s", f.server.baseURL)
	}
	if config := f.server.passkeys.webauthn.Config; config.RPID != publicHost || config.RPOrigins[0] != "https://"+publicHost {
		t.Fatalf("passkey: %s %v", config.RPID, config.RPOrigins)
	}
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.RemoteAddr = "[2001:db8::1]:51000"
	if ip := f.server.clientIP(request); ip != "2001:db8::1" {
		t.Errorf("IP sem header do proxy: %q", ip)
	}
	request.Header.Set("Fly-Client-IP", "203.0.113.9")
	if ip := f.server.clientIP(request); ip != "203.0.113.9" {
		t.Errorf("IP do proxy: %q", ip)
	}
	request.RemoteAddr = "sem-porta"
	request.Header.Del("Fly-Client-IP")
	if ip := f.server.clientIP(request); ip != "sem-porta" {
		t.Errorf("RemoteAddr sem porta: %q", ip)
	}
	for _, bad := range []string{"", "https://" + publicHost, publicHost + ":443", publicHost + "/painel"} {
		if err := f.server.UsePublicHost(bad); err == nil {
			t.Errorf("%q aceito", bad)
		}
	}
}
