package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nfse/internal/adapters/memory"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata.
var brt = time.FixedZone("BRT", -3*3600)

// stubEmitter autoriza, rejeita ou falha conforme o programado e conta chamadas.
type stubEmitter struct {
	reject bool
	err    error
	calls  int
}

// Emit cumpre ports.Emitter para o teste.
func (e *stubEmitter) Emit(context.Context, ports.EmitRequest) (ports.EmitResult, error) {
	e.calls++
	if e.err != nil {
		return ports.EmitResult{}, e.err
	}
	if e.reject {
		return ports.EmitResult{Status: domain.StatusRejected, ErrorCode: "E0116", ErrorMessage: "[E0116] A IM deve ser informada"}, nil
	}
	return ports.EmitResult{Status: domain.StatusAuthorized, AccessKey: "35057082112223330001810000000000003826100000000001", NFSeNumber: "7"}, nil
}

// stubCanceller aceita ou recusa o cancelamento.
type stubCanceller struct{ refuse bool }

// Cancel cumpre ports.Canceller para o teste.
func (c stubCanceller) Cancel(context.Context, domain.CancelEvent) (ports.CancelResult, error) {
	if c.refuse {
		return ports.CancelResult{ErrorCode: "E1831", ErrorMessage: "não existe"}, nil
	}
	return ports.CancelResult{Registered: true}, nil
}

// referenceXML lê a NFS-e fictícia de testdata/.
func referenceXML(t *testing.T) []byte {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// session sobe o servidor e um cliente MCP ligados em memória.
func session(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := New(deps).Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "teste", Version: "1"}, nil)
	cs, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call chama a ferramenta e decodifica a saída estruturada em `out`.
func call(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) (isError bool, text string) {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: erro de protocolo: %v", name, err)
	}
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if out != nil && result.StructuredContent != nil {
		raw, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(raw, out)
	}
	return result.IsError, text
}

// fixture monta o núcleo com os dados fictícios dos testes, em memória.
func fixture(t *testing.T, emitter *stubEmitter) (Deps, *memory.Repository) {
	repo := memory.NewSeeded(brt)
	now := func() time.Time { return time.Date(2026, 10, 30, 10, 0, 0, 0, brt) }
	service := &app.Service{Repo: repo, Emitter: emitter, Now: now, Location: brt}
	return Deps{
		Service: service, Canceller: stubCanceller{}, AppVersion: "teste", OutputDir: t.TempDir(),
		FetchXML: func(context.Context, domain.Invoice) ([]byte, error) { return referenceXML(t), nil },
		Certificate: func(context.Context) (domain.CertificateInfo, error) {
			return domain.CertificateInfo{NotAfter: now().AddDate(1, 0, 0)}, nil
		},
	}, repo
}

// TestListingTools: status da conta, tomadores, modelos, notas, agendamentos.
func TestListingTools(t *testing.T) {
	deps, repo := fixture(t, &stubEmitter{})
	schedule := domain.Schedule{TemplateID: 1, CustomerID: 1, Amount: 100, Day: 28, Competence: domain.CompetenceEmission, Active: true}
	_ = repo.SaveSchedule(context.Background(), &schedule)
	cs := session(t, deps)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("ferramentas: %v", err)
	}
	var status AccountStatus
	if isErr, _ := call(t, cs, "account_status", map[string]any{}, &status); isErr || status.CNPJ != "11.222.333/0001-81" || status.Environment != "Homologação" || status.CertificateTil != "30/10/2027" {
		t.Fatalf("status: %+v", status)
	}
	var customers CustomerList
	call(t, cs, "list_customers", map[string]any{"query": "aurora"}, &customers)
	if len(customers.Customers) != 1 {
		t.Fatalf("tomadores: %+v", customers)
	}
	var templates TemplateList
	call(t, cs, "list_templates", map[string]any{}, &templates)
	var invoices InvoiceList
	call(t, cs, "list_invoices", map[string]any{"limit": 2}, &invoices)
	var schedules ScheduleList
	call(t, cs, "list_schedules", map[string]any{}, &schedules)
	if len(templates.Templates) == 0 || len(invoices.Invoices) != 2 || len(schedules.Schedules) == 0 || len(schedules.Schedules[0].NextRuns) != 4 {
		t.Fatalf("listas: %d modelos, %d notas, %+v", len(templates.Templates), len(invoices.Invoices), schedules)
	}
	var invoice InvoiceOut
	if isErr, _ := call(t, cs, "show_invoice", map[string]any{"id": invoices.Invoices[0].ID}, &invoice); isErr || invoice.ID != invoices.Invoices[0].ID {
		t.Fatalf("show: %+v", invoice)
	}
	if isErr, text := call(t, cs, "show_invoice", map[string]any{"id": 999}, nil); !isErr || !strings.Contains(text, "#999") {
		t.Fatalf("nota inexistente: %v %s", isErr, text)
	}
	// Cada chamada grava o contato com o nome do cliente MCP ("teste").
	contacts, _ := repo.AgentContacts(context.Background())
	if len(contacts) != 1 || contacts[0].Client != "teste" || contacts[0].Tool != "show_invoice" {
		t.Fatalf("contato do agente: %+v", contacts)
	}
}

