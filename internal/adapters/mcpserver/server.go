// Package mcpserver expõe o emissor como servidor MCP (story 4.2), para o
// Claude Code emitir, consultar e cancelar notas conversando. Cada ferramenta
// é uma casca fina sobre o mesmo núcleo (app) da CLI e do painel: a guarda de
// produção, a idempotência e a tradução dos erros da SEFIN vêm de lá.
package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nfse/internal/adapters/danfse"
	"nfse/internal/adapters/nfsexml"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Deps são as peças que o servidor usa; a CLI monta com os adapters reais e o
// teste com fakes.
type Deps struct {
	Service *app.Service
	// Canceller registra o cancelamento na SEFIN.
	Canceller ports.Canceller
	// FetchXML busca o XML da nota na SEFIN quando ele não está guardado.
	FetchXML func(ctx context.Context, invoice domain.Invoice) ([]byte, error)
	// Certificate lê os dados do A1 para o account_status; nil = não informado.
	Certificate func(ctx context.Context) (domain.CertificateInfo, error)
	AppVersion  string
	// OutputDir é onde o download_danfse grava os PDFs.
	OutputDir string
}

// New monta o servidor com todas as ferramentas registradas.
func New(d Deps) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "nfse-digital", Version: d.AppVersion}, nil)
	h := handlers{d}
	mcp.AddTool(server, tool("account_status"), h.accountStatus)
	mcp.AddTool(server, tool("list_customers"), h.listCustomers)
	mcp.AddTool(server, tool("list_templates"), h.listTemplates)
	mcp.AddTool(server, tool("list_invoices"), h.listInvoices)
	mcp.AddTool(server, tool("show_invoice"), h.showInvoice)
	mcp.AddTool(server, tool("preview_invoice"), h.previewInvoice)
	mcp.AddTool(server, tool("emit_invoice"), h.emitInvoice)
	mcp.AddTool(server, tool("cancel_invoice"), h.cancelInvoice)
	mcp.AddTool(server, tool("download_danfse"), h.downloadDANFSe)
	mcp.AddTool(server, tool("list_schedules"), h.listSchedules)
	return server
}

// handlers agrupa as funções de cada ferramenta.
type handlers struct{ Deps }

// noInput é a entrada das ferramentas sem parâmetro.
type noInput struct{}

// AccountStatus é a saída do account_status.
type AccountStatus struct {
	LegalName      string   `json:"legal_name"`
	CNPJ           string   `json:"cnpj"`
	Environment    string   `json:"environment"`
	Series         string   `json:"series"`
	NextDPSNumber  int64    `json:"next_dps_number"`
	CertificateTil string   `json:"certificate_valid_until,omitempty"`
	Missing        []string `json:"missing"`
}

// accountStatus junta emitente, certificado e pendências de cadastro.
func (h handlers) accountStatus(ctx context.Context, req *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, AccountStatus, error) {
	h.touch(ctx, req, "account_status")
	status := AccountStatus{Missing: []string{}}
	issuer, err := h.Service.Repo.Issuer(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		status.Missing = append(status.Missing, "emitente não configurado (nfse settings ou painel → Configurações)")
		return nil, status, nil
	}
	if err != nil {
		return nil, status, err
	}
	status.LegalName, status.CNPJ = issuer.LegalName, domain.FormatDocument(issuer.CNPJ)
	status.Environment, status.Series, status.NextDPSNumber = issuer.Environment.Label(), issuer.Series, issuer.NextDPSNumber
	if err := issuer.Validate(); err != nil {
		status.Missing = append(status.Missing, "cadastro do emitente: "+err.Error())
	}
	if h.Certificate != nil {
		info, err := h.Certificate(ctx)
		if err != nil {
			status.Missing = append(status.Missing, "certificado A1 não abriu: "+err.Error())
		} else {
			status.CertificateTil = info.NotAfter.In(h.Service.Location).Format("02/01/2006")
		}
	}
	return nil, status, nil
}

// SearchInput filtra listagens por texto.
type SearchInput struct {
	Query string `json:"query,omitempty" jsonschema:"texto para filtrar por nome ou documento; vazio lista tudo"`
}

// CustomerOut é um tomador na resposta.
type CustomerOut struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Document string `json:"document"`
	Email    string `json:"email,omitempty"`
}

// CustomerList envolve a lista (a saída estruturada do MCP precisa ser objeto).
type CustomerList struct {
	Customers []CustomerOut `json:"customers"`
}

