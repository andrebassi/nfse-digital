package web

import (
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// invoiceRow junta a nota ao tomador, que a lista e o detalhe mostram juntos.
type invoiceRow struct {
	domain.Invoice
	Customer domain.Customer
}

// statusTab é uma aba de filtro por estado, com a contagem do período.
type statusTab struct {
	Label  string
	Count  int
	Active bool
	URL    string
}

// invoicesView são os dados da tela de notas.
type invoicesView struct {
	Period   domain.Period
	Periods  []domain.Period
	Status   domain.InvoiceStatus
	Tabs     []statusTab
	Summary  domain.Summary
	Rows     []invoiceRow
	Selected *invoiceRow
	Template domain.Template
	// DANFSe mostra o botão de download (painel ligado à SEFIN real).
	DANFSe bool
	// Cancel mostra o formulário de cancelamento (painel ligado à SEFIN real).
	Cancel bool
	// Email mostra o botão "Enviar por e-mail"; Emails são os envios da nota aberta.
	Email  bool
	Emails []domain.InvoiceEmail
}

// invoices mostra a lista com período, abas, indicadores e o detalhe em ?id=.
func (s *Server) invoices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	period := domain.ParsePeriod(query.Get("period"))
	status := domain.InvoiceStatus(query.Get("status"))

	all, err := s.repo.Invoices(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	inPeriod := domain.FilterByPeriod(all, period, s.now())

	view := invoicesView{Period: period, Periods: domain.Periods(), Status: status, Summary: domain.Summarize(inPeriod), DANFSe: s.danfsePDF != nil, Cancel: s.canceller != nil, Email: s.service.EmailEnabled()}
	tabs := []struct {
		label  string
		status domain.InvoiceStatus
	}{{"Todas", ""}, {"Autorizadas", domain.StatusAuthorized}, {"Canceladas", domain.StatusCancelled}, {"Rejeitadas", domain.StatusRejected}}
	for _, tab := range tabs {
		link := url.Values{"period": {strconv.Itoa(int(period))}}
		if tab.status != "" {
			link.Set("status", string(tab.status))
		}
		view.Tabs = append(view.Tabs, statusTab{
			Label: tab.label, Count: len(domain.FilterByStatus(inPeriod, tab.status)),
			Active: tab.status == status, URL: "/invoices?" + link.Encode(),
		})
	}
	for _, invoice := range domain.FilterByStatus(inPeriod, status) {
		customer, _ := s.repo.Customer(ctx, invoice.CustomerID)
		view.Rows = append(view.Rows, invoiceRow{Invoice: invoice, Customer: customer})
	}

	if id := formID(query.Get("id")); id > 0 {
		invoice, err := s.repo.Invoice(ctx, id)
		if err != nil {
			fail(w, err)
			return
		}
		customer, _ := s.repo.Customer(ctx, invoice.CustomerID)
		view.Template, _ = s.repo.Template(ctx, invoice.TemplateID)
		view.Selected = &invoiceRow{Invoice: invoice, Customer: customer}
		if view.Email {
			if view.Emails, err = s.repo.InvoiceEmails(ctx, invoice.ID); err != nil {
				fail(w, err)
				return
			}
		}
	}
	s.render(w, r, http.StatusOK, "invoices", "Notas", view)
}

// emitView são os dados da tela de emissão.
type emitView struct {
	Query       string
	Customers   []domain.Customer
	Customer    *domain.Customer
	Templates   []domain.Template
	TemplateID  int64
	Template    *domain.Template
	Amount      string
	Competence  string
	Description string
	Errors      domain.FieldErrors
	// Production liga a caixa de confirmação: o emitente está em produção.
	Production bool
}

// emitForm mostra o passo 1 (tomador) e, com tomador escolhido, o passo 2.
func (s *Server) emitForm(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	view := emitView{Query: query.Get("q"), Competence: s.now().In(s.location).Format("2006-01-02")}
	s.fillEmitView(w, r, &view, formID(query.Get("customer")), 0)
}

// emitSubmit trata os dois botões do formulário: "Usar modelo" recarrega valor
// e descrição do modelo escolhido; "Emitir nota" chama o caso de uso.
func (s *Server) emitSubmit(w http.ResponseWriter, r *http.Request) {
	view := emitView{
		Amount: r.PostFormValue("amount"), Competence: r.PostFormValue("competence"),
		Description: r.PostFormValue("description"),
	}
	customerID, templateID := formID(r.PostFormValue("customer_id")), formID(r.PostFormValue("template_id"))
	if r.PostFormValue("action") == "load-template" {
		view.Amount, view.Description = "", ""
		s.fillEmitView(w, r, &view, customerID, templateID)
		return
	}

	invoice, err := s.service.Emit(r.Context(), app.EmitInput{
		CustomerID: customerID, TemplateID: templateID, Amount: view.Amount,
		Competence: view.Competence, Description: view.Description,
		ConfirmProduction: r.PostFormValue("confirm_production") == "yes",
	})
	var fields domain.FieldErrors
	if errors.Is(err, app.ErrProductionNotConfirmed) {
		fields = domain.FieldErrors{"confirm_production": "o emitente está em produção: marque a confirmação para emitir nota com valor fiscal"}
		err = fields
	}
	switch {
	case errors.As(err, &fields):
		view.Errors = fields
		s.fillEmitView(w, r, &view, customerID, templateID)
	case err != nil && invoice.ID == 0:
		fail(w, err)
	default:
		// Mesmo com falha de comunicação a nota existe (pendente): mostrar o detalhe.
		http.Redirect(w, r, "/invoices?period=90&id="+strconv.FormatInt(invoice.ID, 10), http.StatusSeeOther)
	}
}

// fillEmitView completa a tela com tomadores, modelos e os valores do modelo
// escolhido, e renderiza. Status 422 quando há erro de campo.
func (s *Server) fillEmitView(w http.ResponseWriter, r *http.Request, view *emitView, customerID, templateID int64) {
	ctx := r.Context()
	var err error
	if view.Customers, err = s.repo.Customers(ctx, view.Query); err != nil {
		fail(w, err)
		return
	}
	if customerID > 0 {
		customer, err := s.repo.Customer(ctx, customerID)
		if err != nil {
			fail(w, err)
			return
		}
		view.Customer = &customer
	}
	view.Templates, _ = s.repo.Templates(ctx, "")
	if issuer, err := s.repo.Issuer(ctx); err == nil {
		view.Production = issuer.Environment == domain.EnvProduction
	}
	// Sem modelo escolhido, sugere o modelo ligado ao tomador.
	if templateID == 0 && view.Customer != nil {
		for _, template := range view.Templates {
			if template.CustomerID == view.Customer.ID {
				templateID = template.ID
				break
			}
		}
	}
	if templateID > 0 {
		if template, err := s.repo.Template(ctx, templateID); err == nil {
			view.Template, view.TemplateID = &template, template.ID
			competence, parseErr := time.ParseInLocation("2006-01-02", view.Competence, s.location)
			if parseErr != nil {
				competence = s.now().In(s.location)
			}
			if view.Amount == "" && template.DefaultAmount > 0 {
				view.Amount = template.DefaultAmount.InputValue()
			}
			if view.Description == "" {
				view.Description = domain.RenderDescription(template.Description, competence)
			}
		}
	}
	status := http.StatusOK
	if len(view.Errors) > 0 {
		status = http.StatusUnprocessableEntity
	}
	s.render(w, r, status, "emit", "Emitir nota", view)
}

// scheduleRow junta o agendamento ao modelo, ao tomador e à próxima emissão.
type scheduleRow struct {
	domain.Schedule
	Template domain.Template
	Customer domain.Customer
	Next     domain.ScheduledRun
}

// schedules lista os agendamentos com a próxima emissão de cada um.
func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.repo.Schedules(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	rows := []scheduleRow{}
	for _, schedule := range list {
		template, _ := s.repo.Template(ctx, schedule.TemplateID)
		customer, _ := s.repo.Customer(ctx, schedule.CustomerID)
		rows = append(rows, scheduleRow{Schedule: schedule, Template: template, Customer: customer, Next: schedule.NextRuns(s.now(), 1, s.location)[0]})
	}
	s.render(w, r, http.StatusOK, "schedules", "Agendamentos", rows)
}

// scheduleView são os dados do formulário de agendamento com a pré-visualização.
type scheduleView struct {
	Templates []domain.Template
	Customers []domain.Customer
	Days      []int
	Form      domain.Schedule
	Amount    string
	Runs      []domain.ScheduledRun
	Example   string
	Errors    domain.FieldErrors
}

// scheduleForm abre o formulário com o primeiro modelo, dia 28 e mês anterior,
// que é como o dono fatura.
func (s *Server) scheduleForm(w http.ResponseWriter, r *http.Request) {
	view := scheduleView{Form: domain.Schedule{Day: 28, Competence: domain.CompetencePreviousMonth, NotifyEve: true}}
	templates, _ := s.repo.Templates(r.Context(), "")
	if len(templates) > 0 {
		view.Form.TemplateID = templates[0].ID
		view.Form.CustomerID = templates[0].CustomerID
		view.Form.Description = templates[0].Description
		view.Amount = templates[0].DefaultAmount.InputValue()
	}
	s.fillScheduleView(w, r, &view, http.StatusOK)
}

// scheduleSubmit pré-visualiza (botão "Pré-visualizar") ou grava o agendamento.
// Nada é emitido no protótipo.
func (s *Server) scheduleSubmit(w http.ResponseWriter, r *http.Request) {
	amount, amountErr := domain.ParseBRL(r.PostFormValue("amount"))
	day, _ := strconv.Atoi(r.PostFormValue("day"))
	view := scheduleView{
		Amount: r.PostFormValue("amount"),
		Form: domain.Schedule{
			TemplateID: formID(r.PostFormValue("template_id")), CustomerID: formID(r.PostFormValue("customer_id")),
			Amount: amount, Day: day, Competence: domain.CompetenceMode(r.PostFormValue("competence")),
			Description: strings.TrimSpace(r.PostFormValue("description")),
			NotifyEve:   r.PostFormValue("notify_eve") == "on", SendToCustomer: r.PostFormValue("send_to_customer") == "on",
			Active: true,
		},
	}
	if r.PostFormValue("action") == "preview" {
		s.fillScheduleView(w, r, &view, http.StatusOK)
		return
	}
	err := view.Form.Validate()
	if amountErr != nil {
		err = errors.Join(err, domain.FieldErrors{"amount": "valor inválido"})
	}
	if err != nil {
		view.Errors = collectFieldErrors(err)
		s.fillScheduleView(w, r, &view, http.StatusUnprocessableEntity)
		return
	}
	if err := s.repo.SaveSchedule(r.Context(), &view.Form); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/schedules?flash=Agendamento+salvo", http.StatusSeeOther)
}

// collectFieldErrors junta FieldErrors que vieram embrulhados num errors.Join.
func collectFieldErrors(err error) domain.FieldErrors {
	result := domain.FieldErrors{}
	var walk func(error)
	walk = func(e error) {
		if fields, ok := e.(domain.FieldErrors); ok {
			maps.Copy(result, fields)
		}
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range joined.Unwrap() {
				walk(inner)
			}
		}
	}
	walk(err)
	return result
}

