package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// testPort é a porta usada nos testes; só entra no Host esperado.
const testPort = 8735

// fixture junta servidor, handler e repositório para cada teste.
type fixture struct {
	server  *Server
	handler http.Handler
	repo    *memory.Repository
	session *http.Cookie
}

// newFixture monta o painel com o seed real, emissor simulado e relógio fixo
// em 29/10/2026 22:52, o momento das medições de referência.
func newFixture(t *testing.T, emitter ports.Emitter, repo ports.Repository) fixture {
	t.Helper()
	seeded := memory.NewSeeded(brt)
	if repo == nil {
		repo = seeded
	}
	if emitter == nil {
		emitter = simulated.Emitter{}
	}
	service := &app.Service{Repo: repo, Emitter: emitter, Location: brt, Now: func() time.Time { return time.Date(2026, 10, 29, 22, 52, 0, 0, brt) }}
	server, err := New(service, testPort)
	if err != nil {
		t.Fatal(err)
	}
	// A conta e a sessão nascem prontas: os testes de tela não são sobre login.
	owner := domain.Account{Name: "Dona Exemplo", Email: "dona@exemplo.example"}
	_ = seeded.CreateAccount(context.Background(), &owner)
	session := &http.Cookie{Name: sessionCookie, Value: server.sessions.create(service.Now(), owner.ID)}
	return fixture{server: server, handler: server.Handler(), repo: seeded, session: session}
}

// get faz um GET com o Host certo.
func (f fixture) get(target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Host = "127.0.0.1:8735"
	request.AddCookie(f.session)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// post faz um POST de formulário com o Host certo e o token CSRF do servidor.
func (f fixture) post(target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	if form.Get("csrf") == "" {
		form.Set("csrf", f.server.csrf)
	}
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Host = "127.0.0.1:8735"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(f.session)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// idOf acha o ID de um tomador ou modelo do seed pela busca.
func (f fixture) customerID(t *testing.T, query string) int64 {
	t.Helper()
	list, _ := f.repo.Customers(context.Background(), query)
	return list[0].ID
}

// templateID acha o ID de um modelo do seed pela busca.
func (f fixture) templateID(t *testing.T, query string) int64 {
	t.Helper()
	list, _ := f.repo.Templates(context.Background(), query)
	return list[0].ID
}

// itoa converte o ID para texto de formulário.
func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// TestEveryPageRenders: cada tela abre com 200 e mostra o texto que a identifica.
func TestEveryPageRenders(t *testing.T) {
	f := newFixture(t, nil, nil)
	ipe := itoa(f.customerID(t, "ipe"))
	template := itoa(f.templateID(t, "ipe"))
	invoices, _ := f.repo.Invoices(context.Background())
	pages := map[string]string{
		"/invoices":                                      "Ticket médio",
		"/invoices?period=90&status=rejected":            "rejeitada",
		"/invoices?period=90&id=" + itoa(invoices[0].ID): "Retorno da SEFIN",
		"/invoices?status=cancelled":                     "Nenhuma nota neste filtro",
		"/emit":                                          "1 · Tomador",
		"/emit?q=zzz":                                    "Nenhum tomador encontrado",
		"/emit?customer=" + ipe:                          "Ipê mensal",
		"/schedules":                                     "Nenhum agendamento ainda",
		"/schedules/new":                                 "Próximas emissões",
		"/customers":                                     "IPÊ INFORMÁTICA LTDA",
		"/customers?q=zzz":                               "Nenhum tomador encontrado",
		"/customers/new":                                 "Novo tomador",
		"/customers/" + ipe:                              "Editar tomador",
		"/templates":                                     "Modelos de serviço",
		"/templates?q=zzz":                               "Nenhum modelo encontrado",
		"/templates/new":                                 "Novo modelo",
		"/templates/" + template:                         "Editar modelo",
		"/settings":                                      "Razão social",
		"/settings/certificate":                          "Certificado digital A1",
		"/settings/security":                             "Passkeys",
		"/integrations":                                  "claude mcp add nfse",
		"/static/app.css":                                "",
	}
	for target, want := range pages {
		response := f.get(target)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) {
			t.Errorf("GET %s = %d; faltou %q", target, response.Code, want)
		}
	}
	if response := f.get("/"); response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/invoices" {
		t.Errorf("raiz: %d %s", response.Code, response.Header().Get("Location"))
	}
}

