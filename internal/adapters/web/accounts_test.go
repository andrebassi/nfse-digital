package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/adapters/sqlite"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestAccountsSeeOnlyTheirOwnData: duas contas criadas pelo painel num banco
// SQLite de verdade; cada uma configura a própria empresa e emite; a lista de
// notas, o detalhe por ID e o token MCP de uma nunca alcançam a outra.
func TestAccountsSeeOnlyTheirOwnData(t *testing.T) {
	database, err := sqlite.Open(filepath.Join(t.TempDir(), "nfse.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, brt)
	service := &app.Service{Repo: database, Emitter: simulated.Emitter{}, Location: brt, Now: func() time.Time { return now }}
	server, err := New(service, testPort)
	if err != nil {
		t.Fatal(err)
	}
	f := authFixture{server: server, handler: server.Handler(), now: &now}

	// Cadastro das duas contas pelo próprio painel.
	sessions := map[string]*http.Cookie{}
	for _, email := range []string{"alice@exemplo.example", "bruno@exemplo.example"} {
		created := f.do(http.MethodPost, "/signup", url.Values{"name": {email}, "email": {email}, "password": {"senha-de-teste-longa"}})
		sessions[email] = sessionFrom(t, created)
	}
	accounts, _ := database.Accounts(context.Background())
	if len(accounts) != 2 {
		t.Fatalf("contas: %d", len(accounts))
	}

	// Cada conta grava a própria empresa e uma nota, direto pelo repositório
	// com a conta no context — como o painel faz.
	for i, account := range accounts {
		ctx := ports.WithAccount(context.Background(), account.ID)
		issuer := memory.DemoIssuer()
		issuer.LegalName = []string{"EMPRESA ALICE", "EMPRESA BRUNO"}[i]
		_ = database.SaveIssuer(ctx, issuer)
		customer := domain.Customer{Name: issuer.LegalName + " CLIENTE", Document: "56789012000100"}
		_ = database.SaveCustomer(ctx, &customer)
		invoice := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: 7, CustomerID: customer.ID, Amount: 100,
			Status: domain.StatusAuthorized, IssuedAt: now, Competence: now, Description: issuer.LegalName}
		_ = database.SaveInvoice(ctx, &invoice)
	}

	aliceCtx := ports.WithAccount(context.Background(), accounts[0].ID)
	aliceInvoices, _ := database.Invoices(aliceCtx)
	alice, bruno := sessions["alice@exemplo.example"], sessions["bruno@exemplo.example"]

	if body := f.do(http.MethodGet, "/invoices?period=90", nil, alice).Body.String(); !strings.Contains(body, "EMPRESA ALICE CLIENTE") || strings.Contains(body, "EMPRESA BRUNO") {
		t.Fatal("a lista da Alice mostra dado do Bruno, ou não mostra o dela")
	}
	if body := f.do(http.MethodGet, "/invoices?period=90", nil, bruno).Body.String(); !strings.Contains(body, "EMPRESA BRUNO CLIENTE") || strings.Contains(body, "EMPRESA ALICE") {
		t.Fatal("a lista do Bruno mostra dado da Alice, ou não mostra o dele")
	}
	// Abrir a nota da Alice pelo ID, logado como Bruno: não aparece.
	if body := f.do(http.MethodGet, "/invoices?period=90&id="+itoa(aliceInvoices[0].ID), nil, bruno).Body.String(); strings.Contains(body, "EMPRESA ALICE") {
		t.Fatal("o Bruno abriu a nota da Alice pelo ID")
	}
	if body := f.do(http.MethodGet, "/settings", nil, bruno).Body.String(); !strings.Contains(body, "EMPRESA BRUNO") || strings.Contains(body, "EMPRESA ALICE") {
		t.Fatal("configurações do Bruno com o emitente errado")
	}

	// Token MCP: gerado na sessão do Bruno, chega ao MCP com a conta dele.
	var reachedAccount int64
	server.UseMCP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedAccount = ports.AccountID(r.Context())
		w.Header().Set("Mcp-Session-Id", "sessao-do-bruno")
		_, _ = w.Write([]byte(`{}`))
	}))
	f.handler = server.Handler()
	plain, _, err := service.IssueToken(ports.WithAccount(context.Background(), accounts[1].ID))
	if err != nil {
		t.Fatal(err)
	}
	call := func(token, session string) int {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
		request.Host = "127.0.0.1:8735"
		request.Header.Set("Authorization", "Bearer "+token)
		if session != "" {
			request.Header.Set("Mcp-Session-Id", session)
		}
		recorder := httptest.NewRecorder()
		f.handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := call(plain, ""); code != http.StatusOK || reachedAccount != accounts[1].ID {
		t.Fatalf("MCP do Bruno: %d, conta %d", code, reachedAccount)
	}
	// A sessão aberta pelo token do Bruno não serve ao token da Alice.
	alicePlain, _, _ := service.IssueToken(aliceCtx)
	if code := call(alicePlain, "sessao-do-bruno"); code != http.StatusForbidden {
		t.Fatalf("token da Alice na sessão do Bruno: %d", code)
	}
	if code := call(plain, "sessao-do-bruno"); code != http.StatusOK {
		t.Fatalf("token do Bruno na própria sessão: %d", code)
	}
}