// fillScheduleView calcula as próximas 4 emissões e o exemplo de descrição.
func (s *Server) fillScheduleView(w http.ResponseWriter, r *http.Request, view *scheduleView, status int) {
	ctx := r.Context()
	view.Templates, _ = s.repo.Templates(ctx, "")
	view.Customers, _ = s.repo.Customers(ctx, "")
	for day := 1; day <= 31; day++ {
		view.Days = append(view.Days, day)
	}
	if view.Form.Day >= 1 && view.Form.Day <= 31 {
		view.Runs = view.Form.NextRuns(s.now(), 4, s.location)
		view.Example = domain.RenderDescription(view.Form.Description, view.Runs[0].Competence)
	}
	s.render(w, r, status, "schedule_form", "Novo agendamento", view)
}

// customers lista os tomadores com busca.
func (s *Server) customers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	list, err := s.repo.Customers(r.Context(), query)
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, r, http.StatusOK, "customers", "Tomadores", map[string]any{"Query": query, "Customers": list})
}

// customerView são os dados do formulário de tomador.
type customerView struct {
	Customer domain.Customer
	Errors   domain.FieldErrors
	// Email mostra a aba "Envio por e-mail" (painel com envio ligado).
	Email bool
}

// customerForm abre o formulário vazio (/customers/new) ou de edição (/customers/{id}).
func (s *Server) customerForm(w http.ResponseWriter, r *http.Request) {
	view := customerView{Email: s.service.EmailEnabled()}
	if id := r.PathValue("id"); id != "" {
		customer, err := s.repo.Customer(r.Context(), formID(id))
		if err != nil {
			fail(w, err)
			return
		}
		view.Customer = customer
	}
	s.render(w, r, http.StatusOK, "customer_form", "Tomador", view)
}

