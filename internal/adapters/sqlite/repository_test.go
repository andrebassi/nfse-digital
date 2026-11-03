package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// openTemp abre um banco novo num diretório temporário do teste.
func openTemp(t *testing.T) (*Repository, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dados", "nfse.db")
	repo, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo, path
}

// TestOpenCreatesPrivateFileAndMigratesOnce: o arquivo nasce 0600, a pasta
// 0700, e abrir de novo não reaplica a migração.
func TestOpenCreatesPrivateFileAndMigratesOnce(t *testing.T) {
	repo, path := openTemp(t)
	info, _ := os.Stat(path)
	dir, _ := os.Stat(filepath.Dir(path))
	if info.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
		t.Fatalf("permissões: arquivo %v, pasta %v", info.Mode().Perm(), dir.Mode().Perm())
	}
	_ = repo.SaveIssuer(context.Background(), memory.DemoIssuer())
	repo.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer again.Close()
	var count int
	_ = again.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count)
	// Uma linha por arquivo embutido: nem a mais (repetiu), nem a menos (pulou).
	files, _ := fs.Glob(migrationsFS, "migrations/*.sql")
	if issuer, err := again.Issuer(context.Background()); err != nil || issuer.CNPJ != "11222333000181" || count != len(files) || count < 4 {
		t.Fatalf("dados não sobreviveram ou migração repetiu: %+v, %v, %d", issuer, err, count)
	}
}

// TestOpenFailsOnBadPath: pasta impossível de criar vira erro.
func TestOpenFailsOnBadPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "arquivo")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	if _, err := Open(filepath.Join(file, "sub", "nfse.db")); err == nil {
		t.Fatal("deveria falhar com um arquivo no meio do caminho")
	}
}

// TestDefaultPath usa NFSE_DB quando definido e ~/.local/share no resto.
func TestDefaultPath(t *testing.T) {
	t.Setenv("NFSE_DB", "/tmp/x.db")
	if path, _ := DefaultPath(); path != "/tmp/x.db" {
		t.Fatalf("NFSE_DB ignorado: %s", path)
	}
	t.Setenv("NFSE_DB", "")
	t.Setenv("HOME", "/home/dona")
	if path, _ := DefaultPath(); path != "/home/dona/.local/share/nfse-digital/nfse.db" {
		t.Fatalf("padrão: %s", path)
	}
	t.Setenv("HOME", "")
	if _, err := DefaultPath(); err == nil {
		t.Fatal("sem HOME deveria falhar")
	}
}

// TestAccountAndIssuer: antes do cadastro, ErrNotFound; depois, os dados gravados.
func TestAccountAndIssuer(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	if _, err := repo.Account(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("conta: %v", err)
	}
	if _, err := repo.Issuer(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("emitente: %v", err)
	}
	if _, _, err := repo.ReserveDPSNumber(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("reserva sem emitente: %v", err)
	}
	created := time.Date(2026, 10, 29, 22, 52, 0, 0, brt)
	owner := domain.Account{Name: "Dona Exemplo", Email: "dona@exemplo.example", PasswordHash: []byte("hash2"), CreatedAt: created}
	if err := repo.CreateAccount(ctx, &owner); err != nil || owner.ID == 0 {
		t.Fatalf("criar conta: %v", err)
	}
	if err := repo.CreateAccount(ctx, &domain.Account{Name: "X", Email: "dona@exemplo.example", PasswordHash: []byte("h"), CreatedAt: created}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("e-mail repetido: %v", err)
	}
	repo = repo.For(owner.ID)
	account, err := repo.Account(ctx)
	if err != nil || account.Name != "Dona Exemplo" || string(account.PasswordHash) != "hash2" || !account.CreatedAt.Equal(created) {
		t.Fatalf("conta: %+v, %v", account, err)
	}
	if byEmail, err := repo.AccountByEmail(ctx, "dona@exemplo.example"); err != nil || byEmail.ID != owner.ID {
		t.Fatalf("por e-mail: %+v, %v", byEmail, err)
	}
	if list, err := repo.Accounts(ctx); err != nil || len(list) != 1 {
		t.Fatalf("lista: %+v, %v", list, err)
	}
	issuer := memory.DemoIssuer()
	_ = repo.SaveIssuer(ctx, issuer)
	// A alíquota do Simples (migração 013) entra na comparação do struct
	// inteiro: coluna esquecida no SELECT ou no UPDATE quebra aqui.
	issuer.LegalName, issuer.SimplesTaxRate = "OUTRA LTDA", 1552
	_ = repo.SaveIssuer(ctx, issuer)
	if got, _ := repo.Issuer(ctx); got != issuer {
		t.Fatalf("emitente: %+v", got)
	}
}

