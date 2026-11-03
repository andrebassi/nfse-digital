package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// apiFixture é o painel com a API ligada (MCP por HTTP ligado) e um token válido.
type apiFixture struct {
	fixture
	token string
}

// newAPIFixture liga a API no painel de teste e gera um token para as chamadas.
func newAPIFixture(t *testing.T, emitter ports.Emitter, repo ports.Repository) apiFixture {
	t.Helper()
	f := newFixture(t, emitter, repo)
	f.server.UseMCP(http.NotFoundHandler())
	f.handler = f.server.Handler()
	token, _, err := f.server.service.IssueToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return apiFixture{fixture: f, token: token}
}

// call faz uma chamada à API com o token da fixture (auth vazio = sem cabeçalho)
// e devolve a resposta e o JSON decodificado (nil quando o corpo não é JSON).
func (f apiFixture) call(method, target, body string, auth ...string) (*httptest.ResponseRecorder, map[string]any) {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Host = "127.0.0.1:8735"
	header := "Bearer " + f.token
	if len(auth) > 0 {
		header = auth[0]
	}
	if header != "" {
		request.Header.Set("Authorization", header)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder, decoded
}

// emitBody monta o corpo da emissão da Ipê com a chave dada.
func (f apiFixture) emitBody(t *testing.T, key string, extra string) string {
	t.Helper()
	return `{"idempotency_key":"` + key + `","customer_id":` + itoa(f.customerID(t, "ipe")) + `,"template_id":` + itoa(f.templateID(t, "ipe")) + extra + `}`
}

// TestAPIGate: sem MCP ligado a API não existe (cai no painel, que pede
// sessão); com ele, token ausente/errado é 401 em JSON com WWW-Authenticate,
// Host de fora é 403, rota desconhecida é 404 em JSON, e as três rotas
// públicas abrem sem token.
func TestAPIGate(t *testing.T) {
	off := newFixture(t, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.Host = "127.0.0.1:8735"
	recorder := httptest.NewRecorder()
	off.handler.ServeHTTP(recorder, request)
	if strings.Contains(recorder.Header().Get("Content-Type"), "json") {
		t.Fatalf("API respondeu sem MCP ligado: %d", recorder.Code)
	}

	f := newAPIFixture(t, nil, nil)
	for name, auth := range map[string]string{"sem token": "", "token errado": "Bearer nf_errado", "sem Bearer": f.token} {
		response, body := f.call(http.MethodGet, "/api/v1/account", "", auth)
		if response.Code != http.StatusUnauthorized || body["error"] == nil || response.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d %v", name, response.Code, body)
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.Host = "atacante.example.com"
	request.Header.Set("Authorization", "Bearer "+f.token)
	recorder = httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("host de fora: %d", recorder.Code)
	}
	if response, body := f.call(http.MethodGet, "/api/v1/nada", ""); response.Code != http.StatusNotFound || !strings.Contains(body["error"].(string), "/api/docs") {
		t.Errorf("rota desconhecida: %d %v", response.Code, body)
	}
	for _, path := range []string{"/api/v1/openapi.json", "/api/docs", "/api/llms.txt"} {
		if response, _ := f.call(http.MethodGet, path, "", ""); response.Code != http.StatusOK {
			t.Errorf("%s sem token: %d", path, response.Code)
		}
	}
	broken := newAPIFixtureWithRepo(t, brokenTokens{memory.NewSeeded(brt)})
	if response, _ := broken.call(http.MethodGet, "/api/v1/account", "", "Bearer nf_qualquer"); response.Code != http.StatusInternalServerError {
		t.Errorf("banco fora ao conferir token: %d", response.Code)
	}
}

// newAPIFixtureWithRepo liga a API sobre um repositório que pode falhar ao
// gerar token: a fixture fica sem token válido.
func newAPIFixtureWithRepo(t *testing.T, repo ports.Repository) apiFixture {
	t.Helper()
	f := newFixture(t, nil, repo)
	f.server.UseMCP(http.NotFoundHandler())
	f.handler = f.server.Handler()
	return apiFixture{fixture: f}
}

// TestAPIContractMatchesRoutes: toda rota autenticada está no openapi.json e
// o openapi.json não descreve rota que não existe — nos dois sentidos, para
// a documentação nunca mentir sobre a API.
func TestAPIContractMatchesRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal(err)
	}
	var documented, routed []string
	for path, methods := range spec.Paths {
		for method := range methods {
			documented = append(documented, strings.ToUpper(method)+" "+path)
		}
	}
	for _, endpoint := range apiEndpoints {
		routed = append(routed, endpoint.Method+" "+endpoint.Path)
	}
	sort.Strings(documented)
	sort.Strings(routed)
	if strings.Join(documented, "\n") != strings.Join(routed, "\n") {
		t.Fatalf("contrato diverge das rotas\nopenapi.json:\n%s\nrotas:\n%s", strings.Join(documented, "\n"), strings.Join(routed, "\n"))
	}
}