// customerSubmit valida e grava o tomador; o documento é guardado só com dígitos.
func (s *Server) customerSubmit(w http.ResponseWriter, r *http.Request) {
	customer := domain.Customer{
		ID: formID(r.PostFormValue("id")), Foreign: r.PostFormValue("foreign") == "on",
		Document: strings.TrimSpace(r.PostFormValue("document")), Name: strings.TrimSpace(r.PostFormValue("name")),
		Email: strings.TrimSpace(r.PostFormValue("email")), CityCode: strings.TrimSpace(r.PostFormValue("city_code")),
		CityName: strings.TrimSpace(r.PostFormValue("city_name")), ZIP: strings.TrimSpace(r.PostFormValue("zip")),
		District: strings.TrimSpace(r.PostFormValue("district")), Street: strings.TrimSpace(r.PostFormValue("street")),
		Number: strings.TrimSpace(r.PostFormValue("number")), Complement: strings.TrimSpace(r.PostFormValue("complement")),
		Country: strings.TrimSpace(r.PostFormValue("country")),
	}
	if err := customer.Validate(); err != nil {
		s.render(w, r, http.StatusUnprocessableEntity, "customer_form", "Tomador", customerView{Customer: customer, Errors: err.(domain.FieldErrors)})
		return
	}
	if document, err := domain.ParseDocument(customer.Document); err == nil {
		customer.Document = document.Digits()
	}
	err := s.repo.SaveCustomer(r.Context(), &customer)
	// CPF/CNPJ é um tomador só por conta (índice único, migração 012): o
	// repetido volta ao formulário, em vez de 500 ou de um tomador em dobro.
	if errors.Is(err, ports.ErrConflict) {
		s.render(w, r, http.StatusUnprocessableEntity, "customer_form", "Tomador", customerView{Customer: customer, Errors: domain.FieldErrors{"document": "já existe um tomador com este CPF/CNPJ"}})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/customers?flash=Tomador+salvo", http.StatusSeeOther)
}

// templateRow junta o modelo ao tomador padrão, para a lista.
type templateRow struct {
	domain.Template
	Customer domain.Customer
}

// templates lista os modelos com busca.
func (s *Server) templates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query().Get("q")
	list, err := s.repo.Templates(ctx, query)
	if err != nil {
		fail(w, err)
		return
	}
	rows := []templateRow{}
	for _, template := range list {
		customer, _ := s.repo.Customer(ctx, template.CustomerID)
		rows = append(rows, templateRow{Template: template, Customer: customer})
	}
	s.render(w, r, http.StatusOK, "templates", "Modelos", map[string]any{"Query": query, "Templates": rows})
}

