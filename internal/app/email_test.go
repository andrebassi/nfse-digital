package app

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// fakeMailer guarda os e-mails e a senha usada; verifyErr e sendErr simulam
// o Gmail recusando.
type fakeMailer struct {
	sent      []ports.Mail
	passwords []string
	verified  int
	verifyErr error
	sendErr   error
}

// Verify cumpre ports.Mailer para o teste.
func (m *fakeMailer) Verify(_ context.Context, _, password string) error {
	m.verified++
	m.passwords = append(m.passwords, password)
	return m.verifyErr
}

// Send cumpre ports.Mailer para o teste.
func (m *fakeMailer) Send(_ context.Context, password string, mail ports.Mail) error {
	m.passwords = append(m.passwords, password)
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, mail)
	return nil
}

// fakeSealer "sela" invertendo os bytes e prefixando, para o teste conferir
// que a senha nunca é guardada em claro; openErr simula chave trocada.
type fakeSealer struct {
	sealErr, openErr error
}

// Seal cumpre ports.SecretSealer para o teste.
func (s fakeSealer) Seal(_ context.Context, plain []byte) ([]byte, error) {
	reversed := bytes.Clone(plain)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return append([]byte("selado:"), reversed...), s.sealErr
}

// Open cumpre ports.SecretSealer para o teste.
func (s fakeSealer) Open(_ context.Context, sealed []byte) ([]byte, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	reversed := bytes.Clone(bytes.TrimPrefix(sealed, []byte("selado:")))
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

// fakeDocuments devolve anexos fixos e conta as chamadas.
type fakeDocuments struct {
	err              error
	invoice, samples int
}

// Invoice cumpre ports.InvoiceDocuments para o teste.
func (d *fakeDocuments) Invoice(context.Context, domain.Invoice) ([]byte, []byte, error) {
	d.invoice++
	return []byte("%PDF real"), []byte("<NFSe/>"), d.err
}

// Sample cumpre ports.InvoiceDocuments para o teste.
func (d *fakeDocuments) Sample(context.Context, domain.Issuer, domain.Customer, domain.Invoice) ([]byte, []byte, error) {
	d.samples++
	return []byte("%PDF simulação"), []byte("<simulacao/>"), d.err
}

// emailFixture é o caso de uso com envio ligado sobre o seed em memória.
type emailFixture struct {
	service  *Service
	repo     *memory.Repository
	mailer   *fakeMailer
	docs     *fakeDocuments
	customer domain.Customer
}

// newEmailFixture liga o envio e escolhe o tomador Ipê do seed.
func newEmailFixture(t *testing.T, emitter ports.Emitter) emailFixture {
	t.Helper()
	service, repo := newService(emitter)
	mailer, docs := &fakeMailer{}, &fakeDocuments{}
	service.Mailer, service.Sealer, service.Documents = mailer, fakeSealer{}, docs
	customers, _ := repo.Customers(context.Background(), "ipe")
	return emailFixture{service: service, repo: repo, mailer: mailer, docs: docs, customer: customers[0]}
}

// goodSettings é um envio válido para o tomador.
func goodSettings() domain.CustomerEmail {
	settings := domain.DefaultCustomerEmail(domain.Customer{Email: "nf@cliente.example"})
	settings.CC = "copia@cliente.example"
	return settings
}

// sender é o remetente digitado na tela, com a senha nos quatro blocos do Google.
var sender = EmailSenderInput{Address: "dono@gmail.example", Name: "Financeiro", Password: "abcd efgh ijkl mnop"}

// TestEmailSettingsDefaults: tomador sem envio salvo recebe o padrão (Para =
// e-mail do cadastro); tomador inexistente é ErrNotFound.
func TestEmailSettingsDefaults(t *testing.T) {
	f := newEmailFixture(t, nil)
	ctx := context.Background()
	current, settings, err := f.service.EmailSettings(ctx, f.customer.ID)
	if err != nil || current.Configured() || settings.To != f.customer.Email || settings.Subject != domain.DefaultEmailSubject || settings.AutoSend {
		t.Fatalf("padrão: %+v %+v %v", current, settings, err)
	}
	if _, _, err := f.service.EmailSettings(ctx, 99999); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
}

// TestSaveEmailSettings: a senha é testada no Gmail e guardada SELADA e sem
// espaços; salvar de novo sem senha mantém a guardada; trocar o Gmail sem
// senha, Gmail vazio ou inválido e Para inválido voltam por campo; o Gmail
// recusando o login não grava nada; envio desligado é ErrEmailDisabled.
func TestSaveEmailSettings(t *testing.T) {
	f := newEmailFixture(t, nil)
	ctx := context.Background()
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, EmailSenderInput{Address: "dono@gmail.example"}, goodSettings()); fieldError(err, "sender_password") == "" {
		t.Fatalf("primeiro cadastro sem senha: %v", err)
	}
	f.mailer.verifyErr = errors.New("o Gmail recusou o login")
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings()); !strings.Contains(fieldError(err, "sender_password"), "recusou") {
		t.Fatalf("login recusado: %v", err)
	}
	if _, err := f.repo.EmailSender(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("login recusado gravou o remetente")
	}
	f.mailer.verifyErr = nil
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings()); err != nil {
		t.Fatal(err)
	}
	stored, _ := f.repo.EmailSender(ctx)
	if !stored.Configured() || bytes.Contains(stored.SealedPassword, []byte("abcdefghijklmnop")) || f.mailer.passwords[len(f.mailer.passwords)-1] != "abcdefghijklmnop" || stored.VerifiedAt.IsZero() {
		t.Fatalf("remetente: %+v, senhas %v", stored, f.mailer.passwords)
	}
	verified := f.mailer.verified
	keep := goodSettings()
	keep.AutoSend = true
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, EmailSenderInput{Address: "dono@gmail.example", Name: "Outro nome"}, keep); err != nil || f.mailer.verified != verified {
		t.Fatalf("salvar sem senha: %v (verificou de novo? %d)", err, f.mailer.verified)
	}
	if again, _ := f.repo.EmailSender(ctx); !bytes.Equal(again.SealedPassword, stored.SealedPassword) || again.Name != "Outro nome" {
		t.Fatalf("senha guardada perdida: %+v", again)
	}
	if saved, _ := f.repo.CustomerEmail(ctx, f.customer.ID); !saved.AutoSend || saved.CC != "copia@cliente.example" {
		t.Fatalf("envio do tomador: %+v", saved)
	}
	for name, try := range map[string]struct {
		input EmailSenderInput
		field string
	}{
		"Gmail trocado sem senha": {EmailSenderInput{Address: "outro@gmail.example"}, "sender_password"},
		"Gmail vazio":             {EmailSenderInput{}, "sender_address"},
		"Gmail inválido":          {EmailSenderInput{Address: "x"}, "sender_address"},
	} {
		if err := f.service.SaveEmailSettings(ctx, f.customer.ID, try.input, goodSettings()); fieldError(err, try.field) == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := goodSettings()
	bad.To = "não é e-mail"
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, bad); fieldError(err, "to") == "" {
		t.Errorf("Para inválido: %v", err)
	}
	f.service.Sealer = fakeSealer{sealErr: errors.New("sem chave")}
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings()); err == nil || !strings.Contains(err.Error(), "guardar a senha") {
		t.Errorf("selo falhou: %v", err)
	}
	if err := f.service.SaveEmailSettings(ctx, 99999, sender, goodSettings()); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("tomador inexistente: %v", err)
	}
	f.service.Mailer = nil
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings()); !errors.Is(err, ErrEmailDisabled) {
		t.Errorf("desligado: %v", err)
	}
}