// TestNotFound: ID inexistente vira 404, não 500.
func TestNotFound(t *testing.T) {
	f := newFixture(t, nil, nil)
	for _, target := range []string{"/invoices?id=9999", "/emit?customer=9999", "/customers/9999", "/templates/9999", "/nada"} {
		if response := f.get(target); response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d", target, response.Code)
		}
	}
}

// TestSecurity: Host errado e POST sem token são recusados, e os cabeçalhos
// de proteção saem em toda resposta.
func TestSecurity(t *testing.T) {
	f := newFixture(t, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	request.Host = "atacante.test:8735"
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("host errado: %d", recorder.Code)
	}
	if recorder.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "script-src 'self';") {
		t.Error("cabeçalhos de segurança ausentes")
	}
	if response := f.get("/invoices"); response.Code != http.StatusOK {
		t.Errorf("localhost recusado: %d", response.Code)
	}
	for _, token := range []string{"errado", ""} {
		form := url.Values{"csrf": {token}}
		if token == "" {
			form = url.Values{"csrf": {" "}}
		}
		if response := f.post("/emit", form); response.Code != http.StatusForbidden {
			t.Errorf("POST com token %q: %d", token, response.Code)
		}
	}
}

// TestHideValues: o botão grava o cookie, os valores somem e voltam.
func TestHideValues(t *testing.T) {
	f := newFixture(t, nil, nil)
	response := f.post("/preferences/hide-values", url.Values{"back": {"/invoices?period=90"}})
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/invoices?period=90" {
		t.Fatalf("toggle: %d %s", response.Code, response.Header().Get("Location"))
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Value != "1" || !cookie.HttpOnly {
		t.Fatalf("cookie: %+v", cookie)
	}
	if body := f.get("/invoices?period=90", cookie).Body.String(); !strings.Contains(body, "R$ ••••••") || strings.Contains(body, "R$ 19.000,00") {
		t.Error("valores não foram ocultados")
	}
	back := f.post("/preferences/hide-values", url.Values{"back": {"//atacante.test"}}, cookie)
	if back.Header().Get("Location") != "/invoices" || back.Result().Cookies()[0].Value != "0" {
		t.Error("redirecionamento aberto ou toggle não desligou")
	}
}

// TestSafeBack aceita só caminho local.
func TestSafeBack(t *testing.T) {
	cases := map[string]string{"/emit": "/emit", "//x.test": "/invoices", "/\\x.test": "/invoices", "https://x.test": "/invoices", "": "/invoices"}
	for input, want := range cases {
		if got := safeBack(input); got != want {
			t.Errorf("safeBack(%q) = %q", input, got)
		}
	}
}

// TestEmitFlow: usar o modelo preenche valor e descrição; emitir cria a nota
// rejeitada com E0039 e redireciona para o detalhe.
func TestEmitFlow(t *testing.T) {
	f := newFixture(t, nil, nil)
	customer, template := itoa(f.customerID(t, "ipe")), itoa(f.templateID(t, "ipe"))

	load := f.post("/emit", url.Values{"action": {"load-template"}, "customer_id": {customer}, "template_id": {template}, "competence": {"2026-11-28"}})
	if body := load.Body.String(); load.Code != http.StatusOK || !strings.Contains(body, "19.000,00") || !strings.Contains(body, "novembro/2026") {
		t.Fatalf("usar modelo: %d", load.Code)
	}

	emitted := f.post("/emit", url.Values{"action": {"emit"}, "customer_id": {customer}, "template_id": {template}, "amount": {"19.000,00"}, "competence": {"2026-10-29"}, "description": {"teste"}})
	if emitted.Code != http.StatusSeeOther || !strings.Contains(emitted.Header().Get("Location"), "/invoices?period=90&id=") {
		t.Fatalf("emitir: %d %s", emitted.Code, emitted.Header().Get("Location"))
	}
	detail := f.get(emitted.Header().Get("Location"))
	if !strings.Contains(detail.Body.String(), "1001") || !strings.Contains(detail.Body.String(), "simulada") {
		t.Error("detalhe da nota emitida sem DPS 1001 ou sem marca de simulada")
	}
}

// TestEmitValidation: valor inválido volta 422 com a mensagem, sem criar nota.
func TestEmitValidation(t *testing.T) {
	f := newFixture(t, nil, nil)
	customer := itoa(f.customerID(t, "ipe"))
	response := f.post("/emit", url.Values{"action": {"emit"}, "customer_id": {customer}, "amount": {"abc"}, "competence": {"x"}})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "informe um valor maior que zero") {
		t.Fatalf("validação: %d", response.Code)
	}
	if invoices, _ := f.repo.Invoices(context.Background()); len(invoices) != 5 {
		t.Error("validação criou nota")
	}
}