// templateView são os dados do formulário de modelo.
type templateView struct {
	Template  domain.Template
	Amount    string
	Customers []domain.Customer
	Errors    domain.FieldErrors
}

// templateForm abre o formulário vazio ou de edição; modelo novo já vem com o
// município do emitente, que é onde o serviço costuma ser prestado.
func (s *Server) templateForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	issuer, _ := s.repo.Issuer(ctx)
	view := templateView{Template: domain.Template{ServiceCityCode: issuer.CityCode, ServiceCityName: issuer.CityName}}
	if id := r.PathValue("id"); id != "" {
		template, err := s.repo.Template(ctx, formID(id))
		if err != nil {
			fail(w, err)
			return
		}
		view.Template = template
		view.Amount = template.DefaultAmount.InputValue()
	}
	view.Customers, _ = s.repo.Customers(ctx, "")
	s.render(w, r, http.StatusOK, "template_form", "Modelo de serviço", view)
}

// templateSubmit valida e grava o modelo. Valor padrão vazio vale zero.
func (s *Server) templateSubmit(w http.ResponseWriter, r *http.Request) {
	view := templateView{
		Amount: strings.TrimSpace(r.PostFormValue("default_amount")),
		Template: domain.Template{
			ID: formID(r.PostFormValue("id")), Name: strings.TrimSpace(r.PostFormValue("name")),
			Description: strings.TrimSpace(r.PostFormValue("description")), NationalTaxCode: digitsOnly(r.PostFormValue("national_tax_code")),
			MunicipalTaxCode: strings.TrimSpace(r.PostFormValue("municipal_tax_code")), NBS: digitsOnly(r.PostFormValue("nbs")),
			ServiceCityCode: strings.TrimSpace(r.PostFormValue("service_city_code")), ServiceCityName: strings.TrimSpace(r.PostFormValue("service_city_name")),
			SimplesTaxRate: strings.TrimSpace(r.PostFormValue("simples_tax_rate")), ISSQNTaxation: strings.TrimSpace(r.PostFormValue("issqn_taxation")),
			ISSQNRetention: strings.TrimSpace(r.PostFormValue("issqn_retention")), Export: r.PostFormValue("export") == "on",
			CustomerID:      formID(r.PostFormValue("customer_id")),
			IBSCBSOperation: digitsOnly(r.PostFormValue("ibscbs_operation")), IBSCBSCST: digitsOnly(r.PostFormValue("ibscbs_cst")),
			IBSCBSClass: digitsOnly(r.PostFormValue("ibscbs_class")),
		},
	}
	errs := domain.FieldErrors{}
	if view.Amount != "" {
		amount, err := domain.ParseBRL(view.Amount)
		if err != nil {
			errs["default_amount"] = "valor inválido, use o formato 19.000,00"
		}
		view.Template.DefaultAmount = amount
	}
	if err := view.Template.Validate(); err != nil {
		maps.Copy(errs, err.(domain.FieldErrors))
	}
	if len(errs) > 0 {
		view.Errors = errs
		view.Customers, _ = s.repo.Customers(r.Context(), "")
		s.render(w, r, http.StatusUnprocessableEntity, "template_form", "Modelo de serviço", view)
		return
	}
	if err := s.repo.SaveTemplate(r.Context(), &view.Template); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/templates?flash=Modelo+salvo", http.StatusSeeOther)
}