// listCustomers devolve os tomadores filtrados.
func (h handlers) listCustomers(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, CustomerList, error) {
	h.touch(ctx, req, "list_customers")
	customers, err := h.Service.Repo.Customers(ctx, in.Query)
	out := CustomerList{Customers: []CustomerOut{}}
	for _, c := range customers {
		out.Customers = append(out.Customers, CustomerOut{ID: c.ID, Name: c.Name, Document: domain.FormatDocument(c.Document), Email: c.Email})
	}
	return nil, out, err
}

// TemplateOut é um modelo na resposta.
type TemplateOut struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ServiceCode   string `json:"national_tax_code"`
	DefaultAmount string `json:"default_amount"`
	Description   string `json:"description"`
}

// TemplateList envolve a lista de modelos.
type TemplateList struct {
	Templates []TemplateOut `json:"templates"`
}

// listTemplates devolve os modelos filtrados.
func (h handlers) listTemplates(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, TemplateList, error) {
	h.touch(ctx, req, "list_templates")
	templates, err := h.Service.Repo.Templates(ctx, in.Query)
	out := TemplateList{Templates: []TemplateOut{}}
	for _, t := range templates {
		out.Templates = append(out.Templates, TemplateOut{ID: t.ID, Name: t.Name, ServiceCode: t.NationalTaxCode, DefaultAmount: t.DefaultAmount.FormatBRL(), Description: t.Description})
	}
	return nil, out, err
}

// InvoiceOut é uma nota na resposta, com o erro da SEFIN já traduzido.
type InvoiceOut struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	Environment string `json:"environment"`
	DPS         string `json:"dps"`
	NFSeNumber  string `json:"nfse_number,omitempty"`
	AccessKey   string `json:"access_key,omitempty"`
	Amount      string `json:"amount"`
	Competence  string `json:"competence"`
	CustomerID  int64  `json:"customer_id"`
	ErrorCode   string `json:"error_code,omitempty"`
	Message     string `json:"message,omitempty"`
}

// invoiceOut converte a nota do domínio para a resposta.
func invoiceOut(i domain.Invoice) InvoiceOut {
	out := InvoiceOut{
		ID: i.ID, Status: i.Status.Label(), Environment: i.Environment.Label(), DPS: fmt.Sprintf("%s/%d", i.Series, i.DPSNumber),
		NFSeNumber: i.NFSeNumber, AccessKey: i.AccessKey, Amount: i.Amount.FormatBRL(), CustomerID: i.CustomerID, ErrorCode: i.ErrorCode,
	}
	if !i.Competence.IsZero() {
		out.Competence = i.Competence.Format("01/2006")
	}
	if i.ErrorMessage != "" {
		out.Message = domain.TranslateSefinError(i.ErrorCode, i.ErrorMessage)
	}
	return out
}