// TestEmitProductionNeedsConfirmation: com o emitente em produção, a tela
// mostra a caixa de confirmação; sem ela marcada volta 422 e nenhuma nota nem
// número de DPS é gasto; marcada, emite.
func TestEmitProductionNeedsConfirmation(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	issuer, _ := f.repo.Issuer(ctx)
	issuer.Environment = domain.EnvProduction
	_ = f.repo.SaveIssuer(ctx, issuer)
	customer, template := itoa(f.customerID(t, "ipe")), itoa(f.templateID(t, "ipe"))
	if form := f.get("/emit?customer=" + customer); !strings.Contains(form.Body.String(), `name="confirm_production"`) {
		t.Fatal("tela sem caixa de confirmação de produção")
	}
	values := url.Values{"action": {"emit"}, "customer_id": {customer}, "template_id": {template}, "amount": {"1,00"}, "competence": {"2026-10-29"}, "description": {"teste"}}
	refused := f.post("/emit", values)
	if refused.Code != http.StatusUnprocessableEntity || !strings.Contains(refused.Body.String(), "marque a confirmação") {
		t.Fatalf("sem confirmação: %d", refused.Code)
	}
	if invoices, _ := f.repo.Invoices(ctx); len(invoices) != 5 {
		t.Fatal("recusa criou nota")
	}
	if after, _ := f.repo.Issuer(ctx); after.NextDPSNumber != issuer.NextDPSNumber {
		t.Fatal("recusa gastou número de DPS")
	}
	values.Set("confirm_production", "yes")
	if confirmed := f.post("/emit", values); confirmed.Code != http.StatusSeeOther {
		t.Fatalf("confirmada: %d", confirmed.Code)
	}
}

// timeoutEmitter simula queda de comunicação com a SEFIN.
type timeoutEmitter struct{}

// Emit sempre falha com timeout.
func (timeoutEmitter) Emit(context.Context, ports.EmitRequest) (ports.EmitResult, error) {
	return ports.EmitResult{}, errors.New("timeout")
}

// TestEmitTimeoutShowsPendingDetail: sem resposta, a nota pendente aparece com o motivo.
func TestEmitTimeoutShowsPendingDetail(t *testing.T) {
	f := newFixture(t, timeoutEmitter{}, nil)
	customer, template := itoa(f.customerID(t, "ipe")), itoa(f.templateID(t, "ipe"))
	response := f.post("/emit", url.Values{"action": {"emit"}, "customer_id": {customer}, "template_id": {template}, "amount": {"10,00"}, "competence": {"2026-10-29"}, "description": {"x"}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("timeout: %d", response.Code)
	}
	if body := f.get(response.Header().Get("Location")).Body.String(); !strings.Contains(body, "Sem resposta da SEFIN") {
		t.Error("detalhe sem o aviso de pendência")
	}
}

// TestScheduleFlow: pré-visualizar recalcula as datas; salvar grava e lista.
func TestScheduleFlow(t *testing.T) {
	f := newFixture(t, nil, nil)
	form := url.Values{
		"template_id": {itoa(f.templateID(t, "ipe"))}, "customer_id": {itoa(f.customerID(t, "ipe"))},
		"amount": {"19.000,00"}, "day": {"31"}, "competence": {"emission"}, "description": {"ref {mes}/{ano}"},
		"notify_eve": {"on"}, "send_to_customer": {"on"},
	}
	form.Set("action", "preview")
	preview := f.post("/schedules", form)
	if body := preview.Body.String(); preview.Code != http.StatusOK || !strings.Contains(body, "31/10/2026, 03:00") || !strings.Contains(body, "30/11/2026, 03:00") || !strings.Contains(body, "ref outubro/2026") {
		t.Fatalf("pré-visualizar: %d", preview.Code)
	}
	form.Set("action", "save")
	form.Set("csrf", "")
	saved := f.post("/schedules", form)
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("salvar: %d", saved.Code)
	}
	if body := f.get("/schedules").Body.String(); !strings.Contains(body, "todo dia 31") || !strings.Contains(body, "data da emissão") {
		t.Error("agendamento não aparece na lista")
	}

	invalid := url.Values{"action": {"save"}, "amount": {"abc"}, "day": {"40"}}
	if response := f.post("/schedules", invalid); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "valor inválido") {
		t.Errorf("agendamento inválido: %d", response.Code)
	}
}

