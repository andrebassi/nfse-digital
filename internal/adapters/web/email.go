package web

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// customerEmailView alimenta a aba "Envio por e-mail" do tomador.
type customerEmailView struct {
	Customer domain.Customer
	// Sender é o remetente guardado (a senha nunca volta para a tela).
	Sender   domain.EmailSender
	Settings domain.CustomerEmail
	// SenderAddress e SenderName são o que está no formulário (pode ser
	// diferente do guardado, quando a tela volta com erro).
	SenderAddress, SenderName string
	TestTo                    string
	Errors                    domain.FieldErrors
	// Message é o resultado do envio de teste ou o erro que não é de campo.
	Message      string
	MessageError bool
	Preview      emailPreview
	Placeholders []string
}

// emailPreview é a prévia do e-mail com os dados de exemplo.
type emailPreview struct {
	From, To, Subject, Body string
	Attachments             []string
}

// emailForm lê o formulário da aba (tanto do salvar quanto do teste e da prévia).
func emailForm(r *http.Request, customerID int64) (app.EmailSenderInput, domain.CustomerEmail) {
	value := func(name string) string { return r.PostFormValue(name) }
	sender := app.EmailSenderInput{Address: strings.TrimSpace(value("sender_address")), Name: value("sender_name"), Password: value("sender_password")}
	settings := domain.CustomerEmail{
		CustomerID: customerID, AutoSend: value("auto_send") == "on", To: strings.TrimSpace(value("to")), CC: strings.TrimSpace(value("cc")),
		Subject: value("subject"), Body: strings.ReplaceAll(value("body"), "\r\n", "\n"),
		AttachPDF: value("attach_pdf") == "on", AttachXML: value("attach_xml") == "on",
	}
	return sender, settings
}

// preview monta a prévia do texto da tela; sem emitente, mostra o texto cru
// (os marcadores à vista) em vez de quebrar a página.
func (s *Server) preview(r *http.Request, customerID int64, senderAddress string, settings domain.CustomerEmail) emailPreview {
	view := emailPreview{From: senderAddress, To: settings.To, Subject: settings.Subject, Body: settings.Body}
	if subject, body, err := s.service.EmailPreview(r.Context(), customerID, settings); err == nil {
		view.Subject, view.Body = subject, body
	}
	if settings.AttachPDF {
		view.Attachments = append(view.Attachments, "danfse-123.pdf")
	}
	if settings.AttachXML {
		view.Attachments = append(view.Attachments, "nfse-123.xml")
	}
	return view
}

// emailCustomer confere que o envio está ligado e lê o tomador da rota.
func (s *Server) emailCustomer(w http.ResponseWriter, r *http.Request) (domain.Customer, bool) {
	if !s.service.EmailEnabled() {
		http.NotFound(w, r)
		return domain.Customer{}, false
	}
	customer, err := s.repo.Customer(r.Context(), formID(r.PathValue("id")))
	if err != nil {
		fail(w, err)
		return domain.Customer{}, false
	}
	return customer, true
}

// customerEmailForm abre a aba com o que está guardado (ou o padrão).
func (s *Server) customerEmailForm(w http.ResponseWriter, r *http.Request) {
	customer, ok := s.emailCustomer(w, r)
	if !ok {
		return
	}
	sender, settings, err := s.service.EmailSettings(r.Context(), customer.ID)
	if err != nil {
		fail(w, err)
		return
	}
	account, _ := s.repo.Account(r.Context())
	view := customerEmailView{
		Customer: customer, Sender: sender, Settings: settings, SenderAddress: sender.Address, SenderName: sender.Name,
		TestTo: account.Email, Placeholders: domain.EmailPlaceholders,
	}
	view.Preview = s.preview(r, customer.ID, view.SenderAddress, settings)
	s.render(w, r, http.StatusOK, "customer_email", "Envio por e-mail", view)
}

// customerEmailSubmit salva (action=save) ou manda o e-mail de teste
// (action=test) com o texto da tela, sem salvar. A senha digitada nunca volta
// para a página: depois de um erro, é preciso digitá-la de novo.
func (s *Server) customerEmailSubmit(w http.ResponseWriter, r *http.Request) {
	customer, ok := s.emailCustomer(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	input, settings := emailForm(r, customer.ID)
	stored, _, err := s.service.EmailSettings(ctx, customer.ID)
	if err != nil {
		fail(w, err)
		return
	}
	view := customerEmailView{
		Customer: customer, Sender: stored, Settings: settings, SenderAddress: input.Address, SenderName: input.Name,
		TestTo: strings.TrimSpace(r.PostFormValue("test_to")), Placeholders: domain.EmailPlaceholders,
	}
	view.Preview = s.preview(r, customer.ID, input.Address, settings)
	if r.PostFormValue("action") == "test" {
		err = s.service.SendTestEmail(ctx, customer.ID, input, settings, view.TestTo)
		if err == nil {
			view.Message = "E-mail de teste enviado para " + view.TestTo + ". Nada foi salvo."
			s.render(w, r, http.StatusOK, "customer_email", "Envio por e-mail", view)
			return
		}
	} else {
		err = s.service.SaveEmailSettings(ctx, customer.ID, input, settings)
		if err == nil {
			http.Redirect(w, r, "/customers/"+strconv.FormatInt(customer.ID, 10)+"/email?flash=Envio+por+e-mail+salvo", http.StatusSeeOther)
			return
		}
	}
	var fields domain.FieldErrors
	if errors.As(err, &fields) {
		view.Errors = fields
		view.Message, view.MessageError = "Confira os campos destacados.", true
	} else {
		log.Printf("envio por e-mail do tomador #%d: %v", customer.ID, err)
		view.Message, view.MessageError = err.Error(), true
	}
	s.render(w, r, http.StatusUnprocessableEntity, "customer_email", "Envio por e-mail", view)
}

// customerEmailPreview devolve só o cartão da prévia, para o email.js
// atualizar enquanto o dono digita (o texto dos marcadores sai do servidor,
// a mesma regra do envio de verdade).
func (s *Server) customerEmailPreview(w http.ResponseWriter, r *http.Request) {
	customer, ok := s.emailCustomer(w, r)
	if !ok {
		return
	}
	input, settings := emailForm(r, customer.ID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.fragments.ExecuteTemplate(w, "email_preview", s.preview(r, customer.ID, input.Address, settings)); err != nil {
		log.Printf("renderizar email_preview: %v", err)
	}
}

// invoiceEmail é o botão "Enviar por e-mail" da nota: manda agora e volta
// ao detalhe com o resultado (o envio fica no histórico da nota).
func (s *Server) invoiceEmail(w http.ResponseWriter, r *http.Request) {
	if !s.service.EmailEnabled() {
		http.NotFound(w, r)
		return
	}
	id := formID(r.PathValue("id"))
	record, err := s.service.SendInvoiceEmail(r.Context(), id, false)
	if errors.Is(err, ports.ErrNotFound) {
		fail(w, err)
		return
	}
	flash := "E-mail enviado para " + record.To
	if err != nil {
		flash = "E-mail não enviado: " + err.Error()
	}
	http.Redirect(w, r, "/invoices?period=90&id="+strconv.FormatInt(id, 10)+"&flash="+url.QueryEscape(flash), http.StatusSeeOther)
}