// fieldError devolve a mensagem do campo quando err é FieldErrors.
func fieldError(err error, field string) string {
	var errs domain.FieldErrors
	if errors.As(err, &errs) {
		return errs[field]
	}
	return ""
}

// TestSendTestEmail: o teste sai com o texto da tela (sem salvar), assunto
// [TESTE], sem cópia e os anexos de simulação; sem senha digitada usa a
// guardada; recusa e-mail de teste inválido, Gmail inválido e falta de senha.
func TestSendTestEmail(t *testing.T) {
	f := newEmailFixture(t, nil)
	ctx := context.Background()
	draft := goodSettings()
	draft.Subject = "Nota {numero}\n de {tomador}"
	if err := f.service.SendTestEmail(ctx, f.customer.ID, sender, draft, "eu@exemplo.example"); err != nil {
		t.Fatal(err)
	}
	mail := f.mailer.sent[0]
	if mail.Subject != "[TESTE] Nota 123 de "+f.customer.Name || len(mail.CC) != 0 || mail.To[0] != "eu@exemplo.example" ||
		len(mail.Attachments) != 2 || string(mail.Attachments[0].Data) != "%PDF simulação" || mail.Attachments[0].Name != "danfse-123.pdf" {
		t.Fatalf("teste: %+v", mail)
	}
	if _, err := f.repo.CustomerEmail(ctx, f.customer.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("o teste gravou o envio do tomador")
	}
	for name, try := range map[string]struct {
		input EmailSenderInput
		to    string
		field string
	}{
		"teste sem destino":  {sender, "", "test_to"},
		"Gmail inválido":     {EmailSenderInput{Address: "x", Password: "p"}, "eu@exemplo.example", "sender_address"},
		"sem senha guardada": {EmailSenderInput{Address: "dono@gmail.example"}, "eu@exemplo.example", "sender_password"},
	} {
		if err := f.service.SendTestEmail(ctx, f.customer.ID, try.input, draft, try.to); fieldError(err, try.field) == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	_ = f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings())
	noAttachments := goodSettings()
	noAttachments.AttachPDF, noAttachments.AttachXML = false, false
	samples := f.docs.samples
	if err := f.service.SendTestEmail(ctx, f.customer.ID, EmailSenderInput{Address: "dono@gmail.example"}, noAttachments, "eu@exemplo.example"); err != nil {
		t.Fatal(err)
	}
	if last := f.mailer.passwords[len(f.mailer.passwords)-1]; last != "abcdefghijklmnop" || f.docs.samples != samples || len(f.mailer.sent[1].Attachments) != 0 {
		t.Fatalf("senha guardada no teste: %q, anexos %d", last, len(f.mailer.sent[1].Attachments))
	}
	f.docs.err = errors.New("sem fonte")
	if err := f.service.SendTestEmail(ctx, f.customer.ID, sender, draft, "eu@exemplo.example"); err == nil || !strings.Contains(err.Error(), "anexos de exemplo") {
		t.Errorf("anexo falhou: %v", err)
	}
	f.service.Sealer = fakeSealer{openErr: errors.New("chave trocada")}
	if err := f.service.SendTestEmail(ctx, f.customer.ID, EmailSenderInput{Address: "dono@gmail.example"}, noAttachments, "eu@exemplo.example"); err == nil {
		t.Error("senha guardada que não abre devia falhar")
	}
	if err := f.service.SendTestEmail(ctx, 99999, sender, draft, "eu@exemplo.example"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("tomador inexistente: %v", err)
	}
	broken := newEmailFixture(t, nil)
	broken.service.Repo = issuerFailRepo{broken.repo}
	if err := broken.service.SendTestEmail(ctx, broken.customer.ID, sender, draft, "eu@exemplo.example"); err == nil {
		t.Error("sem emitente devia falhar")
	}
	f.service.Documents = nil
	if err := f.service.SendTestEmail(ctx, f.customer.ID, sender, draft, "eu@exemplo.example"); !errors.Is(err, ErrEmailDisabled) {
		t.Errorf("desligado: %v", err)
	}
}