// ListInvoicesInput limita a listagem.
type ListInvoicesInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"quantas notas devolver; padrão 20"`
}

// InvoiceList envolve a lista de notas.
type InvoiceList struct {
	Invoices []InvoiceOut `json:"invoices"`
}

// listInvoices devolve as notas mais recentes.
func (h handlers) listInvoices(ctx context.Context, req *mcp.CallToolRequest, in ListInvoicesInput) (*mcp.CallToolResult, InvoiceList, error) {
	h.touch(ctx, req, "list_invoices")
	invoices, err := h.Service.Repo.Invoices(ctx)
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	out := InvoiceList{Invoices: []InvoiceOut{}}
	for i, invoice := range invoices {
		if i == limit {
			break
		}
		out.Invoices = append(out.Invoices, invoiceOut(invoice))
	}
	return nil, out, err
}

// InvoiceIDInput identifica uma nota.
type InvoiceIDInput struct {
	ID int64 `json:"id" jsonschema:"ID da nota no emissor (o número de list_invoices)"`
}

// showInvoice devolve uma nota.
func (h handlers) showInvoice(ctx context.Context, req *mcp.CallToolRequest, in InvoiceIDInput) (*mcp.CallToolResult, InvoiceOut, error) {
	h.touch(ctx, req, "show_invoice")
	invoice, err := h.Service.Repo.Invoice(ctx, in.ID)
	if err != nil {
		return nil, InvoiceOut{}, fmt.Errorf("nota #%d: %w", in.ID, err)
	}
	return nil, invoiceOut(invoice), nil
}

// EmitInput é o pedido de emissão do agente.
type EmitInput struct {
	CustomerID        int64  `json:"customer_id" jsonschema:"ID do tomador (list_customers)"`
	TemplateID        int64  `json:"template_id" jsonschema:"ID do modelo de serviço (list_templates)"`
	Amount            string `json:"amount,omitempty" jsonschema:"valor, como 19000.00 ou 19.000,00; vazio usa o do modelo"`
	Competence        string `json:"competence,omitempty" jsonschema:"data de competência AAAA-MM-DD; vazio usa hoje"`
	Description       string `json:"description,omitempty" jsonschema:"descrição do serviço; vazio usa a do modelo, com {mes} e {ano} resolvidos"`
	IdempotencyKey    string `json:"idempotency_key" jsonschema:"chave estável deste pedido; repetir a mesma chave nunca gera segunda nota"`
	ConfirmProduction bool   `json:"confirm_production,omitempty" jsonschema:"obrigatório true quando o emitente está em produção (nota com valor fiscal)"`
}

// emitInvoice emite pela mesma regra da CLI. Rejeição e falha de comunicação
// voltam como resultado legível; só dado inválido e guarda de produção voltam
// como erro da ferramenta.
// PreviewInput é a entrada do espelho: os mesmos campos da emissão, menos os
// dois que só fazem sentido ao emitir de verdade — não há idempotência a
// garantir nem produção a confirmar em algo que não cria nota.
type PreviewInput struct {
	CustomerID  int64  `json:"customer_id" jsonschema:"ID do tomador (list_customers)"`
	TemplateID  int64  `json:"template_id" jsonschema:"ID do modelo de serviço (list_templates)"`
	Amount      string `json:"amount,omitempty" jsonschema:"valor, como 19000.00 ou 19.000,00; vazio usa o do modelo"`
	Competence  string `json:"competence,omitempty" jsonschema:"data de competência AAAA-MM-DD; vazio usa hoje"`
	Description string `json:"description,omitempty" jsonschema:"descrição do serviço; vazio usa a do modelo, com {mes} e {ano} resolvidos"`
}

// PreviewOut é o espelho da nota: tudo o que será enviado, com o ambiente em
// destaque, antes de existir documento fiscal.
type PreviewOut struct {
	Environment  string `json:"environment"`
	IsProduction bool   `json:"is_production"`
	IssuerName   string `json:"issuer_name"`
	IssuerCNPJ   string `json:"issuer_cnpj"`
	CustomerName string `json:"customer_name"`
	CustomerDoc  string `json:"customer_document"`
	TemplateName string `json:"template_name"`
	ServiceCode  string `json:"service_code"`
	Competence   string `json:"competence"`
	ServiceMonth string `json:"service_month"`
	Amount       string `json:"amount"`
	Description  string `json:"description"`
	NextDPS      int64  `json:"next_dps_number"`
	Notice       string `json:"notice"`
}

// previewInvoice mostra a nota ANTES de emitir. Não reserva número, não grava
// e não transmite: o número da DPS informado é o que SERÁ usado, e continua
// disponível depois desta chamada.
func (h handlers) previewInvoice(ctx context.Context, req *mcp.CallToolRequest, in PreviewInput) (*mcp.CallToolResult, PreviewOut, error) {
	h.touch(ctx, req, "preview_invoice")
	template, err := h.Service.Repo.Template(ctx, in.TemplateID)
	if err != nil {
		return nil, PreviewOut{}, fmt.Errorf("modelo #%d: %w", in.TemplateID, err)
	}
	// Os mesmos padrões do emit_invoice, para o espelho não mostrar uma coisa
	// e a emissão mandar outra.
	now := h.Service.Now().In(h.Service.Location)
	if in.Competence == "" {
		in.Competence = now.Format("2006-01-02")
	}
	if in.Amount == "" {
		in.Amount = template.DefaultAmount.InputValue()
	}
	if in.Description == "" {
		competence, _ := time.ParseInLocation("2006-01-02", in.Competence, h.Service.Location)
		in.Description = domain.RenderDescription(template.Description, competence)
	}
	resolved, err := h.Service.Preview(ctx, app.EmitInput{
		CustomerID: in.CustomerID, TemplateID: in.TemplateID, Amount: in.Amount,
		Competence: in.Competence, Description: in.Description,
	})
	if err != nil {
		return nil, PreviewOut{}, err
	}
	production := resolved.Issuer.Environment == domain.EnvProduction
	notice := "espelho: nada foi emitido nem gravado. Mostre ao dono e só chame emit_invoice depois do sim dele."
	if production {
		notice = "🛑 PRODUÇÃO: a emissão gera nota fiscal REAL, que só se desfaz por cancelamento. " + notice
	}
	out := PreviewOut{
		Environment:  resolved.Issuer.Environment.Label(),
		IsProduction: production,
		IssuerName:   resolved.Issuer.LegalName,
		IssuerCNPJ:   resolved.Issuer.CNPJ,
		CustomerName: resolved.Customer.Name,
		CustomerDoc:  resolved.Customer.Document,
		TemplateName: resolved.Template.Name,
		ServiceCode:  resolved.Template.NationalTaxCode,
		Competence:   resolved.Competence.Format("2006-01-02"),
		ServiceMonth: resolved.Competence.AddDate(0, -1, 0).Format("01/2006"),
		Amount:       resolved.Amount.FormatBRL(),
		Description:  in.Description,
		Notice:       notice,
	}
	// O número que SERÁ usado, sem reservá-lo.
	if issuer, err := h.Service.Repo.Issuer(ctx); err == nil {
		out.NextDPS = issuer.NextDPSNumber
	}
	return nil, out, nil
}

func (h handlers) emitInvoice(ctx context.Context, req *mcp.CallToolRequest, in EmitInput) (*mcp.CallToolResult, InvoiceOut, error) {
	h.touch(ctx, req, "emit_invoice")
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, InvoiceOut{}, errors.New("idempotency_key é obrigatória: gere uma chave estável para este pedido e repita a mesma se precisar tentar de novo")
	}
	template, err := h.Service.Repo.Template(ctx, in.TemplateID)
	if err != nil {
		return nil, InvoiceOut{}, fmt.Errorf("modelo #%d: %w", in.TemplateID, err)
	}
	now := h.Service.Now().In(h.Service.Location)
	if in.Competence == "" {
		in.Competence = now.Format("2006-01-02")
	}
	if in.Amount == "" {
		in.Amount = template.DefaultAmount.InputValue()
	}
	if in.Description == "" {
		competence, _ := time.ParseInLocation("2006-01-02", in.Competence, h.Service.Location)
		in.Description = domain.RenderDescription(template.Description, competence)
	}
	invoice, err := h.Service.Emit(ctx, app.EmitInput{
		CustomerID: in.CustomerID, TemplateID: in.TemplateID, Amount: in.Amount, Competence: in.Competence,
		Description: in.Description, IdempotencyKey: in.IdempotencyKey, ConfirmProduction: in.ConfirmProduction,
	})
	if invoice.ID == 0 {
		return nil, InvoiceOut{}, err
	}
	out := invoiceOut(invoice)
	if err != nil && invoice.Status == domain.StatusPending {
		out.Message = "sem resposta definitiva da SEFIN (" + err.Error() + "): consulte com show_invoice antes de qualquer nova emissão"
	}
	return nil, out, nil
}

// CancelInput é o pedido de cancelamento do agente.
type CancelInput struct {
	ID                int64  `json:"id" jsonschema:"ID da nota autorizada"`
	Reason            string `json:"reason" jsonschema:"motivo: erro_emissao, servico_nao_prestado ou outros"`
	Description       string `json:"description" jsonschema:"justificativa de 15 a 255 caracteres"`
	ConfirmProduction bool   `json:"confirm_production,omitempty" jsonschema:"obrigatório true quando a nota é de produção"`
}

// cancelInvoice registra o cancelamento; recusa da SEFIN volta legível.
func (h handlers) cancelInvoice(ctx context.Context, req *mcp.CallToolRequest, in CancelInput) (*mcp.CallToolResult, InvoiceOut, error) {
	h.touch(ctx, req, "cancel_invoice")
	report, err := h.Service.Cancel(ctx, h.Canceller, in.ID, in.Reason, in.Description, h.AppVersion, in.ConfirmProduction)
	if err != nil {
		return nil, InvoiceOut{}, err
	}
	out := invoiceOut(report.Invoice)
	if !report.Registered {
		out.Message = "a SEFIN recusou o cancelamento: " + out.Message
	}
	return nil, out, nil
}

// DANFSeOut entrega o PDF do DANFSe. O conteúdo em base64 é o produto da
// ferramenta: o cliente MCP costuma rodar em outra máquina que o servidor, e lá
// um caminho de arquivo não alcança nada. Path é só a cópia em disco de quem
// roda o servidor localmente, e por isso é opcional.
type DANFSeOut struct {
	FileName      string `json:"file_name"`
	ContentBase64 string `json:"content_base64"`
	Bytes         int    `json:"bytes"`
	Path          string `json:"path,omitempty"`
	// O XML autorizado pela SEFIN é o documento fiscal em si — o PDF é só a
	// representação dele. Vai junto porque o arquivamento guarda os dois.
	XMLFileName string `json:"xml_file_name"`
	XMLBase64   string `json:"xml_base64"`
	XMLBytes    int    `json:"xml_bytes"`
}

// downloadDANFSe gera o PDF localmente (a API do ADN está suspensa, NT-008).
func (h handlers) downloadDANFSe(ctx context.Context, req *mcp.CallToolRequest, in InvoiceIDInput) (*mcp.CallToolResult, DANFSeOut, error) {
	h.touch(ctx, req, "download_danfse")
	invoice, err := h.Service.Repo.Invoice(ctx, in.ID)
	if err != nil {
		return nil, DANFSeOut{}, fmt.Errorf("nota #%d: %w", in.ID, err)
	}
	if invoice.AccessKey == "" || (invoice.Status != domain.StatusAuthorized && invoice.Status != domain.StatusCancelled) {
		return nil, DANFSeOut{}, fmt.Errorf("a nota está %s: só nota autorizada ou cancelada tem DANFSe", invoice.Status.Label())
	}
	if len(invoice.XML) == 0 {
		if invoice.XML, err = h.FetchXML(ctx, invoice); err != nil {
			return nil, DANFSeOut{}, err
		}
		if err := h.Service.Repo.SaveInvoice(ctx, &invoice); err != nil {
			return nil, DANFSeOut{}, err
		}
	}
	data, err := nfsexml.ParseDANFSe(invoice.XML)
	if err != nil {
		return nil, DANFSeOut{}, err
	}
	pdf, err := danfse.Render(data)
	if err != nil {
		return nil, DANFSeOut{}, err
	}
	name := danfse.FileName(data)
	out := DANFSeOut{
		FileName:      name,
		ContentBase64: base64.StdEncoding.EncodeToString(pdf),
		Bytes:         len(pdf),
		XMLFileName:   strings.TrimSuffix(name, ".pdf") + ".xml",
		XMLBase64:     base64.StdEncoding.EncodeToString(invoice.XML),
		XMLBytes:      len(invoice.XML),
	}
	// A cópia em disco é conveniência de quem roda o servidor na própria
	// máquina. Num container o diretório pode não existir, e isso não pode
	// impedir a entrega do PDF: falhou, segue sem Path.
	if path := filepath.Join(h.OutputDir, name); os.WriteFile(path, pdf, 0o600) == nil {
		out.Path = path
	}
	return nil, out, nil
}

// ScheduleOut é um agendamento com as próximas emissões.
type ScheduleOut struct {
	ID         int64    `json:"id"`
	Active     bool     `json:"active"`
	Day        int      `json:"day"`
	Amount     string   `json:"amount"`
	CustomerID int64    `json:"customer_id"`
	TemplateID int64    `json:"template_id"`
	NextRuns   []string `json:"next_runs"`
}

// ScheduleList envolve a lista de agendamentos.
type ScheduleList struct {
	Schedules []ScheduleOut `json:"schedules"`
}

// listSchedules devolve os agendamentos com as próximas quatro emissões.
func (h handlers) listSchedules(ctx context.Context, req *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, ScheduleList, error) {
	h.touch(ctx, req, "list_schedules")
	schedules, err := h.Service.Repo.Schedules(ctx)
	out := ScheduleList{Schedules: []ScheduleOut{}}
	for _, s := range schedules {
		item := ScheduleOut{ID: s.ID, Active: s.Active, Day: s.Day, Amount: s.Amount.FormatBRL(), CustomerID: s.CustomerID, TemplateID: s.TemplateID, NextRuns: []string{}}
		for _, run := range s.NextRuns(h.Service.Now(), 4, h.Service.Location) {
			item.NextRuns = append(item.NextRuns, run.EmitAt.Format("02/01/2006 15:04")+" · competência "+run.Competence.Format("01/2006"))
		}
		out.Schedules = append(out.Schedules, item)
	}
	return nil, out, err
}

// touch grava qual agente chamou qual ferramenta e quando, para o "Testar
// conexão" da tela de Integrações. Falha ao gravar não derruba a ferramenta:
// é diagnóstico, não parte da operação.
func (h handlers) touch(ctx context.Context, req *mcp.CallToolRequest, tool string) {
	client := "cliente MCP"
	if req != nil && req.Session != nil {
		if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil && params.ClientInfo.Name != "" {
			client = params.ClientInfo.Name
		}
	}
	now := time.Now
	if h.Service.Now != nil {
		now = h.Service.Now
	}
	_ = h.Service.Repo.RecordAgentContact(ctx, domain.AgentContact{Client: client, Tool: tool, At: now()})
}