// digitsOnly tira pontos e espaços de códigos digitados com máscara ("01.07.01").
func digitsOnly(text string) string {
	return strings.Map(func(char rune) rune {
		if char >= '0' && char <= '9' {
			return char
		}
		if char == '.' || char == ' ' || char == '-' {
			return -1
		}
		return char
	}, strings.TrimSpace(text))
}

// settingsView são os dados da tela de configurações.
type settingsView struct {
	Issuer domain.Issuer
	Errors domain.FieldErrors
	// Certificate é o estado público do A1; CertificateEnabled liga o envio.
	Certificate        domain.CertificateStatus
	CertificateEnabled bool
	// CertificateStorage diz onde o certificado fica guardado (CertificateOps.Storage).
	CertificateStorage string
	CertificateError   string
	// Section é a aba (issuer, certificate, security); Sections, as abas.
	Section  string
	Sections []agentTab
	// Passkeys e PasskeyHost alimentam a aba Segurança.
	Passkeys []domain.Passkey
	// PasskeyLocalURL vem preenchido quando a página foi aberta por IP:
	// aponta para a mesma aba em localhost, onde a passkey funciona.
	PasskeyLocalURL string
}

// settingsSections são as abas de Configurações, na ordem da tela: o
// certificado primeiro, porque sem ele nada funciona (e o menu abre nele).
var settingsSections = []agentTab{{Key: "certificate", Label: "Certificado"}, {Key: "issuer", Label: "Emitente"}, {Key: "security", Label: "Segurança"}}