// TestAutoEmailAfterAuthorization: com envio automático ligado, a nota
// autorizada sai sozinha para Para e Cópia, com o DANFSe e o XML de verdade,
// e o envio fica registrado como automático; desligado, nada sai; o Gmail
// falhando não desfaz a emissão — a falha fica registrada na nota.
func TestAutoEmailAfterAuthorization(t *testing.T) {
	f := newEmailFixture(t, &uniqueEmitter{})
	ctx := context.Background()
	input := demoInput(t, f.repo)
	if _, err := f.service.Emit(ctx, input); err != nil || len(f.mailer.sent) != 0 {
		t.Fatalf("sem configuração saiu e-mail: %v %d", err, len(f.mailer.sent))
	}
	settings := goodSettings()
	settings.AutoSend = true
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, settings); err != nil {
		t.Fatal(err)
	}
	invoice, err := f.service.Emit(ctx, input)
	if err != nil || len(f.mailer.sent) != 1 {
		t.Fatalf("automático: %v, %d e-mails", err, len(f.mailer.sent))
	}
	mail := f.mailer.sent[0]
	if strings.Join(mail.To, ",") != "nf@cliente.example" || mail.CC[0] != "copia@cliente.example" || mail.From != "dono@gmail.example" ||
		mail.FromName != "Financeiro" || string(mail.Attachments[1].Data) != "<NFSe/>" || !strings.Contains(mail.Body, "R$ 19.000,00") {
		t.Fatalf("e-mail: %+v", mail)
	}
	if sent, _ := f.repo.InvoiceEmails(ctx, invoice.ID); len(sent) != 1 || !sent[0].Automatic || sent[0].Error != "" {
		t.Fatalf("registro: %+v", sent)
	}
	f.mailer.sendErr = errors.New("Gmail fora")
	failed, err := f.service.Emit(ctx, input)
	if err != nil || failed.Status != domain.StatusAuthorized {
		t.Fatalf("falha no e-mail desfez a emissão: %v %v", err, failed.Status)
	}
	if sent, _ := f.repo.InvoiceEmails(ctx, failed.ID); len(sent) != 1 || sent[0].Error != "Gmail fora" {
		t.Fatalf("falha não registrada: %+v", sent)
	}
}

