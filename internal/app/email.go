package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Erros do envio por e-mail que a tela mostra como estão.
var (
	ErrEmailDisabled = errors.New("envio por e-mail desligado neste painel")
	ErrNoSender      = errors.New("configure o Gmail que envia (Tomadores → Envio por e-mail) antes de enviar")
	ErrNotEmailable  = errors.New("só nota autorizada é enviada por e-mail")
)

// emailTestPrefix marca o assunto do e-mail de teste, para quem recebe não
// confundir com uma nota de verdade.
const emailTestPrefix = "[TESTE] "

// EmailSenderInput é o remetente como chega da tela. Password vazio mantém a
// senha já guardada (o campo nunca volta preenchido para o navegador).
type EmailSenderInput struct {
	Address  string
	Name     string
	Password string
}

// EmailEnabled diz se o painel tem o necessário para enviar.
func (s *Service) EmailEnabled() bool {
	return s.Mailer != nil && s.Sealer != nil && s.Documents != nil
}

// EmailSettings devolve o remetente da conta (vazio antes do primeiro
// cadastro) e o envio do tomador (o padrão, se nunca foi salvo).
func (s *Service) EmailSettings(ctx context.Context, customerID int64) (domain.EmailSender, domain.CustomerEmail, error) {
	customer, err := s.Repo.Customer(ctx, customerID)
	if err != nil {
		return domain.EmailSender{}, domain.CustomerEmail{}, err
	}
	sender, err := s.Repo.EmailSender(ctx)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return domain.EmailSender{}, domain.CustomerEmail{}, err
	}
	settings, err := s.Repo.CustomerEmail(ctx, customerID)
	if errors.Is(err, ports.ErrNotFound) {
		return sender, domain.DefaultCustomerEmail(customer), nil
	}
	return sender, settings, err
}

// normalizePassword tira os espaços: o Google mostra a senha de app em
// quatro blocos ("abcd efgh ijkl mnop"), mas o login é sem eles.
func normalizePassword(password string) string {
	return strings.Join(strings.Fields(password), "")
}

// SaveEmailSettings grava o remetente e o envio do tomador. Senha nova é
// testada com um login no Gmail ANTES de gravar: senha errada nunca fica
// guardada para falhar só na próxima nota.
func (s *Service) SaveEmailSettings(ctx context.Context, customerID int64, input EmailSenderInput, settings domain.CustomerEmail) error {
	if !s.EmailEnabled() {
		return ErrEmailDisabled
	}
	current, _, err := s.EmailSettings(ctx, customerID)
	if err != nil {
		return err
	}
	settings.CustomerID = customerID
	errs := domain.FieldErrors{}
	if err := settings.Validate(); err != nil {
		errs = err.(domain.FieldErrors)
	}
	address, password := strings.TrimSpace(input.Address), normalizePassword(input.Password)
	switch {
	case address == "":
		errs["sender_address"] = "informe o Gmail que envia"
	case !domain.ValidEmail(address):
		errs["sender_address"] = "e-mail inválido: " + address
	case password == "" && (address != current.Address || !current.Configured()):
		errs["sender_password"] = "informe a senha de app do Google (o login é testado ao salvar)"
	}
	if len(errs) > 0 {
		return errs
	}
	sender := current
	sender.Address, sender.Name = address, strings.TrimSpace(input.Name)
	if password != "" {
		if err := s.Mailer.Verify(ctx, address, password); err != nil {
			return domain.FieldErrors{"sender_password": err.Error()}
		}
		if sender.SealedPassword, err = s.Sealer.Seal(ctx, []byte(password)); err != nil {
			return fmt.Errorf("guardar a senha de app: %w", err)
		}
		sender.VerifiedAt = s.Now()
	}
	if err := s.Repo.SaveEmailSender(ctx, sender); err != nil {
		return err
	}
	return s.Repo.SaveCustomerEmail(ctx, settings)
}