// TestReserveDPSNumberIsUniqueUnderConcurrency: 50 reservas em paralelo nunca
// repetem número, e o contador avança 50.
func TestReserveDPSNumberIsUniqueUnderConcurrency(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	_ = repo.SaveIssuer(ctx, memory.DemoIssuer())
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int64]bool{}
	for range 50 {
		wg.Go(func() {
			series, number, err := repo.ReserveDPSNumber(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || series != "1" || seen[number] {
				t.Errorf("reserva: %s/%d, %v", series, number, err)
			}
			seen[number] = true
		})
	}
	wg.Wait()
	if issuer, _ := repo.Issuer(ctx); issuer.NextDPSNumber != 1051 {
		t.Fatalf("contador = %d", issuer.NextDPSNumber)
	}
}

// TestCustomersAndTemplates: criar, atualizar, buscar sem acento e o ErrNotFound.
func TestCustomersAndTemplates(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	customer := domain.Customer{Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100", Email: "notas@ipe.example", Foreign: false}
	if err := repo.SaveCustomer(ctx, &customer); err != nil || customer.ID == 0 {
		t.Fatalf("criar tomador: %v", err)
	}
	foreign := domain.Customer{Name: "ACME Inc", Foreign: true, Country: "US"}
	_ = repo.SaveCustomer(ctx, &foreign)
	customer.CityName = "São José dos Campos/SP"
	if err := repo.SaveCustomer(ctx, &customer); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Customer(ctx, customer.ID); got != customer {
		t.Fatalf("tomador: %+v", got)
	}
	if got, _ := repo.Customer(ctx, foreign.ID); !got.Foreign || got.Country != "US" {
		t.Fatalf("exterior: %+v", got)
	}
	if list, _ := repo.Customers(ctx, "ipe"); len(list) != 1 {
		t.Fatalf("busca sem acento: %d", len(list))
	}
	if list, _ := repo.Customers(ctx, "56.789.012"); len(list) != 1 {
		t.Fatal("busca por CNPJ mascarado")
	}

	template := domain.Template{Name: "Ipê mensal", Description: "x", NationalTaxCode: "010701", NBS: "115013000", ServiceCityCode: "3505708", DefaultAmount: 1900000, Export: true, CustomerID: customer.ID, IBSCBSOperation: "050101", IBSCBSCST: "000", IBSCBSClass: "000001"}
	if err := repo.SaveTemplate(ctx, &template); err != nil {
		t.Fatal(err)
	}
	template.DefaultAmount = 2000000
	_ = repo.SaveTemplate(ctx, &template)
	if got, _ := repo.Template(ctx, template.ID); got != template {
		t.Fatalf("modelo: %+v", got)
	}
	for query, want := range map[string]int{"ipe": 1, "suporte tecnico": 1, "zzz": 0, "": 1} {
		if list, _ := repo.Templates(ctx, query); len(list) != want {
			t.Errorf("Templates(%q) = %d", query, len(list))
		}
	}

	missing := int64(9999)
	checks := []error{
		repo.SaveCustomer(ctx, &domain.Customer{ID: missing, Name: "x"}),
		repo.SaveTemplate(ctx, &domain.Template{ID: missing}),
		repo.SaveInvoice(ctx, &domain.Invoice{ID: missing}),
		repo.SaveSchedule(ctx, &domain.Schedule{ID: missing, Day: 1}),
	}
	_, e1 := repo.Customer(ctx, missing)
	_, e2 := repo.Template(ctx, missing)
	_, e3 := repo.Invoice(ctx, missing)
	for i, err := range append(checks, e1, e2, e3) {
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("verificação %d: %v", i, err)
		}
	}
}