// TestSendInvoiceEmail: o botão manual manda a nota autorizada e registra;
// nota não autorizada, sem remetente, sem Para, anexo e senha que falham
// voltam como erro (e as falhas de envio ficam registradas na nota).
func TestSendInvoiceEmail(t *testing.T) {
	f := newEmailFixture(t, &uniqueEmitter{})
	ctx := context.Background()
	invoice, _ := f.service.Emit(ctx, demoInput(t, f.repo))
	if record, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); !errors.Is(err, ErrNoSender) || record.Error == "" {
		t.Fatalf("sem remetente: %+v %v", record, err)
	}
	if err := f.service.SaveEmailSettings(ctx, f.customer.ID, sender, goodSettings()); err != nil {
		t.Fatal(err)
	}
	record, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false)
	if err != nil || record.Automatic || record.To != "nf@cliente.example" || f.docs.invoice != 1 {
		t.Fatalf("manual: %+v %v", record, err)
	}
	if sent, _ := f.repo.InvoiceEmails(ctx, invoice.ID); len(sent) != 2 || sent[0].ID != record.ID {
		t.Fatalf("histórico: %+v", sent)
	}
	f.docs.err = errors.New("XML indisponível")
	if _, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); err == nil || !strings.Contains(err.Error(), "anexos") {
		t.Errorf("anexo: %v", err)
	}
	f.docs.err = nil
	f.service.Sealer = fakeSealer{openErr: errors.New("chave trocada")}
	if _, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); err == nil {
		t.Error("senha que não abre devia falhar")
	}
	f.service.Sealer = fakeSealer{}
	noTo := goodSettings()
	noTo.To = ""
	_ = f.repo.SaveCustomerEmail(ctx, domain.CustomerEmail{CustomerID: f.customer.ID, Subject: "s", Body: "b"})
	if _, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); err == nil || !strings.Contains(err.Error(), "Para") {
		t.Errorf("sem Para: %v", err)
	}
	_ = f.repo.SaveCustomerEmail(ctx, domain.CustomerEmail{CustomerID: f.customer.ID, To: "ruim", Subject: "s", Body: "b"})
	if _, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); err == nil || !strings.Contains(err.Error(), "ruim") {
		t.Errorf("Para inválido guardado: %v", err)
	}
	broken := newEmailFixture(t, &uniqueEmitter{})
	issued, _ := broken.service.Emit(ctx, demoInput(t, broken.repo))
	_ = broken.service.SaveEmailSettings(ctx, broken.customer.ID, sender, goodSettings())
	broken.service.Repo = issuerFailRepo{broken.repo}
	if _, err := broken.service.SendInvoiceEmail(ctx, issued.ID, false); err == nil || !strings.Contains(err.Error(), "emitente") {
		t.Errorf("sem emitente: %v", err)
	}
	others, _ := f.repo.Invoices(ctx)
	for _, other := range others {
		if other.Status == domain.StatusRejected {
			if _, err := f.service.SendInvoiceEmail(ctx, other.ID, false); !errors.Is(err, ErrNotEmailable) {
				t.Errorf("rejeitada: %v", err)
			}
			break
		}
	}
	if _, err := f.service.SendInvoiceEmail(ctx, 99999, false); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("inexistente: %v", err)
	}
	recordFail := newEmailFixture(t, &uniqueEmitter{})
	issued, _ = recordFail.service.Emit(ctx, demoInput(t, recordFail.repo))
	_ = recordFail.service.SaveEmailSettings(ctx, recordFail.customer.ID, sender, goodSettings())
	recordFail.service.Repo = recordFailRepo{recordFail.repo}
	if _, err := recordFail.service.SendInvoiceEmail(ctx, issued.ID, false); err == nil || !strings.Contains(err.Error(), "registrar") {
		t.Errorf("registro falhou: %v", err)
	}
	f.service.Mailer = nil
	if _, err := f.service.SendInvoiceEmail(ctx, invoice.ID, false); !errors.Is(err, ErrEmailDisabled) {
		t.Errorf("desligado: %v", err)
	}
}

// uniqueEmitter autoriza toda DPS com chave e número próprios (o banco
// recusa duas notas com a mesma chave de acesso).
type uniqueEmitter struct{ count int }