// TestCustomerFlow: criar, editar, recusar inválido e atualizar inexistente.
func TestCustomerFlow(t *testing.T) {
	f := newFixture(t, nil, nil)
	created := f.post("/customers", url.Values{"name": {"ACME"}, "document": {"529.982.247-25"}, "email": {"a@acme.test"}})
	if created.Code != http.StatusSeeOther {
		t.Fatalf("criar: %d", created.Code)
	}
	found, _ := f.repo.Customers(context.Background(), "acme")
	if len(found) != 1 || found[0].Document != "52998224725" {
		t.Fatalf("documento não foi normalizado: %+v", found)
	}
	foreign := f.post("/customers", url.Values{"name": {"Foreign Inc"}, "foreign": {"on"}, "country": {"US"}})
	if foreign.Code != http.StatusSeeOther {
		t.Errorf("tomador no exterior: %d", foreign.Code)
	}
	if response := f.post("/customers", url.Values{"document": {"123"}}); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "CPF/CNPJ inválido") {
		t.Errorf("inválido: %d", response.Code)
	}
	if response := f.post("/customers", url.Values{"id": {"9999"}, "name": {"X"}, "document": {"52998224725"}}); response.Code != http.StatusNotFound {
		t.Errorf("inexistente: %d", response.Code)
	}
	// O mesmo CPF de novo, num tomador novo: volta ao formulário, sem duplicar.
	if response := f.post("/customers", url.Values{"name": {"Outro nome"}, "document": {"529.982.247-25"}}); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "já existe um tomador com este CPF/CNPJ") {
		t.Errorf("CPF repetido: %d", response.Code)
	}
	if again, _ := f.repo.Customers(context.Background(), "52998224725"); len(again) != 1 {
		t.Errorf("CPF repetido gravou outro tomador: %d", len(again))
	}
}

// TestTemplateFlow: criar com código mascarado, recusar código e valor errados.
func TestTemplateFlow(t *testing.T) {
	f := newFixture(t, nil, nil)
	valid := url.Values{"name": {"Novo"}, "description": {"x"}, "national_tax_code": {"01.07.01"}, "nbs": {"1.1501.30.00"}, "service_city_code": {"3505708"}, "default_amount": {"1.000,00"}, "customer_id": {"0"}, "export": {"on"}}
	if response := f.post("/templates", valid); response.Code != http.StatusSeeOther {
		t.Fatalf("criar: %d %s", response.Code, response.Body.String())
	}
	found, _ := f.repo.Templates(context.Background(), "novo")
	if len(found) != 1 || found[0].NationalTaxCode != "010701" || found[0].NBS != "115013000" || found[0].DefaultAmount != 100000 {
		t.Fatalf("modelo gravado errado: %+v", found)
	}
	invalid := url.Values{"name": {"X"}, "description": {"x"}, "national_tax_code": {"17"}, "service_city_code": {"3505708"}, "default_amount": {"abc"}}
	if response := f.post("/templates", invalid); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "valor inválido") || !strings.Contains(response.Body.String(), "6 dígitos") {
		t.Errorf("inválido: %d", response.Code)
	}
	missing := url.Values{"id": {"9999"}, "name": {"X"}, "description": {"x"}, "national_tax_code": {"010701"}, "service_city_code": {"3505708"}}
	if response := f.post("/templates", missing); response.Code != http.StatusNotFound {
		t.Errorf("inexistente: %d", response.Code)
	}
}

// settingsForm devolve o formulário do emitente real, pronto para alterar.
func settingsForm() url.Values {
	return url.Values{
		"legal_name": {"EXEMPLO TECNOLOGIA LTDA"}, "cnpj": {"11.222.333/0001-81"}, "municipal_registration": {"000000"},
		"city_code": {"3505708"}, "city_name": {"Barueri/SP"}, "simples_option": {"me-epp"}, "simples_regime": {"federal-municipal-sn"},
		"special_regime": {"none"}, "environment": {"homologation"}, "series": {"1"}, "next_dps_number": {"1100"},
		"simples_tax_rate": {"6,00"},
	}
}