// TestInvoicesAndSchedules: nota volta igual (inclusive datas), lista em ordem
// decrescente e DPS repetida é recusada pelo banco.
func TestInvoicesAndSchedules(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	older := domain.Invoice{Environment: domain.EnvProduction, Series: "1", NFSeNumber: "35", CustomerID: 1, Amount: 1900000,
		Competence: time.Date(2026, 9, 1, 0, 0, 0, 0, brt), IssuedAt: time.Date(2026, 9, 5, 10, 0, 0, 0, brt), Status: domain.StatusAuthorized}
	newer := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: 1001, CustomerID: 1, TemplateID: 2, Amount: 1000,
		Competence: time.Date(2026, 10, 1, 0, 0, 0, 0, brt), IssuedAt: time.Date(2026, 10, 29, 22, 52, 0, 0, brt), Status: domain.StatusPending}
	_ = repo.SaveInvoice(ctx, &older)
	_ = repo.SaveInvoice(ctx, &newer)
	newer.Status, newer.ErrorCode, newer.ErrorMessage, newer.Simulated = domain.StatusRejected, "E0039", "msg", true
	if err := repo.SaveInvoice(ctx, &newer); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Invoice(ctx, newer.ID)
	if got.Status != domain.StatusRejected || !got.Simulated || !got.IssuedAt.Equal(newer.IssuedAt) || !got.Competence.Equal(newer.Competence) {
		t.Fatalf("nota: %+v", got)
	}
	list, _ := repo.Invoices(ctx)
	if len(list) != 2 || list[0].ID != newer.ID {
		t.Fatalf("ordem: %+v", list)
	}
	duplicate := newer
	duplicate.ID = 0
	if err := repo.SaveInvoice(ctx, &duplicate); err == nil {
		t.Fatal("DPS repetida deveria ser recusada pelo índice único")
	}

	first := domain.Schedule{TemplateID: 2, CustomerID: 1, Amount: 1900000, Day: 28, Competence: domain.CompetencePreviousMonth, NotifyEve: true, Active: true}
	second := domain.Schedule{TemplateID: 2, CustomerID: 1, Amount: 1000, Day: 5, Competence: domain.CompetenceEmission, SendToCustomer: true}
	_ = repo.SaveSchedule(ctx, &first)
	_ = repo.SaveSchedule(ctx, &second)
	first.Amount = 2000000
	_ = repo.SaveSchedule(ctx, &first)
	schedules, _ := repo.Schedules(ctx)
	if len(schedules) != 2 || schedules[0] != second || schedules[1] != first {
		t.Fatalf("agendamentos: %+v", schedules)
	}
}

// TestScheduleRuns: a nota guarda agendamento e mês, o índice recusa a
// segunda do mesmo par como ErrConflict, o aviso só é marcado uma vez por mês
// e apagar o agendamento não apaga a nota.
func TestScheduleRuns(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	schedule := domain.Schedule{TemplateID: 2, CustomerID: 1, Amount: 1000, Day: 28, Competence: domain.CompetenceEmission, Active: true, Production: true}
	_ = repo.SaveSchedule(ctx, &schedule)
	invoice := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: 1, CustomerID: 1, Status: domain.StatusPending,
		IssuedAt: time.Date(2026, 11, 28, 3, 0, 0, 0, brt), ScheduleID: schedule.ID, SchedulePeriod: "2026-11"}
	if err := repo.SaveInvoice(ctx, &invoice); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Invoice(ctx, invoice.ID)
	if got.ScheduleID != schedule.ID || got.SchedulePeriod != "2026-11" {
		t.Fatalf("vínculo: %+v", got)
	}
	again := invoice
	again.ID, again.DPSNumber = 0, 2
	if err := repo.SaveInvoice(ctx, &again); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("segunda nota do mês: %v", err)
	}
	nextMonth := again
	nextMonth.SchedulePeriod = "2026-12"
	if err := repo.SaveInvoice(ctx, &nextMonth); err != nil {
		t.Fatalf("mês seguinte: %v", err)
	}
	// Chave de idempotência: a mesma chave duas vezes é recusada pelo índice;
	// chave vazia nunca conflita.
	keyed := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: 10, IdempotencyKey: "pedido-1", IssuedAt: invoice.IssuedAt, Status: domain.StatusPending}
	if err := repo.SaveInvoice(ctx, &keyed); err != nil {
		t.Fatal(err)
	}
	if stored, _ := repo.Invoice(ctx, keyed.ID); stored.IdempotencyKey != "pedido-1" {
		t.Fatalf("chave não gravada: %+v", stored)
	}
	sameKey := keyed
	sameKey.ID, sameKey.DPSNumber = 0, 11
	if err := repo.SaveInvoice(ctx, &sameKey); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("mesma chave: %v", err)
	}
	for number := int64(12); number <= 13; number++ {
		unkeyed := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: number, IssuedAt: invoice.IssuedAt, Status: domain.StatusPending}
		if err := repo.SaveInvoice(ctx, &unkeyed); err != nil {
			t.Fatalf("sem chave %d: %v", number, err)
		}
	}

	for i, want := range []bool{true, false} {
		if claimed, err := repo.ClaimNotification(ctx, schedule.ID, "2026-11"); err != nil || claimed != want {
			t.Fatalf("aviso %d: %v, %v", i, claimed, err)
		}
	}
	schedules, _ := repo.Schedules(ctx)
	if !schedules[0].Production || schedules[0].NotifiedPeriod != "2026-11" {
		t.Fatalf("agendamento: %+v", schedules[0])
	}
	if err := repo.DeleteSchedule(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSchedule(ctx, schedule.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("apagar de novo: %v", err)
	}
	if _, err := repo.Invoice(ctx, invoice.ID); err != nil {
		t.Fatalf("nota sumiu com o agendamento: %v", err)
	}
	repo.Close()
	if _, err := repo.ClaimNotification(ctx, 1, "x"); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
}

