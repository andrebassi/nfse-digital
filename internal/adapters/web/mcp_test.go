package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
)

// mcpCall faz uma chamada ao /mcp com o Host e o Authorization dados.
func (f fixture) mcpCall(host, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Host = host
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// TestTokenFlowAndMCPGate: gerar mostra o token uma vez, já dentro do comando;
// o /mcp recusa sem token, com token errado e com Host de fora, e aceita o
// token gerado; revogar derruba o token na hora.
func TestTokenFlowAndMCPGate(t *testing.T) {
	f := newFixture(t, nil, nil)
	reached := 0
	f.server.UseMCP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	f.handler = f.server.Handler()

	page := f.get("/integrations").Body.String()
	if !strings.Contains(page, "Gerar token") || !strings.Contains(f.get("/integrations/tokens").Body.String(), "Nenhum token ainda") ||
		!strings.Contains(page, `claude mcp add --transport http nfse-digital http://127.0.0.1:8735/mcp --header "Authorization: Bearer &lt;TOKEN&gt;"`) {
		t.Fatal("tela sem o fluxo de token")
	}
	generated := f.post("/integrations/tokens", url.Values{"agent": {"codex"}})
	body := generated.Body.String()
	token := regexp.MustCompile(`nf_[A-Za-z0-9_-]{40,}`).FindString(body)
	if generated.Code != http.StatusOK || token == "" || !strings.Contains(body, "Copie agora") ||
		!strings.Contains(body, `export NFSE_TOKEN="`+token+`"`) || !strings.Contains(body, "--bearer-token-env-var NFSE_TOKEN") {
		t.Fatalf("gerar: %d", generated.Code)
	}
	if strings.Contains(f.get("/integrations").Body.String(), token) {
		t.Fatal("o token apareceu de novo depois de gerado")
	}

	cases := []struct {
		name, host, auth string
		want             int
	}{
		{"sem token", "127.0.0.1:8735", "", http.StatusUnauthorized},
		{"token errado", "127.0.0.1:8735", "Bearer nf_errado", http.StatusUnauthorized},
		{"sem Bearer", "127.0.0.1:8735", token, http.StatusUnauthorized},
		{"host de fora", "atacante.example.com", "Bearer " + token, http.StatusForbidden},
		{"válido", "127.0.0.1:8735", "Bearer " + token, http.StatusOK},
	}
	for _, c := range cases {
		if response := f.mcpCall(c.host, c.auth); response.Code != c.want {
			t.Errorf("%s: %d", c.name, response.Code)
		}
	}
	if reached != 1 {
		t.Fatalf("o MCP foi alcançado %d vez(es); só o válido devia passar", reached)
	}
	tokens, _ := f.repo.APITokens(context.Background())
	listing := f.get("/integrations/tokens").Body.String()
	if len(tokens) != 1 || tokens[0].LastUsedAt.IsZero() || !strings.Contains(listing, "nf_…"+token[len(token)-4:]) {
		t.Fatalf("lista de tokens: %+v", tokens)
	}
	revoked := f.post("/integrations/tokens/"+itoa(tokens[0].ID)+"/revoke", url.Values{})
	if revoked.Code != http.StatusSeeOther {
		t.Fatalf("revogar: %d", revoked.Code)
	}
	if response := f.mcpCall("127.0.0.1:8735", "Bearer "+token); response.Code != http.StatusUnauthorized {
		t.Fatalf("token revogado ainda abre: %d", response.Code)
	}
	if response := f.post("/integrations/tokens/999/revoke", url.Values{}); response.Code != http.StatusNotFound {
		t.Fatalf("revogar inexistente: %d", response.Code)
	}
}

// TestMCPGateErrors: sem MCP ligado, /mcp cai no painel (exige sessão);
// falha do banco ao conferir vira 500, não 401; falha ao gerar vira 500.
func TestMCPGateErrors(t *testing.T) {
	f := newFixture(t, nil, nil)
	if response := f.mcpCall("127.0.0.1:8735", "Bearer nf_x"); response.Code == http.StatusOK || response.Code == http.StatusUnauthorized {
		t.Fatalf("sem MCP ligado: %d", response.Code)
	}
	broken := newFixture(t, nil, brokenTokens{memory.NewSeeded(brt)})
	broken.server.UseMCP(http.NotFoundHandler())
	broken.handler = broken.server.Handler()
	if response := broken.mcpCall("127.0.0.1:8735", "Bearer nf_qualquer"); response.Code != http.StatusInternalServerError {
		t.Fatalf("banco fora: %d", response.Code)
	}
	if response := broken.post("/integrations/tokens", url.Values{}); response.Code != http.StatusInternalServerError {
		t.Fatalf("gerar com banco fora: %d", response.Code)
	}
}

// brokenTokens falha nas operações de token.
type brokenTokens struct{ *memory.Repository }

// APITokenByHash falha sempre.
func (brokenTokens) APITokenByHash(context.Context, string) (domain.APIToken, error) {
	return domain.APIToken{}, errors.New("banco fora")
}

// CreateAPIToken falha sempre.
func (brokenTokens) CreateAPIToken(context.Context, string, *domain.APIToken) error {
	return errors.New("banco fora")
}