// Emit cumpre ports.Emitter para o teste.
func (e *uniqueEmitter) Emit(_ context.Context, request ports.EmitRequest) (ports.EmitResult, error) {
	e.count++
	number := strconv.Itoa(e.count)
	return ports.EmitResult{Status: domain.StatusAuthorized, AccessKey: "CHAVE-" + number, NFSeNumber: number}, nil
}

// TestScheduledEmail: a caixa "Enviar a nota ao tomador" do agendamento
// manda a nota autorizada — também quando ela só sai no reenvio da hora
// seguinte; com o envio automático do tomador ligado, sai UM e-mail, não
// dois; sem a caixa, nenhum.
func TestScheduledEmail(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, sendToCustomer bool) (*Scheduler, *memory.Repository, *fakeMailer, *scriptedEmitter, int64) {
		schedule := monthlySchedule()
		schedule.SendToCustomer = sendToCustomer
		scheduler, repo, emitter := schedulerFixture(t, memory.DemoIssuer(), schedule)
		mailer := &fakeMailer{}
		scheduler.Service.Mailer, scheduler.Service.Sealer, scheduler.Service.Documents = mailer, fakeSealer{}, &fakeDocuments{}
		customers, _ := repo.Customers(ctx, "")
		settings := goodSettings()
		if err := scheduler.Service.SaveEmailSettings(ctx, customers[0].ID, sender, settings); err != nil {
			t.Fatal(err)
		}
		return scheduler, repo, mailer, emitter, customers[0].ID
	}
	scheduler, _, mailer, _, _ := setup(t, true)
	if _, err := scheduler.Run(ctx, at(28, 3, 0), false); err != nil || len(mailer.sent) != 1 {
		t.Fatalf("agendada com envio: %v, %d e-mails", err, len(mailer.sent))
	}
	scheduler, _, mailer, _, _ = setup(t, false)
	_, _ = scheduler.Run(ctx, at(28, 3, 0), false)
	if len(mailer.sent) != 0 {
		t.Fatalf("sem a caixa saiu e-mail: %d", len(mailer.sent))
	}
	scheduler, repo, mailer, emitter, customerID := setup(t, true)
	settings, _ := repo.CustomerEmail(ctx, customerID)
	settings.AutoSend = true
	_ = repo.SaveCustomerEmail(ctx, settings)
	emitter.errs = []error{errors.New("SEFIN fora")}
	scheduler.Service.Now = func() time.Time { return at(28, 3, 0) }
	_, _ = scheduler.Run(ctx, at(28, 3, 0), false)
	if len(mailer.sent) != 0 {
		t.Fatal("nota pendente saiu por e-mail")
	}
	_, _ = scheduler.Run(ctx, at(28, 4, 0), false)
	if len(mailer.sent) != 1 {
		t.Fatalf("reenvio autorizado com automático ligado: %d e-mails (esperado 1, sem dobra)", len(mailer.sent))
	}
}

// recordFailRepo falha ao registrar o envio.
type recordFailRepo struct{ *memory.Repository }

// RecordInvoiceEmail sempre falha.
func (recordFailRepo) RecordInvoiceEmail(context.Context, *domain.InvoiceEmail) error {
	return errors.New("banco fora")
}

// senderFailRepo falha ao ler o remetente.
type senderFailRepo struct{ *memory.Repository }

// EmailSender sempre falha.
func (senderFailRepo) EmailSender(context.Context) (domain.EmailSender, error) {
	return domain.EmailSender{}, errors.New("banco fora")
}

// TestEmailPreviewAndReadFailure: a prévia resolve os marcadores com a nota
// de exemplo; banco fora ao ler o remetente sobe como erro.
func TestEmailPreviewAndReadFailure(t *testing.T) {
	f := newEmailFixture(t, nil)
	ctx := context.Background()
	subject, body, err := f.service.EmailPreview(ctx, f.customer.ID, goodSettings())
	if err != nil || !strings.HasPrefix(subject, "NFS-e nº 123 - ") || !strings.Contains(body, "R$ 1.500,00") || !strings.Contains(body, "ConsultaPublica") {
		t.Fatalf("prévia: %q %q %v", subject, body, err)
	}
	if _, _, err := f.service.EmailPreview(ctx, 99999, goodSettings()); err == nil {
		t.Error("prévia de tomador inexistente devia falhar")
	}
	f.service.Repo = senderFailRepo{f.repo}
	if _, _, err := f.service.EmailSettings(ctx, f.customer.ID); err == nil {
		t.Error("banco fora devia falhar")
	}
}