// TestSettingsFlow: salvar muda a numeração; dado inválido é recusado; o campo
// Ambiente existe, mas NUNCA liga produção sozinho — nem
// com o CNPJ forjado no POST: pedir produção grava o resto e manda para a
// confirmação do CNPJ (story 4.1). Voltar para homologação vale na hora, e
// salvar sem o campo não tira de produção quem já está nela.
func TestSettingsFlow(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	if response := f.post("/settings", settingsForm()); response.Code != http.StatusSeeOther {
		t.Fatalf("salvar: %d %s", response.Code, response.Body.String())
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.NextDPSNumber != 1100 || issuer.CNPJ != "11222333000181" {
		t.Fatalf("emitente: %+v", issuer)
	}
	body := f.get("/settings").Body.String()
	if strings.Contains(body, `name="confirm_cnpj"`) || !strings.Contains(body, `name="environment"`) {
		t.Fatal("o Emitente precisa do campo Ambiente e não pode ter a confirmação de CNPJ")
	}
	forged := settingsForm()
	forged.Set("environment", "production")
	forged.Set("confirm_cnpj", "11.222.333/0001-81")
	forged.Set("next_dps_number", "1200")
	response := f.post("/settings", forged)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/environment/production" {
		t.Fatalf("pedir produção tem de ir para a confirmação: %d %q", response.Code, response.Header().Get("Location"))
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvHomologation || issuer.NextDPSNumber != 1200 {
		t.Fatalf("o formulário passou para produção ou não gravou o resto: %+v", issuer)
	}
	invalid := settingsForm()
	invalid.Set("cnpj", "123")
	if response := f.post("/settings", invalid); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("inválido: %d", response.Code)
	}
	issuer, _ := f.repo.Issuer(ctx)
	issuer.Environment = domain.EnvProduction
	_ = f.repo.SaveIssuer(ctx, issuer)
	absent := settingsForm()
	absent.Del("environment")
	f.post("/settings", absent)
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvProduction {
		t.Fatal("salvar sem o campo Ambiente tirou de produção")
	}
	stay := settingsForm()
	stay.Set("environment", "production")
	if response := f.post("/settings", stay); response.Header().Get("Location") == "/environment/production" {
		t.Fatal("já em produção, salvar não pode pedir a confirmação de novo")
	}
	f.post("/settings", settingsForm())
	if issuer, _ := f.repo.Issuer(ctx); issuer.Environment != domain.EnvHomologation {
		t.Fatal("escolher homologação no formulário não voltou para homologação")
	}
}

// TestSettingsSimplesTaxRate: a alíquota do Simples (pTotTribSN, exigida para
// ME/EPP desde o E0712 de 02/11/2026) é gravada do jeito que se digita, volta
// no campo com vírgula e, ilegível, é recusada no campo — nunca vira zero
// calado, que só estouraria na próxima emissão.
func TestSettingsSimplesTaxRate(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	form := settingsForm()
	form.Set("simples_tax_rate", "15,5%")
	if response := f.post("/settings", form); response.Code != http.StatusSeeOther {
		t.Fatalf("salvar: %d", response.Code)
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.SimplesTaxRate != 1550 {
		t.Fatalf("alíquota gravada: %d", issuer.SimplesTaxRate)
	}
	if !strings.Contains(f.get("/settings").Body.String(), `name="simples_tax_rate" value="15,50"`) {
		t.Error("o campo não mostra a alíquota gravada")
	}
	form.Set("simples_tax_rate", "seis")
	response := f.post("/settings", form)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "percentual inválido") {
		t.Fatalf("alíquota ilegível: %d", response.Code)
	}
	if issuer, _ := f.repo.Issuer(ctx); issuer.SimplesTaxRate != 1550 {
		t.Fatal("a alíquota ilegível sobrescreveu a boa")
	}
}

// TestSettingsSpecialRegimeOptions: a tela oferece os oito códigos do XSD
// (TSRegEspTrib) com um valor por código — a versão antiga tinha "Ato
// cooperado" e "Cooperativa" mandando o mesmo 1 e não tinha 3, 4, 5 e 9; o
// valor antigo "cooperative" ainda aparece selecionado como ato cooperado; e
// regime fora da tabela é recusado no campo, não no meio da emissão.
func TestSettingsSpecialRegimeOptions(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	form := settingsForm()
	form.Set("special_regime", "self-employed")
	if response := f.post("/settings", form); response.Code != http.StatusSeeOther {
		t.Fatalf("salvar: %d", response.Code)
	}
	body := f.get("/settings").Body.String()
	for _, value := range []string{"none", "cooperative-act", "estimate", "municipal-microenterprise", "notary", "self-employed", "professional-society", "other"} {
		if !strings.Contains(body, `value="`+value+`"`) {
			t.Errorf("falta a opção %q", value)
		}
	}
	if strings.Contains(body, `value="cooperative"`) {
		t.Error("a opção duplicada Cooperativa voltou")
	}
	if !strings.Contains(body, `value="self-employed" selected`) {
		t.Error("o regime gravado não aparece selecionado")
	}
	issuer, _ := f.repo.Issuer(ctx)
	issuer.SpecialRegime = "cooperative"
	_ = f.repo.SaveIssuer(ctx, issuer)
	if !strings.Contains(f.get("/settings").Body.String(), `value="cooperative-act" selected`) {
		t.Error("o valor antigo cooperative não aparece como ato cooperado")
	}
	form.Set("special_regime", "x")
	if response := f.post("/settings", form); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "regime especial desconhecido") {
		t.Errorf("regime desconhecido: %d", response.Code)
	}
}