// TestAPIPublicDocs: o contrato sai com o endereço deste painel em servers;
// a página do Scalar tem CSP própria (só script deste host); o llms.txt sai
// com URLs absolutas, sem nenhuma marca de template sobrando.
func TestAPIPublicDocs(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	_, spec := f.call(http.MethodGet, "/api/v1/openapi.json", "", "")
	servers, _ := spec["servers"].([]any)
	if len(servers) != 1 || servers[0].(map[string]any)["url"] != "http://127.0.0.1:8735/api/v1" {
		t.Fatalf("servers: %v", spec["servers"])
	}
	docs, _ := f.call(http.MethodGet, "/api/docs", "", "")
	csp := docs.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self';") || !strings.Contains(docs.Body.String(), "/static/vendor/scalar-api-reference-1.76.4.js") {
		t.Fatalf("docs: CSP %q", csp)
	}
	if script := f.get("/static/vendor/scalar-api-reference-1.76.4.js"); script.Code != http.StatusOK || script.Body.Len() < 1<<20 {
		t.Fatalf("Scalar vendorizado: %d, %d bytes", script.Code, script.Body.Len())
	}
	llms, _ := f.call(http.MethodGet, "/api/llms.txt", "", "")
	text := llms.Body.String()
	if !strings.Contains(text, "http://127.0.0.1:8735/api/v1/invoices") || strings.Contains(text, "{{") {
		t.Fatal("llms.txt sem URL absoluta ou com template sobrando")
	}
}

// TestAPIReadOnly: conta, tomadores, modelos e a listagem de notas com filtro
// e limite; limite e status fora da faixa são 400.
func TestAPIReadOnly(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	if response, body := f.call(http.MethodGet, "/api/v1/account", ""); response.Code != http.StatusOK || body["environment"] != "homologation" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("account: %d %v", response.Code, body)
	}
	if _, body := f.call(http.MethodGet, "/api/v1/customers?q=ipe", ""); len(body["customers"].([]any)) != 1 {
		t.Fatalf("customers: %v", body)
	}
	if _, body := f.call(http.MethodGet, "/api/v1/templates?q=zzz", ""); len(body["templates"].([]any)) != 0 {
		t.Fatalf("templates: %v", body)
	}
	if _, body := f.call(http.MethodGet, "/api/v1/templates", ""); len(body["templates"].([]any)) == 0 {
		t.Fatal("templates sem filtro veio vazio")
	}
	_, all := f.call(http.MethodGet, "/api/v1/invoices", "")
	if len(all["invoices"].([]any)) != 5 {
		t.Fatalf("seed tem 5 notas: %v", all)
	}
	if _, limited := f.call(http.MethodGet, "/api/v1/invoices?limit=2", ""); len(limited["invoices"].([]any)) != 2 {
		t.Fatal("limit=2 não cortou")
	}
	_, rejected := f.call(http.MethodGet, "/api/v1/invoices?status=rejected", "")
	for _, invoice := range rejected["invoices"].([]any) {
		if invoice.(map[string]any)["status"] != "rejected" {
			t.Fatalf("filtro de status deixou passar: %v", invoice)
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=x", "status=outro"} {
		if response, _ := f.call(http.MethodGet, "/api/v1/invoices?"+query, ""); response.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", query, response.Code)
		}
	}
	empty := newAPIFixture(t, nil, memory.New(domain.Issuer{}))
	if response, _ := empty.call(http.MethodGet, "/api/v1/account", ""); response.Code != http.StatusNotFound {
		t.Errorf("conta sem emitente: %d", response.Code)
	}
}