// SendTestEmail manda o texto da tela (mesmo sem salvar) com dados de
// exemplo para o endereço dado: assunto com [TESTE], sem cópia, anexos de
// simulação. Nada é gravado. Sem senha digitada, usa a guardada — desde que
// o Gmail seja o mesmo.
func (s *Service) SendTestEmail(ctx context.Context, customerID int64, input EmailSenderInput, draft domain.CustomerEmail, to string) error {
	if !s.EmailEnabled() {
		return ErrEmailDisabled
	}
	current, _, err := s.EmailSettings(ctx, customerID)
	if err != nil {
		return err
	}
	recipients, err := domain.ParseEmailList(to)
	if err != nil || len(recipients) == 0 {
		return domain.FieldErrors{"test_to": "informe um e-mail válido para o teste"}
	}
	sender := domain.EmailSender{Address: strings.TrimSpace(input.Address), Name: strings.TrimSpace(input.Name)}
	if !domain.ValidEmail(sender.Address) {
		return domain.FieldErrors{"sender_address": "informe o Gmail que envia"}
	}
	password := normalizePassword(input.Password)
	if password == "" {
		if sender.Address != current.Address || !current.Configured() {
			return domain.FieldErrors{"sender_password": "informe a senha de app do Google para o teste"}
		}
		if password, err = s.openPassword(ctx, current); err != nil {
			return err
		}
	}
	issuer, customer, err := s.issuerAndCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	sample := domain.SampleInvoice(issuer, customer, s.Now().In(s.Location))
	draft.CC = ""
	mail := compose(sender, draft, domain.InvoiceEmailValues(issuer, customer, sample), recipients)
	mail.Subject = emailTestPrefix + mail.Subject
	if draft.AttachPDF || draft.AttachXML {
		pdf, xml, err := s.Documents.Sample(ctx, issuer, customer, sample)
		if err != nil {
			return fmt.Errorf("gerar os anexos de exemplo: %w", err)
		}
		mail.Attachments = attachments(draft, sample, pdf, xml)
	}
	return s.Mailer.Send(ctx, password, mail)
}

// SendInvoiceEmail manda a nota autorizada para os destinatários do
// tomador e registra o resultado (o envio e a falha aparecem no detalhe da
// nota). automatic marca o envio feito logo depois da autorização.
func (s *Service) SendInvoiceEmail(ctx context.Context, invoiceID int64, automatic bool) (domain.InvoiceEmail, error) {
	if !s.EmailEnabled() {
		return domain.InvoiceEmail{}, ErrEmailDisabled
	}
	invoice, err := s.Repo.Invoice(ctx, invoiceID)
	if err != nil {
		return domain.InvoiceEmail{}, err
	}
	if invoice.Status != domain.StatusAuthorized {
		return domain.InvoiceEmail{}, ErrNotEmailable
	}
	sender, settings, err := s.EmailSettings(ctx, invoice.CustomerID)
	if err != nil {
		return domain.InvoiceEmail{}, err
	}
	record := domain.InvoiceEmail{InvoiceID: invoice.ID, SentAt: s.Now(), To: settings.To, Automatic: automatic}
	sendErr := s.deliver(ctx, invoice, sender, settings)
	if sendErr != nil {
		record.Error = sendErr.Error()
	}
	if err := s.Repo.RecordInvoiceEmail(ctx, &record); err != nil {
		return record, errors.Join(sendErr, fmt.Errorf("registrar o envio: %w", err))
	}
	return record, sendErr
}

// deliver monta e envia o e-mail de uma nota.
func (s *Service) deliver(ctx context.Context, invoice domain.Invoice, sender domain.EmailSender, settings domain.CustomerEmail) error {
	if !sender.Configured() {
		return ErrNoSender
	}
	to, err := domain.ParseEmailList(settings.To)
	if err != nil {
		return err
	}
	if len(to) == 0 {
		return errors.New("o tomador não tem e-mail de destino (Tomadores → Envio por e-mail → Para)")
	}
	password, err := s.openPassword(ctx, sender)
	if err != nil {
		return err
	}
	issuer, customer, err := s.issuerAndCustomer(ctx, invoice.CustomerID)
	if err != nil {
		return err
	}
	mail := compose(sender, settings, domain.InvoiceEmailValues(issuer, customer, invoice), to)
	if settings.AttachPDF || settings.AttachXML {
		pdf, xml, err := s.Documents.Invoice(ctx, invoice)
		if err != nil {
			return fmt.Errorf("gerar os anexos: %w", err)
		}
		mail.Attachments = attachments(settings, invoice, pdf, xml)
	}
	return s.Mailer.Send(ctx, password, mail)
}