// TestCorruptRowsAndClosedDatabase: linha com tipo errado e banco fechado
// viram erro em toda leitura e gravação, sem pânico.
func TestCorruptRowsAndClosedDatabase(t *testing.T) {
	repo, _ := openTemp(t)
	// As linhas abaixo entram com account_id padrão (1): lê pela conta 1.
	ctx := ports.WithAccount(context.Background(), 1)
	for _, statement := range []string{
		`INSERT INTO customers (name, foreign_customer) VALUES ('x', 'não-é-número')`,
		`INSERT INTO templates (name, description, national_tax_code, service_city_code, export) VALUES ('x', 'x', 'x', 'x', 'x')`,
		`INSERT INTO invoices (environment, series, dps_number, customer_id, amount_cents, competence, issued_at, status) VALUES ('h', '1', 'x', 1, 1, '', '', 'pending')`,
		`INSERT INTO schedules (template_id, customer_id, amount_cents, day, competence, notify_eve) VALUES (1, 1, 1, 1, 'emission', 'x')`,
	} {
		if _, err := repo.db.Exec(statement); err != nil {
			t.Fatalf("preparar linha corrompida: %v", err)
		}
	}
	if _, err := repo.Customers(ctx, ""); err == nil {
		t.Error("Customers com linha corrompida")
	}
	if _, err := repo.Templates(ctx, ""); err == nil {
		t.Error("Templates com linha corrompida")
	}
	if _, err := repo.Invoices(ctx); err == nil {
		t.Error("Invoices com linha corrompida")
	}
	if _, err := repo.Schedules(ctx); err == nil {
		t.Error("Schedules com linha corrompida")
	}

	repo.Close()
	_, e1 := repo.Customers(ctx, "")
	_, e2 := repo.Templates(ctx, "")
	_, e3 := repo.Invoices(ctx)
	_, e4 := repo.Schedules(ctx)
	errs := []error{e1, e2, e3, e4,
		repo.SaveCustomer(ctx, &domain.Customer{}), repo.SaveCustomer(ctx, &domain.Customer{ID: 1}),
		repo.SaveTemplate(ctx, &domain.Template{}), repo.SaveInvoice(ctx, &domain.Invoice{}),
		repo.SaveSchedule(ctx, &domain.Schedule{}),
	}
	for i, err := range errs {
		if err == nil || errors.Is(err, ports.ErrNotFound) {
			t.Errorf("operação %d com banco fechado: %v", i, err)
		}
	}
}