// TestAPIEmitIdempotent: nota nova é 201 com Location; a mesma chave de novo
// é 200 com a MESMA nota e nenhuma nota a mais; a consulta pela chave e pelo
// id devolve a nota; campos vazios vêm do modelo.
func TestAPIEmitIdempotent(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	ctx := context.Background()
	before, _ := f.repo.Invoices(ctx)
	created, first := f.call(http.MethodPost, "/api/v1/invoices", f.emitBody(t, "fatura-1", ""))
	if created.Code != http.StatusCreated || first["id"] == nil || !strings.HasSuffix(created.Header().Get("Location"), "/api/v1/invoices/"+jsonID(first)) {
		t.Fatalf("emitir: %d %v %q", created.Code, first, created.Header().Get("Location"))
	}
	// Sem amount/description: vale o modelo (valor padrão e descrição resolvida).
	if first["amount"] == "0.00" || first["description"] == "" || first["idempotency_key"] != "fatura-1" {
		t.Fatalf("padrões do modelo não aplicados: %v", first)
	}
	replay, second := f.call(http.MethodPost, "/api/v1/invoices", f.emitBody(t, "fatura-1", `,"amount":"999.00"`))
	if replay.Code != http.StatusOK || second["id"] != first["id"] {
		t.Fatalf("reenvio: %d %v", replay.Code, second)
	}
	if after, _ := f.repo.Invoices(ctx); len(after) != len(before)+1 {
		t.Fatalf("reenvio criou nota: %d → %d", len(before), len(after))
	}
	if _, byKey := f.call(http.MethodGet, "/api/v1/invoices/by-key?idempotency_key=fatura-1", ""); byKey["id"] != first["id"] {
		t.Fatalf("by-key: %v", byKey)
	}
	if _, byID := f.call(http.MethodGet, "/api/v1/invoices/"+jsonID(first), ""); byID["id"] != first["id"] {
		t.Fatalf("by-id: %v", byID)
	}
	for _, path := range []string{"/api/v1/invoices/99999", "/api/v1/invoices/abc", "/api/v1/invoices/0", "/api/v1/invoices/by-key?idempotency_key=outra", "/api/v1/invoices/by-key"} {
		if response, _ := f.call(http.MethodGet, path, ""); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, response.Code)
		}
	}
}

// jsonID devolve o id do JSON decodificado como texto.
func jsonID(body map[string]any) string {
	id, _ := body["id"].(float64)
	return itoa(int64(id))
}

// TestAPIEmitRefusals: corpo inválido e campo desconhecido são 400; sem
// chave, sem modelo ou com valor inválido é 422 com fields; produção sem
// confirm_production é 409 e nada é emitido; com a confirmação, emite.
func TestAPIEmitRefusals(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	ctx := context.Background()
	cases := []struct {
		name, body string
		status     int
		field      string
	}{
		{"JSON quebrado", `{`, http.StatusBadRequest, ""},
		{"campo desconhecido", f.emitBody(t, "k1", `,"confirmProduction":true`), http.StatusBadRequest, ""},
		{"sem chave", f.emitBody(t, " ", ""), http.StatusUnprocessableEntity, "idempotency_key"},
		{"modelo inexistente", `{"idempotency_key":"k2","customer_id":1,"template_id":99999}`, http.StatusUnprocessableEntity, "template_id"},
		{"valor inválido", f.emitBody(t, "k3", `,"amount":"abc"`), http.StatusUnprocessableEntity, "amount"},
	}
	for _, c := range cases {
		response, body := f.call(http.MethodPost, "/api/v1/invoices", c.body)
		if response.Code != c.status {
			t.Errorf("%s: %d %v", c.name, response.Code, body)
			continue
		}
		if c.field != "" {
			if fields, _ := body["fields"].(map[string]any); fields[c.field] == nil {
				t.Errorf("%s: sem fields.%s: %v", c.name, c.field, body)
			}
		}
	}
	issuer, _ := f.repo.Issuer(ctx)
	issuer.Environment = domain.EnvProduction
	_ = f.repo.SaveIssuer(ctx, issuer)
	before, _ := f.repo.Invoices(ctx)
	if response, _ := f.call(http.MethodPost, "/api/v1/invoices", f.emitBody(t, "prod-1", "")); response.Code != http.StatusConflict {
		t.Fatalf("produção sem confirmação: %d", response.Code)
	}
	if after, _ := f.repo.Invoices(ctx); len(after) != len(before) {
		t.Fatal("recusa de produção criou nota")
	}
	if response, _ := f.call(http.MethodPost, "/api/v1/invoices", f.emitBody(t, "prod-1", `,"confirm_production":true`)); response.Code != http.StatusCreated {
		t.Fatalf("produção confirmada: %d", response.Code)
	}
	broken := newAPIFixture(t, nil, failingInvoices{memory.NewSeeded(brt)})
	if response, _ := broken.call(http.MethodPost, "/api/v1/invoices", broken.emitBody(t, "k4", "")); response.Code != http.StatusInternalServerError {
		t.Errorf("banco fora na emissão: %d", response.Code)
	}
	if response, _ := broken.call(http.MethodGet, "/api/v1/invoices", ""); response.Code != http.StatusInternalServerError {
		t.Errorf("banco fora na listagem: %d", response.Code)
	}
}