// autoEmail manda a nota recém-autorizada quando o tomador pediu envio
// automático. Falha não desfaz a emissão: fica no registro de envios da nota
// e no log, e o botão "Enviar por e-mail" continua lá.
func (s *Service) autoEmail(ctx context.Context, invoice domain.Invoice) {
	if !s.EmailEnabled() {
		return
	}
	settings, err := s.Repo.CustomerEmail(ctx, invoice.CustomerID)
	if err != nil || !settings.AutoSend {
		return
	}
	if _, err := s.SendInvoiceEmail(ctx, invoice.ID, true); err != nil {
		log.Printf("e-mail automático da nota #%d: %v", invoice.ID, err)
	}
}

// scheduledEmail atende a caixa "Enviar a nota ao tomador após a emissão"
// do agendamento: manda a nota autorizada mesmo sem o envio automático do
// tomador ligado. Com ele ligado, o autoEmail já mandou — não sai em dobro.
func (s *Service) scheduledEmail(ctx context.Context, invoice domain.Invoice) {
	if !s.EmailEnabled() || invoice.Status != domain.StatusAuthorized {
		return
	}
	if settings, err := s.Repo.CustomerEmail(ctx, invoice.CustomerID); err == nil && settings.AutoSend {
		return
	}
	if _, err := s.SendInvoiceEmail(ctx, invoice.ID, true); err != nil {
		log.Printf("e-mail da nota agendada #%d: %v", invoice.ID, err)
	}
}

// openPassword abre a senha de app guardada.
func (s *Service) openPassword(ctx context.Context, sender domain.EmailSender) (string, error) {
	plain, err := s.Sealer.Open(ctx, sender.SealedPassword)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// issuerAndCustomer lê o emitente e o tomador que entram no texto.
func (s *Service) issuerAndCustomer(ctx context.Context, customerID int64) (domain.Issuer, domain.Customer, error) {
	issuer, err := s.Repo.Issuer(ctx)
	if err != nil {
		return domain.Issuer{}, domain.Customer{}, fmt.Errorf("ler emitente: %w", err)
	}
	customer, err := s.Repo.Customer(ctx, customerID)
	return issuer, customer, err
}

// compose monta o e-mail com o texto do tomador já resolvido. Quebra de
// linha no assunto vira espaço: o assunto é uma linha só no cabeçalho.
func compose(sender domain.EmailSender, settings domain.CustomerEmail, values domain.EmailValues, to []string) ports.Mail {
	cc, _ := domain.ParseEmailList(settings.CC)
	subject := strings.Join(strings.Fields(domain.RenderEmailTemplate(settings.Subject, values)), " ")
	return ports.Mail{
		From: sender.Address, FromName: strings.Join(strings.Fields(sender.Name), " "), To: to, CC: cc,
		Subject: subject, Body: domain.RenderEmailTemplate(settings.Body, values),
	}
}

// attachments monta os anexos marcados, com os nomes da prévia
// (danfse-<número>.pdf e nfse-<número>.xml).
func attachments(settings domain.CustomerEmail, invoice domain.Invoice, pdf, xml []byte) []ports.MailAttachment {
	var list []ports.MailAttachment
	if settings.AttachPDF {
		list = append(list, ports.MailAttachment{Name: "danfse-" + invoice.NFSeNumber + ".pdf", ContentType: "application/pdf", Data: pdf})
	}
	if settings.AttachXML {
		list = append(list, ports.MailAttachment{Name: "nfse-" + invoice.NFSeNumber + ".xml", ContentType: "application/xml", Data: xml})
	}
	return list
}

// EmailPreview resolve assunto e mensagem com dados de exemplo, para a
// prévia da tela (nada é enviado nem gravado).
func (s *Service) EmailPreview(ctx context.Context, customerID int64, draft domain.CustomerEmail) (subject, body string, err error) {
	issuer, customer, err := s.issuerAndCustomer(ctx, customerID)
	if err != nil {
		return "", "", err
	}
	values := domain.InvoiceEmailValues(issuer, customer, domain.SampleInvoice(issuer, customer, s.Now().In(s.Location)))
	mail := compose(domain.EmailSender{}, draft, values, nil)
	return mail.Subject, mail.Body, nil
}