// TestMigrateErrors: falha ao criar o controle e ao aplicar migração viram erro.
func TestMigrateErrors(t *testing.T) {
	repo, _ := openTemp(t)
	repo.Close()
	if err := migrate(repo.db); err == nil {
		t.Fatal("migrar banco fechado deveria falhar")
	}
	fresh, _ := openTemp(t)
	if _, err := fresh.db.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	// Com o registro apagado, a migração tenta recriar tabelas que existem e falha.
	if err := migrate(fresh.db); err == nil {
		t.Fatal("reaplicar a migração deveria falhar")
	}
}

// TestAgentContacts: o contato do mesmo cliente é substituído, a lista vem do
// mais recente para o mais antigo e banco fechado vira erro.
func TestAgentContacts(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	first := time.Date(2026, 10, 30, 1, 0, 0, 0, brt)
	_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "claude-code", Tool: "list_invoices", At: first})
	_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "codex", Tool: "account_status", At: first.Add(time.Minute)})
	if err := repo.RecordAgentContact(ctx, domain.AgentContact{Client: "claude-code", Tool: "account_status", At: first.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	contacts, err := repo.AgentContacts(ctx)
	if err != nil || len(contacts) != 2 || contacts[0].Client != "claude-code" || contacts[0].Tool != "account_status" || !contacts[0].At.Equal(first.Add(2*time.Minute)) {
		t.Fatalf("contatos: %+v, %v", contacts, err)
	}
	repo.Close()
	if _, err := repo.AgentContacts(ctx); err == nil {
		t.Fatal("banco fechado deveria falhar na leitura")
	}
	if err := repo.RecordAgentContact(ctx, domain.AgentContact{Client: "x"}); err == nil {
		t.Fatal("banco fechado deveria falhar na gravação")
	}
}

// TestAPITokensAndCertificateStatus: token gravado só pelo hash, achado pelo
// hash, uso registrado, hash repetido recusado, apagar duas vezes é
// ErrNotFound; o estado do certificado começa vazio e guarda tudo o que recebe.
func TestAPITokensAndCertificateStatus(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	created := time.Date(2026, 10, 30, 1, 0, 0, 0, brt)
	first := domain.APIToken{Hint: "abcd", CreatedAt: created}
	if err := repo.CreateAPIToken(ctx, "hash-1", &first); err != nil || first.ID == 0 {
		t.Fatalf("criar: %v", err)
	}
	second := domain.APIToken{Hint: "wxyz", CreatedAt: created}
	_ = repo.CreateAPIToken(ctx, "hash-2", &second)
	if err := repo.CreateAPIToken(ctx, "hash-1", &domain.APIToken{Hint: "dup", CreatedAt: created}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("hash repetido: %v", err)
	}
	if err := repo.TouchAPIToken(ctx, first.ID, created.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	found, err := repo.APITokenByHash(ctx, "hash-1")
	if err != nil || found.Hint != "abcd" || !found.LastUsedAt.Equal(created.Add(time.Hour)) {
		t.Fatalf("por hash: %+v %v", found, err)
	}
	if _, err := repo.APITokenByHash(ctx, "nao-existe"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
	if list, _ := repo.APITokens(ctx); len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("lista: %+v", list)
	}
	if err := repo.DeleteAPIToken(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAPIToken(ctx, first.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("apagar de novo: %v", err)
	}

	if status, err := repo.CertificateStatus(ctx); err != nil || status.Info.CNPJ != "" || status.PortalAuthorized() {
		t.Fatalf("estado inicial: %+v %v", status, err)
	}
	want := domain.CertificateStatus{
		Info:      domain.CertificateInfo{HolderName: "EXEMPLO", CNPJ: "11222333000181", IssuerName: "AC", NotBefore: created, NotAfter: created.AddDate(1, 0, 0)},
		CheckedAt: created, PortalAuthorizedAt: created.Add(time.Minute),
	}
	_ = repo.SaveCertificateStatus(ctx, want)
	want.PortalAuthorizedAt = time.Time{}
	if err := repo.SaveCertificateStatus(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.CertificateStatus(ctx)
	if got.Info.CNPJ != "11222333000181" || !got.Info.NotAfter.Equal(want.Info.NotAfter) || got.PortalAuthorized() {
		t.Fatalf("estado: %+v", got)
	}

	repo.Close()
	if _, err := repo.APITokens(ctx); err == nil {
		t.Fatal("banco fechado deveria falhar na lista de tokens")
	}
	if _, err := repo.CertificateStatus(ctx); err == nil {
		t.Fatal("banco fechado deveria falhar no certificado")
	}
	if err := repo.SaveCertificateStatus(ctx, want); err == nil {
		t.Fatal("banco fechado deveria falhar ao gravar o certificado")
	}
}

// TestPasskeys: cria, recusa credencial repetida, atualiza contador e último
// uso, lista na ordem de cadastro e apaga todas; banco fechado vira erro.
func TestPasskeys(t *testing.T) {
	repo, _ := openTemp(t)
	ctx := context.Background()
	created := time.Date(2026, 10, 30, 1, 0, 0, 0, brt)
	first := domain.Passkey{CredentialID: []byte("cred-1"), Data: []byte(`{"v":1}`), CreatedAt: created}
	if err := repo.SavePasskey(ctx, &first); err != nil || first.ID == 0 {
		t.Fatalf("criar: %v", err)
	}
	_ = repo.SavePasskey(ctx, &domain.Passkey{CredentialID: []byte("cred-2"), Data: []byte(`{}`), CreatedAt: created})
	if err := repo.SavePasskey(ctx, &domain.Passkey{CredentialID: []byte("cred-1"), Data: []byte(`{}`), CreatedAt: created}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("credencial repetida: %v", err)
	}
	first.Data, first.LastUsedAt = []byte(`{"v":2}`), created.Add(time.Hour)
	if err := repo.SavePasskey(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePasskey(ctx, &domain.Passkey{ID: 999, Data: []byte(`{}`)}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("atualizar inexistente: %v", err)
	}
	list, _ := repo.Passkeys(ctx)
	if len(list) != 2 || string(list[0].Data) != `{"v":2}` || !list[0].LastUsedAt.Equal(created.Add(time.Hour)) || string(list[1].CredentialID) != "cred-2" {
		t.Fatalf("lista: %+v", list)
	}
	if err := repo.DeletePasskeys(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.Passkeys(ctx); len(list) != 0 {
		t.Fatal("não apagou")
	}
	repo.Close()
	if _, err := repo.Passkeys(ctx); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
	if err := repo.DeletePasskeys(ctx); err == nil {
		t.Fatal("banco fechado deveria falhar ao apagar")
	}
}

// TestAccountsAreIsolated: duas contas no mesmo banco nunca enxergam os dados
// uma da outra — emitente, tomador, modelo, nota, agendamento, certificado,
// token, passkey e contato de agente — e podem usar a mesma série e o mesmo
// número de DPS. A conta vem do context, como no painel; sem conta, nada.
func TestAccountsAreIsolated(t *testing.T) {
	repo, _ := openTemp(t)
	base := context.Background()
	created := time.Date(2026, 10, 30, 2, 0, 0, 0, brt)
	alice := domain.Account{Name: "A", Email: "a@exemplo.example", PasswordHash: []byte("h"), CreatedAt: created}
	bruno := domain.Account{Name: "B", Email: "b@exemplo.example", PasswordHash: []byte("h"), CreatedAt: created}
	_ = repo.CreateAccount(base, &alice)
	_ = repo.CreateAccount(base, &bruno)
	ctxA, ctxB := ports.WithAccount(base, alice.ID), ports.WithAccount(base, bruno.ID)

	// Cada conta grava o seu conjunto completo.
	for _, ctx := range []context.Context{ctxA, ctxB} {
		issuer := memory.DemoIssuer()
		issuer.LegalName = fmt.Sprintf("EMPRESA %d", ports.AccountID(ctx))
		if err := repo.SaveIssuer(ctx, issuer); err != nil {
			t.Fatal(err)
		}
		customer := domain.Customer{Name: issuer.LegalName + " CLIENTE", Document: "56789012000100"}
		_ = repo.SaveCustomer(ctx, &customer)
		template := domain.Template{Name: issuer.LegalName + " MODELO", NationalTaxCode: "010701", CustomerID: customer.ID}
		_ = repo.SaveTemplate(ctx, &template)
		series, number, err := repo.ReserveDPSNumber(ctx)
		if err != nil {
			t.Fatal(err)
		}
		invoice := domain.Invoice{Environment: domain.EnvHomologation, Series: series, DPSNumber: number, CustomerID: customer.ID,
			Status: domain.StatusPending, IssuedAt: created, IdempotencyKey: "mesma-chave"}
		if err := repo.SaveInvoice(ctx, &invoice); err != nil {
			t.Fatalf("mesma série e número em outra conta: %v", err)
		}
		schedule := domain.Schedule{TemplateID: template.ID, CustomerID: customer.ID, Amount: 1, Day: 1, Competence: domain.CompetenceEmission, Active: true}
		_ = repo.SaveSchedule(ctx, &schedule)
		_ = repo.SaveCertificateStatus(ctx, domain.CertificateStatus{Info: domain.CertificateInfo{HolderName: issuer.LegalName}})
		_ = repo.CreateAPIToken(ctx, fmt.Sprintf("hash-%d", ports.AccountID(ctx)), &domain.APIToken{Hint: "x", CreatedAt: created})
		_ = repo.SavePasskey(ctx, &domain.Passkey{CredentialID: []byte(issuer.LegalName), Data: []byte("{}"), CreatedAt: created})
		_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "claude-code", Tool: issuer.LegalName, At: created})
	}

	// Cada conta só enxerga o próprio.
	for _, ctx := range []context.Context{ctxA, ctxB} {
		want := fmt.Sprintf("EMPRESA %d", ports.AccountID(ctx))
		issuer, _ := repo.Issuer(ctx)
		customers, _ := repo.Customers(ctx, "")
		templates, _ := repo.Templates(ctx, "")
		invoices, _ := repo.Invoices(ctx)
		schedules, _ := repo.Schedules(ctx)
		certificate, _ := repo.CertificateStatus(ctx)
		tokens, _ := repo.APITokens(ctx)
		passkeys, _ := repo.Passkeys(ctx)
		contacts, _ := repo.AgentContacts(ctx)
		account, _ := repo.Account(ctx)
		if issuer.LegalName != want || len(customers) != 1 || customers[0].Name != want+" CLIENTE" || len(templates) != 1 ||
			templates[0].Name != want+" MODELO" || len(invoices) != 1 || invoices[0].DPSNumber != 1001 || len(schedules) != 1 ||
			certificate.Info.HolderName != want || len(tokens) != 1 || len(passkeys) != 1 || string(passkeys[0].CredentialID) != want ||
			len(contacts) != 1 || contacts[0].Tool != want || account.ID != ports.AccountID(ctx) {
			t.Fatalf("conta %d enxergou dado alheio ou perdeu o seu", ports.AccountID(ctx))
		}
		// Ler, alterar ou apagar pelo ID da outra conta não acha nada.
		other := ctxB
		if ctx == ctxB {
			other = ctxA
		}
		otherInvoices, _ := repo.Invoices(other)
		otherCustomers, _ := repo.Customers(other, "")
		otherSchedules, _ := repo.Schedules(other)
		if _, err := repo.Invoice(ctx, otherInvoices[0].ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("leu nota de outra conta: %v", err)
		}
		if _, err := repo.Customer(ctx, otherCustomers[0].ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("leu tomador de outra conta: %v", err)
		}
		stolen := otherInvoices[0]
		if err := repo.SaveInvoice(ctx, &stolen); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("alterou nota de outra conta: %v", err)
		}
		if err := repo.DeleteSchedule(ctx, otherSchedules[0].ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("apagou agendamento de outra conta: %v", err)
		}
		if claimed, _ := repo.ClaimNotification(ctx, otherSchedules[0].ID, "2026-11"); claimed {
			t.Error("marcou aviso de outra conta")
		}
	}

	// O token acha a conta dona dele, e é por isso que o MCP sabe de quem é a chamada.
	if token, err := repo.APITokenByHash(base, fmt.Sprintf("hash-%d", bruno.ID)); err != nil || token.AccountID != bruno.ID {
		t.Fatalf("token: %+v, %v", token, err)
	}
	// Sem conta no context e sem conta padrão: nada aparece.
	if invoices, _ := repo.Invoices(base); len(invoices) != 0 {
		t.Fatal("sem conta, apareceram notas")
	}
	// A conta padrão (For) vale quando o context não traz conta.
	if issuer, _ := repo.For(alice.ID).Issuer(base); issuer.LegalName != fmt.Sprintf("EMPRESA %d", alice.ID) {
		t.Fatalf("For: %+v", issuer)
	}
}