// failingInvoices falha ao ler as notas (e o resto funciona).
type failingInvoices struct{ *memory.Repository }

// Invoices sempre falha.
func (failingInvoices) Invoices(context.Context) ([]domain.Invoice, error) {
	return nil, errors.New("banco fora do ar")
}

// TestAPIEmitPending: SEFIN sem resposta é 202 com a nota pendente e o aviso
// de consultar antes de tentar de novo.
func TestAPIEmitPending(t *testing.T) {
	f := newAPIFixture(t, timeoutEmitter{}, nil)
	response, body := f.call(http.MethodPost, "/api/v1/invoices", f.emitBody(t, "fatura-pendente", ""))
	if response.Code != http.StatusAccepted || body["status"] != "pending" || !strings.Contains(body["message"].(string), "consulte a nota") {
		t.Fatalf("pendente: %d %v", response.Code, body)
	}
}

// TestAPICancel: sem canceller é 503; aceito é 200 e cancelada; recusado pela
// SEFIN é 422 com a nota ainda autorizada; justificativa curta é 422 com
// fields; nota não autorizada e produção sem confirmação são 409; SEFIN fora
// do ar é 502; nota inexistente é 404.
func TestAPICancel(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	id := saveAuthorized(t, f.fixture, domain.EnvHomologation)
	path := "/api/v1/invoices/" + itoa(id) + "/cancel"
	good := `{"reason":"erro_emissao","description":"Nota de teste emitida por engano, sem serviço."}`
	if response, _ := f.call(http.MethodPost, path, good); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("sem canceller: %d", response.Code)
	}
	canceller := &recordingCanceller{result: ports.CancelResult{ErrorCode: "E1235", ErrorMessage: "fora do prazo"}}
	f.server.UseCanceller(canceller, "v")
	if response, _ := f.call(http.MethodPost, path, `{"motivo":"x"}`); response.Code != http.StatusBadRequest {
		t.Errorf("campo desconhecido: %d", response.Code)
	}
	if response, body := f.call(http.MethodPost, path, `{"reason":"erro_emissao","description":"curta"}`); response.Code != http.StatusUnprocessableEntity || body["fields"] == nil || canceller.calls != 0 {
		t.Errorf("justificativa curta: %d %v (idas %d)", response.Code, body, canceller.calls)
	}
	refused, body := f.call(http.MethodPost, path, good)
	invoice, _ := body["invoice"].(map[string]any)
	if refused.Code != http.StatusUnprocessableEntity || invoice["status"] != "authorized" || !strings.Contains(body["error"].(string), "recusou") {
		t.Fatalf("recusa: %d %v", refused.Code, body)
	}
	canceller.result, canceller.err = ports.CancelResult{}, errors.New("SEFIN fora do ar")
	if response, body := f.call(http.MethodPost, path, good); response.Code != http.StatusBadGateway || body["invoice"] == nil {
		t.Errorf("SEFIN fora: %d %v", response.Code, body)
	}
	canceller.result, canceller.err = ports.CancelResult{Registered: true}, nil
	if response, body := f.call(http.MethodPost, path, good); response.Code != http.StatusOK || body["status"] != "cancelled" {
		t.Fatalf("aceito: %d %v", response.Code, body)
	}
	if response, _ := f.call(http.MethodPost, path, good); response.Code != http.StatusConflict {
		t.Errorf("já cancelada: %d", response.Code)
	}
	// Chave de acesso diferente da de saveAuthorized: o banco recusa duas iguais.
	production := domain.Invoice{Environment: domain.EnvProduction, Status: domain.StatusAuthorized, CustomerID: 1, Amount: 100, AccessKey: "35057082112223330001810000000000000126110000000002", Series: "1", DPSNumber: 202635}
	if err := f.repo.SaveInvoice(context.Background(), &production); err != nil {
		t.Fatal(err)
	}
	if response, _ := f.call(http.MethodPost, "/api/v1/invoices/"+itoa(production.ID)+"/cancel", good); response.Code != http.StatusConflict {
		t.Errorf("produção sem confirmação: %d", response.Code)
	}
	for target, want := range map[string]int{"/api/v1/invoices/99999/cancel": http.StatusNotFound, "/api/v1/invoices/abc/cancel": http.StatusNotFound} {
		if response, _ := f.call(http.MethodPost, target, good); response.Code != want {
			t.Errorf("%s: %d", target, response.Code)
		}
	}
}