// failingRepo devolve erro em toda leitura de lista e em toda gravação, para
// cobrir as respostas 500 do painel.
type failingRepo struct{ *memory.Repository }

// diskErr é o erro de I/O simulado.
var diskErr = errors.New("disco")

// Invoices falha.
func (failingRepo) Invoices(context.Context) ([]domain.Invoice, error) { return nil, diskErr }

// Customers falha.
func (failingRepo) Customers(context.Context, string) ([]domain.Customer, error) { return nil, diskErr }

// Templates falha.
func (failingRepo) Templates(context.Context, string) ([]domain.Template, error) { return nil, diskErr }

// Schedules falha.
func (failingRepo) Schedules(context.Context) ([]domain.Schedule, error) { return nil, diskErr }

// Issuer falha.
func (failingRepo) Issuer(context.Context) (domain.Issuer, error) { return domain.Issuer{}, diskErr }

// SaveSchedule falha.
func (failingRepo) SaveSchedule(context.Context, *domain.Schedule) error { return diskErr }

// SaveIssuer falha.
func (failingRepo) SaveIssuer(context.Context, domain.Issuer) error { return diskErr }

// ReserveDPSNumber falha.
func (failingRepo) ReserveDPSNumber(context.Context) (string, int64, error) { return "", 0, diskErr }

// TestRepositoryFailuresAre500: falha de I/O vira 500 limpo, sem pânico.
func TestRepositoryFailuresAre500(t *testing.T) {
	seeded := memory.NewSeeded(brt)
	f := newFixture(t, nil, failingRepo{seeded})
	f.repo = seeded
	for _, target := range []string{"/invoices", "/emit", "/schedules", "/customers", "/templates", "/settings"} {
		if response := f.get(target); response.Code != http.StatusInternalServerError {
			t.Errorf("GET %s = %d", target, response.Code)
		}
	}
	customer, template := itoa(f.customerID(t, "ipe")), itoa(f.templateID(t, "ipe"))
	emit := f.post("/emit", url.Values{"action": {"emit"}, "customer_id": {customer}, "template_id": {template}, "amount": {"1,00"}, "competence": {"2026-10-29"}, "description": {"x"}})
	if emit.Code != http.StatusInternalServerError {
		t.Errorf("emitir: %d", emit.Code)
	}
	schedule := url.Values{"action": {"save"}, "template_id": {template}, "customer_id": {customer}, "amount": {"1,00"}, "day": {"5"}, "competence": {"emission"}}
	if response := f.post("/schedules", schedule); response.Code != http.StatusInternalServerError {
		t.Errorf("agendar: %d", response.Code)
	}
	if response := f.post("/settings", settingsForm()); response.Code != http.StatusInternalServerError {
		t.Errorf("configurações: %d", response.Code)
	}
}

// TestHelpers cobre os conversores de formulário e a junção de erros.
func TestHelpers(t *testing.T) {
	if formID("-3") != 0 || formID("abc") != 0 || formID(" 42 ") != 42 {
		t.Error("formID")
	}
	if digitsOnly(" 01.07-01 ") != "010701" || digitsOnly("01a") != "01a" {
		t.Error("digitsOnly")
	}
	joined := collectFieldErrors(errors.Join(domain.FieldErrors{"a": "1"}, errors.New("x"), domain.FieldErrors{"b": "2"}))
	if joined["a"] != "1" || joined["b"] != "2" {
		t.Errorf("collectFieldErrors: %v", joined)
	}
	if funcs["brDate"].(func(string) string)("2026-10-29") != "29/10/2026" || funcs["brDate"].(func(string) string)("x") != "x" {
		t.Error("brDate")
	}
}