// TestCatalogMatchesServer: o que o servidor registra é exatamente o catálogo
// que a tela de Integrações mostra, com as mesmas descrições; a skill embutida
// cita toda ferramenta; nome fora do catálogo para na hora.
func TestCatalogMatchesServer(t *testing.T) {
	deps, _ := fixture(t, &stubEmitter{})
	listed, err := session(t, deps).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]string{}
	for _, tool := range listed.Tools {
		registered[tool.Name] = tool.Description
	}
	if len(registered) != len(Tools) {
		t.Fatalf("servidor tem %d ferramentas, catálogo tem %d", len(registered), len(Tools))
	}
	for _, info := range Tools {
		if registered[info.Name] != info.Description {
			t.Errorf("%s: descrição diverge ou ferramenta ausente", info.Name)
		}
		if !strings.Contains(Skill, "`"+info.Name+"`") {
			t.Errorf("a skill não cita %s", info.Name)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("ferramenta fora do catálogo deveria parar")
		}
	}()
	tool("nao_existe")
}

// TestEmitTool: chave obrigatória, emissão, repetição pela mesma chave sem
// segunda nota, rejeição legível, pendente com orientação e guarda de produção.
func TestEmitTool(t *testing.T) {
	emitter := &stubEmitter{}
	deps, repo := fixture(t, emitter)
	cs := session(t, deps)
	templates, _ := repo.Templates(context.Background(), "Aurora")
	args := map[string]any{"customer_id": templates[0].CustomerID, "template_id": templates[0].ID}

	if isErr, text := call(t, cs, "emit_invoice", args, nil); !isErr || !strings.Contains(text, "idempotency_key") {
		t.Fatalf("sem chave: %s", text)
	}
	args["idempotency_key"] = "pedido-1"
	var first, again InvoiceOut
	if isErr, text := call(t, cs, "emit_invoice", args, &first); isErr || first.Status != "autorizada" || first.Amount != "R$ 17.000,00" {
		t.Fatalf("emissão: %+v %s", first, text)
	}
	call(t, cs, "emit_invoice", args, &again)
	if again.ID != first.ID || emitter.calls != 1 {
		t.Fatalf("repetição gerou outra nota: %d vs %d, %d envios", again.ID, first.ID, emitter.calls)
	}
	stored, _ := repo.Invoice(context.Background(), first.ID)
	if !strings.Contains(stored.Description, "outubro/2026") {
		t.Fatalf("descrição não resolvida: %q", stored.Description)
	}

	emitter.reject = true
	args["idempotency_key"] = "pedido-2"
	var rejected InvoiceOut
	if isErr, _ := call(t, cs, "emit_invoice", args, &rejected); isErr || rejected.ErrorCode != "E0116" || !strings.Contains(rejected.Message, "inscrição municipal") {
		t.Fatalf("rejeição deveria ser resultado legível: %+v", rejected)
	}

	emitter.reject, emitter.err = false, errors.New("timeout")
	args["idempotency_key"] = "pedido-3"
	var pending InvoiceOut
	if isErr, _ := call(t, cs, "emit_invoice", args, &pending); isErr || !strings.Contains(pending.Message, "show_invoice") {
		t.Fatalf("pendente: %+v", pending)
	}

	args["template_id"], args["idempotency_key"] = 999, "pedido-4"
	if isErr, _ := call(t, cs, "emit_invoice", args, nil); !isErr {
		t.Fatal("modelo inexistente deveria ser erro")
	}

	issuer, _ := repo.Issuer(context.Background())
	issuer.Environment = domain.EnvProduction
	_ = repo.SaveIssuer(context.Background(), issuer)
	emitter.err = nil
	args["template_id"], args["idempotency_key"] = templates[0].ID, "pedido-5"
	if isErr, text := call(t, cs, "emit_invoice", args, nil); !isErr || !strings.Contains(text, "produção") {
		t.Fatalf("produção sem confirm_production: %v %s", isErr, text)
	}
	args["confirm_production"] = true
	if isErr, _ := call(t, cs, "emit_invoice", args, nil); isErr {
		t.Fatal("produção confirmada deveria emitir")
	}
}