// TestAPIDANFSe: sem gerador é 503; nota não autorizada é 409; autorizada
// baixa o PDF; falha do gerador é 500; inexistente é 404.
func TestAPIDANFSe(t *testing.T) {
	f := newAPIFixture(t, nil, nil)
	id := saveAuthorized(t, f.fixture, domain.EnvHomologation)
	path := "/api/v1/invoices/" + itoa(id) + "/danfse"
	if response, _ := f.call(http.MethodGet, path, ""); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("sem gerador: %d", response.Code)
	}
	fail := false
	f.server.UseDANFSe(func(context.Context, domain.Invoice) ([]byte, string, error) {
		if fail {
			return nil, "", errors.New("XML ilegível")
		}
		return []byte("%PDF-1.7"), "danfse.pdf", nil
	})
	if response, _ := f.call(http.MethodGet, path, ""); response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/pdf" || response.Body.String() != "%PDF-1.7" {
		t.Fatalf("PDF: %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	invoices, _ := f.repo.Invoices(context.Background())
	for _, invoice := range invoices {
		if invoice.Status == domain.StatusRejected {
			if response, _ := f.call(http.MethodGet, "/api/v1/invoices/"+itoa(invoice.ID)+"/danfse", ""); response.Code != http.StatusConflict {
				t.Errorf("rejeitada: %d", response.Code)
			}
			break
		}
	}
	fail = true
	if response, _ := f.call(http.MethodGet, path, ""); response.Code != http.StatusInternalServerError {
		t.Errorf("gerador falhou: %d", response.Code)
	}
	for _, target := range []string{"/api/v1/invoices/99999/danfse", "/api/v1/invoices/x/danfse"} {
		if response, _ := f.call(http.MethodGet, target, ""); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", target, response.Code)
		}
	}
}

// TestIntegrationsAPITab: a aba API REST mostra URL base, autenticação e o
// exemplo de cada operação com o tomador e o modelo da conta; operação
// desconhecida é 404; sem MCP por HTTP a aba some; o "Gerar token" da aba
// volta nela com o token já dentro dos exemplos.
func TestIntegrationsAPITab(t *testing.T) {
	off := newFixture(t, nil, nil)
	if strings.Contains(off.get("/integrations").Body.String(), "API REST") || off.get("/integrations/api").Code != http.StatusNotFound {
		t.Fatal("aba API REST apareceu sem MCP por HTTP")
	}
	f := newAPIFixture(t, nil, nil)
	if !strings.Contains(f.get("/integrations").Body.String(), `href="/integrations/api"`) {
		t.Fatal("aba API REST fora da navegação")
	}
	customers, _ := f.repo.Customers(context.Background(), "")
	cases := map[string][]string{
		"/integrations/api":        {"http://127.0.0.1:8735/api/v1</p>", "Authorization: Bearer &lt;TOKEN&gt;", `"customer_id": ` + itoa(customers[0].ID), "Documentação da API", `href="/api/llms.txt"`, "/static/copy.js"},
		"/integrations/api/show":   {"by-key?idempotency_key=fatura-2026-11-0042", "/invoices/42"},
		"/integrations/api/list":   {"invoices?status=authorized&amp;limit=20"},
		"/integrations/api/cancel": {"/invoices/42/cancel", "servico_nao_prestado"},
	}
	for path, wants := range cases {
		response := f.get(path)
		for _, want := range wants {
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
				t.Errorf("%s (%d) sem %q", path, response.Code, want)
			}
		}
	}
	if response := f.get("/integrations/api/outra"); response.Code != http.StatusNotFound {
		t.Errorf("operação desconhecida: %d", response.Code)
	}
	generated := f.post("/integrations/tokens", url.Values{"section": {"api"}, "op": {"list"}}).Body.String()
	token := regexp.MustCompile(`nf_[A-Za-z0-9_-]{40,}`).FindString(generated)
	if token == "" || !strings.Contains(generated, "Authorization: Bearer "+token) || !strings.Contains(generated, `aria-current="page">Listar`) {
		t.Fatal("gerar token na aba API REST não voltou com o token nos exemplos")
	}
	// O token gerado na tela abre a API de verdade.
	if response, _ := f.call(http.MethodGet, "/api/v1/account", "", "Bearer "+token); response.Code != http.StatusOK {
		t.Fatalf("token da tela na API: %d", response.Code)
	}
}
