package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"nfse/internal/ports"
)

// TestDeleteAccount: a aba Segurança oferece excluir; e-mail errado não
// apaga nada; com o e-mail certo a conta e os dados somem, o cookie é
// apagado, a sessão aberta em OUTRO navegador também cai, e a entrada volta
// com o aviso.
func TestDeleteAccount(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	if body := f.get("/settings/security").Body.String(); !strings.Contains(body, "Excluir conta e todos os dados") || !strings.Contains(body, "dona@exemplo.example") {
		t.Fatal("Segurança sem a opção de excluir a conta")
	}
	account, _ := f.repo.AccountByEmail(ctx, "dona@exemplo.example")
	otherBrowser := &http.Cookie{Name: sessionCookie, Value: f.server.sessions.create(f.server.now(), account.ID)}

	wrong := f.post("/settings/security/delete-account", url.Values{"confirm_email": {"outra@exemplo.example"}})
	if location, _ := url.QueryUnescape(wrong.Header().Get("Location")); !strings.Contains(location, "digite o e-mail desta conta") {
		t.Fatalf("e-mail errado: %s", location)
	}
	if _, err := f.repo.AccountByEmail(ctx, "dona@exemplo.example"); err != nil {
		t.Fatal("e-mail errado apagou a conta")
	}

	deleted := f.post("/settings/security/delete-account", url.Values{"confirm_email": {" Dona@Exemplo.example "}})
	location, _ := url.QueryUnescape(deleted.Header().Get("Location"))
	if deleted.Code != http.StatusSeeOther || !strings.Contains(location, "Conta excluída") {
		t.Fatalf("excluir: %d %s", deleted.Code, location)
	}
	if cookies := deleted.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("o cookie da sessão não foi apagado: %+v", cookies)
	}
	if _, err := f.repo.AccountByEmail(ctx, "dona@exemplo.example"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a conta continua existindo: %v", err)
	}
	if customers, _ := f.repo.Customers(ctx, ""); len(customers) != 0 {
		t.Fatalf("sobraram %d tomadores", len(customers))
	}
	for _, cookie := range []*http.Cookie{f.session, otherBrowser} {
		request := f.get("/invoices", cookie)
		if request.Code != http.StatusSeeOther || strings.HasPrefix(request.Header().Get("Location"), "/invoices") {
			t.Errorf("sessão de conta excluída ainda abre o painel: %d", request.Code)
		}
	}
}