// TestCancelAndDANFSeTools: cancelamento aceito e recusado, DANFSe gerado
// buscando o XML e recusado para nota sem chave.
func TestCancelAndDANFSeTools(t *testing.T) {
	deps, repo := fixture(t, &stubEmitter{})
	ctx := context.Background()
	authorized := domain.Invoice{Environment: domain.EnvHomologation, Status: domain.StatusAuthorized, AccessKey: "35057082112223330001810000000000003826100000000001"}
	draft := domain.Invoice{Environment: domain.EnvHomologation, Status: domain.StatusRejected}
	_ = repo.SaveInvoice(ctx, &authorized)
	_ = repo.SaveInvoice(ctx, &draft)
	cs := session(t, deps)

	var pdf DANFSeOut
	if isErr, text := call(t, cs, "download_danfse", map[string]any{"id": authorized.ID}, &pdf); isErr || pdf.Bytes == 0 {
		t.Fatalf("DANFSe: %s", text)
	}
	if data, err := os.ReadFile(pdf.Path); err != nil || !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("arquivo: %v", err)
	}
	if stored, _ := repo.Invoice(ctx, authorized.ID); len(stored.XML) == 0 {
		t.Fatal("XML buscado não foi guardado")
	}
	for _, id := range []int64{draft.ID, 999} {
		if isErr, _ := call(t, cs, "download_danfse", map[string]any{"id": id}, nil); !isErr {
			t.Errorf("DANFSe da nota %d deveria falhar", id)
		}
	}

	refused := deps
	refused.Canceller = stubCanceller{refuse: true}
	var out InvoiceOut
	if _, _ = call(t, session(t, refused), "cancel_invoice", map[string]any{"id": authorized.ID, "reason": "outros", "description": "justificativa comprida"}, &out); !strings.Contains(out.Message, "recusou") {
		t.Fatalf("recusa: %+v", out)
	}
	var cancelled InvoiceOut
	if isErr, _ := call(t, cs, "cancel_invoice", map[string]any{"id": authorized.ID, "reason": "outros", "description": "justificativa comprida"}, &cancelled); isErr || cancelled.Status != "cancelada" || cancelled.ErrorCode != "" {
		t.Fatalf("cancelamento: %+v", cancelled)
	}
	if isErr, _ := call(t, cs, "cancel_invoice", map[string]any{"id": draft.ID, "reason": "outros", "description": "justificativa comprida"}, nil); !isErr {
		t.Fatal("cancelar rejeitada deveria falhar")
	}
}

// TestAccountStatusGaps: sem emitente, emitente incompleto e certificado que
// não abre aparecem em "missing".
func TestAccountStatusGaps(t *testing.T) {
	empty := Deps{Service: &app.Service{Repo: memory.New(domain.Issuer{}), Location: brt}, AppVersion: "t"}
	var status AccountStatus
	call(t, session(t, empty), "account_status", map[string]any{}, &status)
	if len(status.Missing) != 1 || !strings.Contains(status.Missing[0], "emitente não configurado") {
		t.Fatalf("sem emitente: %+v", status)
	}
	incomplete := memory.DemoIssuer()
	incomplete.LegalName = ""
	broken := Deps{Service: &app.Service{Repo: memory.New(incomplete), Location: brt}, AppVersion: "t",
		Certificate: func(context.Context) (domain.CertificateInfo, error) {
			return domain.CertificateInfo{}, errors.New("pass travado")
		}}
	call(t, session(t, broken), "account_status", map[string]any{}, &status)
	if len(status.Missing) != 2 {
		t.Fatalf("pendências: %+v", status.Missing)
	}
}