// settingsURL é a rota de cada aba de Configurações.
func settingsURL(key string) string {
	if key == "issuer" {
		return "/settings"
	}
	return "/settings/" + key
}

// settingsSection abre a aba de Configurações pela rota.
func (s *Server) settingsSection(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		issuer, err := s.repo.Issuer(r.Context())
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			fail(w, err)
			return
		}
		s.fillSettings(w, r, http.StatusOK, settingsView{Issuer: issuer, Section: section})
	}
}

// settings mostra o emitente e o certificado.
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	issuer, err := s.repo.Issuer(r.Context())
	switch {
	case errors.Is(err, ports.ErrNotFound):
		// Banco novo: o formulário abre vazio, em homologação, série 1, DPS 1.
		issuer = domain.Issuer{Environment: domain.EnvHomologation, Series: "1", NextDPSNumber: 1}
	case err != nil:
		fail(w, err)
		return
	}
	s.fillSettings(w, r, http.StatusOK, settingsView{Issuer: issuer})
}

// settingsSubmit grava o emitente. O campo Ambiente volta para homologação na
// hora, mas NUNCA liga produção sozinho: pedir produção grava o resto e manda
// para a confirmação do CNPJ (story 4.1), a mesma do seletor do cabeçalho —
// um select trocado sem querer não pode virar nota com valor fiscal. Campo
// ausente mantém o ambiente atual; conta nova nasce em homologação.
func (s *Server) settingsSubmit(w http.ResponseWriter, r *http.Request) {
	environment := domain.EnvHomologation
	if current, err := s.repo.Issuer(r.Context()); err == nil && current.Environment != "" {
		environment = current.Environment
	}
	wantsProduction := false
	switch domain.Environment(r.PostFormValue("environment")) {
	case domain.EnvHomologation:
		environment = domain.EnvHomologation
	case domain.EnvProduction:
		wantsProduction = environment != domain.EnvProduction
	}
	next, _ := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("next_dps_number")), 10, 64)
	issuer := domain.Issuer{
		LegalName: strings.TrimSpace(r.PostFormValue("legal_name")), CNPJ: digitsOnly(strings.ReplaceAll(r.PostFormValue("cnpj"), "/", "")),
		MunicipalRegistration: strings.TrimSpace(r.PostFormValue("municipal_registration")),
		CityCode:              strings.TrimSpace(r.PostFormValue("city_code")), CityName: strings.TrimSpace(r.PostFormValue("city_name")),
		SimplesOption: r.PostFormValue("simples_option"), SimplesRegime: r.PostFormValue("simples_regime"),
		SpecialRegime: r.PostFormValue("special_regime"), Environment: environment,
		Series: strings.TrimSpace(r.PostFormValue("series")), NextDPSNumber: next,
	}
	// Percentual ilegível vira erro no campo; ele não pode virar zero calado,
	// que travaria a próxima emissão de ME/EPP longe daqui.
	rate, rateErr := domain.ParsePercent(r.PostFormValue("simples_tax_rate"))
	issuer.SimplesTaxRate = rate
	errs := domain.FieldErrors{}
	if err := issuer.Validate(); err != nil {
		errs = err.(domain.FieldErrors)
	}
	if rateErr != nil {
		errs["simples_tax_rate"] = rateErr.Error()
	}
	if len(errs) > 0 {
		s.fillSettings(w, r, http.StatusUnprocessableEntity, settingsView{Issuer: issuer, Errors: errs})
		return
	}
	if err := s.repo.SaveIssuer(r.Context(), issuer); err != nil {
		fail(w, err)
		return
	}
	if wantsProduction {
		http.Redirect(w, r, "/environment/production", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash=Configurações+salvas", http.StatusSeeOther)
}

// cancelSubmit registra na SEFIN o cancelamento de uma nota autorizada. A guarda
// de produção é a mesma da emissão (story 4.1): nota de produção só cancela com
// a caixa de confirmação marcada, porque o evento é irreversível. Erro de campo
// volta como aviso no detalhe; recusa da SEFIN fica gravada na nota e aparece no
// quadro "Retorno da SEFIN" do detalhe.
func (s *Server) cancelSubmit(w http.ResponseWriter, r *http.Request) {
	id := formID(r.PathValue("id"))
	detail := "/invoices?period=90&id=" + strconv.FormatInt(id, 10)
	if s.canceller == nil {
		http.NotFound(w, r)
		return
	}
	report, err := s.service.Cancel(r.Context(), s.canceller, id, r.PostFormValue("reason"),
		strings.TrimSpace(r.PostFormValue("description")), s.appVersion, r.PostFormValue("confirm_production") == "yes")
	var fields domain.FieldErrors
	switch {
	case errors.Is(err, app.ErrProductionNotConfirmed):
		http.Redirect(w, r, detail+"&flash="+url.QueryEscape("Nota de produção: marque a confirmação para cancelar na SEFIN"), http.StatusSeeOther)
	case errors.As(err, &fields):
		http.Redirect(w, r, detail+"&flash="+url.QueryEscape("Cancelamento não enviado: "+fields.Error()), http.StatusSeeOther)
	case errors.Is(err, ports.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		fail(w, err)
	case !report.Registered:
		http.Redirect(w, r, detail+"&flash="+url.QueryEscape("A SEFIN recusou o cancelamento; o motivo está no detalhe da nota"), http.StatusSeeOther)
	default:
		http.Redirect(w, r, detail+"&flash="+url.QueryEscape("Nota cancelada na SEFIN"), http.StatusSeeOther)
	}
}

// danfse entrega o PDF do DANFSe da nota como download. Só existe com o painel
// ligado à SEFIN real; nota sem chave (rejeitada, pendente) responde 409.
func (s *Server) danfse(w http.ResponseWriter, r *http.Request) {
	if s.danfsePDF == nil {
		http.NotFound(w, r)
		return
	}
	invoice, err := s.repo.Invoice(r.Context(), formID(r.PathValue("id")))
	if err != nil {
		fail(w, err)
		return
	}
	if invoice.AccessKey == "" || (invoice.Status != domain.StatusAuthorized && invoice.Status != domain.StatusCancelled) {
		http.Error(w, "só nota autorizada ou cancelada tem DANFSe", http.StatusConflict)
		return
	}
	pdf, name, err := s.danfsePDF(r.Context(), invoice)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(pdf)
}
