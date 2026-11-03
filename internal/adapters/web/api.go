package web

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// openAPISpec é o contrato da API REST (OpenAPI 3.1), escrito à mão: é o que
// a página de documentação (Scalar) lê e o que o integrador importa. Um teste
// confere que toda rota de /api/v1 está nele e que ele não cita rota que não existe.
//
//go:embed api/openapi.json
var openAPISpec []byte

// llmsTemplate é o llms.txt da API: o guia que o agente do integrador (Claude,
// Cursor) lê para integrar sozinho. As URLs saem absolutas, com o endereço
// deste painel.
//
//go:embed api/llms.txt
var llmsTemplate string

// apiDocsPage é a página da documentação. Fica fora do layout do painel: é
// pública (o integrador lê antes de ter conta) e carrega o Scalar.
//
//go:embed api/docs.html
var apiDocsPage []byte

// apiMaxBody limita o corpo de uma chamada: pedidos da API são pequenos, e o
// limite impede um token vazado de encher a memória do painel.
const apiMaxBody = 64 << 10

// apiDefaultLimit e apiMaxLimit são o tamanho padrão e o teto da listagem de
// notas: 20 cabe numa tela; 100 evita uma resposta de megabytes por engano.
const (
	apiDefaultLimit = 20
	apiMaxLimit     = 100
)

// apiDocsCSP é a política da página de documentação. O Scalar injeta estilos
// em tempo de execução (por isso 'unsafe-inline' só em style-src); script
// continua só deste host — o Scalar vem vendorizado em /static/vendor, sem CDN.
const apiDocsCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// apiInvoice é a nota na resposta da API. Valores em formato de máquina
// (status em inglês, valor com ponto, datas ISO); status_label e message são
// o texto para mostrar a uma pessoa.
type apiInvoice struct {
	ID             int64  `json:"id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Status         string `json:"status"`
	StatusLabel    string `json:"status_label"`
	Environment    string `json:"environment"`
	Series         string `json:"dps_series"`
	DPSNumber      int64  `json:"dps_number"`
	NFSeNumber     string `json:"nfse_number,omitempty"`
	AccessKey      string `json:"access_key,omitempty"`
	Amount         string `json:"amount"`
	Competence     string `json:"competence,omitempty"`
	IssuedAt       string `json:"issued_at,omitempty"`
	CustomerID     int64  `json:"customer_id"`
	TemplateID     int64  `json:"template_id"`
	Description    string `json:"description"`
	ErrorCode      string `json:"error_code,omitempty"`
	Message        string `json:"message,omitempty"`
}

// toAPIInvoice converte a nota do domínio, já com o erro da SEFIN traduzido.
func toAPIInvoice(i domain.Invoice) apiInvoice {
	out := apiInvoice{
		ID: i.ID, IdempotencyKey: i.IdempotencyKey, Status: string(i.Status), StatusLabel: i.Status.Label(),
		Environment: string(i.Environment), Series: i.Series, DPSNumber: i.DPSNumber, NFSeNumber: i.NFSeNumber,
		AccessKey: i.AccessKey, Amount: i.Amount.FormatDecimal(), CustomerID: i.CustomerID, TemplateID: i.TemplateID,
		Description: i.Description, ErrorCode: i.ErrorCode,
	}
	if !i.Competence.IsZero() {
		out.Competence = i.Competence.Format("2006-01-02")
	}
	if !i.IssuedAt.IsZero() {
		out.IssuedAt = i.IssuedAt.Format(time.RFC3339)
	}
	if i.ErrorMessage != "" {
		out.Message = domain.TranslateSefinError(i.ErrorCode, i.ErrorMessage)
	}
	return out
}

// apiError é o corpo de toda resposta de erro: error para a pessoa, fields
// por campo quando o pedido tem dado inválido, invoice quando a nota existe.
type apiError struct {
	Error   string            `json:"error"`
	Fields  map[string]string `json:"fields,omitempty"`
	Invoice *apiInvoice       `json:"invoice,omitempty"`
}

// apiEndpoint é uma rota autenticada da API: método, caminho relativo a
// /api/v1 (o mesmo texto da chave em paths do openapi.json) e o handler.
type apiEndpoint struct {
	Method, Path string
	Handle       func(s *Server) http.HandlerFunc
}

// apiEndpoints é a lista única das rotas autenticadas: apiRoutes registra
// exatamente estas, e um teste confere que o openapi.json descreve as mesmas.
var apiEndpoints = []apiEndpoint{
	{"GET", "/account", func(s *Server) http.HandlerFunc { return s.apiAccount }},
	{"GET", "/customers", func(s *Server) http.HandlerFunc { return s.apiCustomers }},
	{"GET", "/templates", func(s *Server) http.HandlerFunc { return s.apiTemplates }},
	{"GET", "/invoices", func(s *Server) http.HandlerFunc { return s.apiInvoices }},
	{"POST", "/invoices", func(s *Server) http.HandlerFunc { return s.apiEmit }},
	{"GET", "/invoices/{id}", func(s *Server) http.HandlerFunc { return s.apiInvoice }},
	{"GET", "/invoices/by-key", func(s *Server) http.HandlerFunc { return s.apiInvoiceByKey }},
	{"POST", "/invoices/{id}/cancel", func(s *Server) http.HandlerFunc { return s.apiCancel }},
	{"GET", "/invoices/{id}/danfse", func(s *Server) http.HandlerFunc { return s.apiDANFSe }},
}

// apiRoutes monta as rotas da API. As três públicas (contrato, documentação
// e llms.txt) não pedem token; o resto passa por apiAuth.
func (s *Server) apiRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/openapi.json", s.apiOpenAPI)
	mux.HandleFunc("GET /api/docs", s.apiDocs)
	mux.HandleFunc("GET /api/llms.txt", s.apiLLMs)
	for _, endpoint := range apiEndpoints {
		mux.Handle(endpoint.Method+" /api/v1"+endpoint.Path, s.apiAuth(endpoint.Handle(s)))
	}
	// Rota desconhecida responde JSON, não a página 404 em texto do ServeMux.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeAPI(w, http.StatusNotFound, apiError{Error: "rota não encontrada: veja " + s.baseURL + "/api/docs"})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Mesma defesa do painel contra DNS rebinding: Host fora da lista é recusado.
		if !s.hosts[r.Host] {
			writeAPI(w, http.StatusForbidden, apiError{Error: "host não permitido"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// apiAuth exige o token de API (o mesmo do MCP) e põe a conta dele no
// contexto: cada chamada só enxerga os dados da conta dona do token. Sessão e
// CSRF não se aplicam — quem chama é o sistema do integrador, não o navegador.
func (s *Server) apiAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, status, message := s.bearer(r)
		if status != 0 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="nfse-digital"`)
			writeAPI(w, status, apiError{Error: message})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, apiMaxBody)
		next(w, r.WithContext(ports.WithAccount(r.Context(), token.AccountID)))
	})
}

// writeAPI responde o valor em JSON indentado, sem cache (resposta com dado fiscal).
func writeAPI(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

// apiFail traduz o erro do caso de uso em status HTTP. A tabela é a mesma do
// openapi.json; erro sem tradução é 500 e vai para o log, sem detalhe na resposta.
func apiFail(w http.ResponseWriter, err error) {
	var fields domain.FieldErrors
	switch {
	case errors.As(err, &fields):
		writeAPI(w, http.StatusUnprocessableEntity, apiError{Error: "dados inválidos", Fields: fields})
	case errors.Is(err, ports.ErrNotFound):
		writeAPI(w, http.StatusNotFound, apiError{Error: "não encontrado"})
	case errors.Is(err, app.ErrProductionNotConfirmed), errors.Is(err, domain.ErrNotCancellable), errors.Is(err, ports.ErrConflict):
		writeAPI(w, http.StatusConflict, apiError{Error: err.Error()})
	default:
		log.Printf("api: %v", err)
		writeAPI(w, http.StatusInternalServerError, apiError{Error: "erro interno"})
	}
}

// pathID lê o {id} da rota; ID que não é número positivo vira 404, como nota inexistente.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeAPI(w, http.StatusNotFound, apiError{Error: "não encontrado"})
		return 0, false
	}
	return id, true
}

// decodeBody lê o JSON do pedido. Campo desconhecido é recusado: um
// "confirmProduction" digitado errado seria ignorado em silêncio e a emissão
// em produção seria recusada sem o integrador entender por quê.
func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeAPI(w, http.StatusBadRequest, apiError{Error: "JSON inválido: " + err.Error()})
		return false
	}
	return true
}

// apiOpenAPI entrega o contrato com o endereço deste painel em servers, para
// o "Testar requisição" do Scalar e o import no Postman apontarem para cá.
func (s *Server) apiOpenAPI(w http.ResponseWriter, r *http.Request) {
	var spec map[string]any
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		apiFail(w, fmt.Errorf("openapi.json embutido inválido: %w", err))
		return
	}
	spec["servers"] = []map[string]string{{"url": s.baseURL + "/api/v1", "description": "este painel"}}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeAPI(w, http.StatusOK, spec)
}

// apiDocs entrega a página do Scalar, com a CSP própria dela.
func (s *Server) apiDocs(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", apiDocsCSP)
	header.Set("X-Frame-Options", "DENY")
	header.Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(apiDocsPage)
}

// apiLLMs entrega o llms.txt com as URLs absolutas deste painel.
func (s *Server) apiLLMs(w http.ResponseWriter, r *http.Request) {
	var buffer bytes.Buffer
	page := template.Must(template.New("llms").Parse(llmsTemplate))
	if err := page.Execute(&buffer, map[string]string{"BaseURL": s.baseURL}); err != nil {
		apiFail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = buffer.WriteTo(w)
}

// apiAccount mostra o emitente da conta do token: confere a integração antes
// da primeira emissão (ambiente, série e próximo número da DPS).
func (s *Server) apiAccount(w http.ResponseWriter, r *http.Request) {
	issuer, err := s.service.Repo.Issuer(r.Context())
	if err != nil {
		apiFail(w, err)
		return
	}
	writeAPI(w, http.StatusOK, map[string]any{
		"legal_name": issuer.LegalName, "cnpj": issuer.CNPJ, "environment": string(issuer.Environment),
		"series": issuer.Series, "next_dps_number": issuer.NextDPSNumber,
	})
}

// apiCustomers lista os tomadores; ?q= filtra por nome ou documento.
func (s *Server) apiCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := s.service.Repo.Customers(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		apiFail(w, err)
		return
	}
	out := []map[string]any{}
	for _, c := range customers {
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "document": c.Document, "email": c.Email, "city": c.CityName})
	}
	writeAPI(w, http.StatusOK, map[string]any{"customers": out})
}

// apiTemplates lista os modelos de serviço; ?q= filtra pelo nome.
func (s *Server) apiTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.service.Repo.Templates(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		apiFail(w, err)
		return
	}
	out := []map[string]any{}
	for _, t := range templates {
		out = append(out, map[string]any{
			"id": t.ID, "name": t.Name, "service_code": t.NationalTaxCode,
			"default_amount": t.DefaultAmount.FormatDecimal(), "description": t.Description,
		})
	}
	writeAPI(w, http.StatusOK, map[string]any{"templates": out})
}

// apiInvoices lista as notas da mais recente para a mais antiga; ?status=
// filtra (pending, authorized, rejected, cancelled) e ?limit= corta (1 a 100).
func (s *Server) apiInvoices(w http.ResponseWriter, r *http.Request) {
	limit := apiDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > apiMaxLimit {
			writeAPI(w, http.StatusBadRequest, apiError{Error: fmt.Sprintf("limit deve ser um número de 1 a %d", apiMaxLimit)})
			return
		}
		limit = parsed
	}
	status := domain.InvoiceStatus(r.URL.Query().Get("status"))
	if status != "" && !knownStatus(status) {
		writeAPI(w, http.StatusBadRequest, apiError{Error: "status deve ser pending, authorized, rejected ou cancelled"})
		return
	}
	invoices, err := s.service.Repo.Invoices(r.Context())
	if err != nil {
		apiFail(w, err)
		return
	}
	out := []apiInvoice{}
	for _, invoice := range invoices {
		if len(out) == limit {
			break
		}
		if status == "" || invoice.Status == status {
			out = append(out, toAPIInvoice(invoice))
		}
	}
	writeAPI(w, http.StatusOK, map[string]any{"invoices": out})
}

// knownStatus diz se o status do filtro existe.
func knownStatus(status domain.InvoiceStatus) bool {
	switch status {
	case domain.StatusPending, domain.StatusAuthorized, domain.StatusRejected, domain.StatusCancelled:
		return true
	}
	return false
}

// apiInvoice consulta uma nota pelo ID.
func (s *Server) apiInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	invoice, err := s.service.Repo.Invoice(r.Context(), id)
	if err != nil {
		apiFail(w, err)
		return
	}
	writeAPI(w, http.StatusOK, toAPIInvoice(invoice))
}

// apiInvoiceByKey consulta uma nota pela idempotency_key da emissão (o id da
// fatura no sistema do integrador): ele não precisa guardar o nosso ID. A
// chave vai na query, não no caminho: /invoices/{id}/cancel e
// /invoices/by-key/{key} casariam o mesmo caminho e o ServeMux recusa os dois.
func (s *Server) apiInvoiceByKey(w http.ResponseWriter, r *http.Request) {
	invoice, err := s.service.InvoiceByIdempotencyKey(r.Context(), r.URL.Query().Get("idempotency_key"))
	if err != nil {
		apiFail(w, err)
		return
	}
	writeAPI(w, http.StatusOK, toAPIInvoice(invoice))
}

// apiEmitRequest é o corpo do POST /invoices.
type apiEmitRequest struct {
	IdempotencyKey    string `json:"idempotency_key"`
	CustomerID        int64  `json:"customer_id"`
	TemplateID        int64  `json:"template_id"`
	Amount            string `json:"amount"`
	Competence        string `json:"competence"`
	Description       string `json:"description"`
	ConfirmProduction bool   `json:"confirm_production"`
}

// apiEmit emite pela mesma regra do painel e do MCP. A idempotency_key é
// obrigatória: repetir a mesma chave devolve a nota já criada (200) em vez de
// emitir outra. Nota nova responde 201 (autorizada ou rejeitada — o status diz
// qual); sem resposta definitiva da SEFIN, 202: consulte antes de tentar de novo.
func (s *Server) apiEmit(w http.ResponseWriter, r *http.Request) {
	var in apiEmitRequest
	if !decodeBody(w, r, &in) {
		return
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		apiFail(w, domain.FieldErrors{"idempotency_key": "obrigatória: use um id estável do seu sistema (ex.: o id da fatura)"})
		return
	}
	ctx := r.Context()
	if previous, err := s.service.InvoiceByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
		writeAPI(w, http.StatusOK, toAPIInvoice(previous))
		return
	} else if !errors.Is(err, ports.ErrNotFound) {
		apiFail(w, err)
		return
	}
	// Campos vazios vêm do modelo, como no MCP: valor padrão, competência de
	// hoje e a descrição com {mes} e {ano} resolvidos.
	template, err := s.service.Repo.Template(ctx, in.TemplateID)
	if err != nil {
		apiFail(w, domain.FieldErrors{"template_id": "modelo de serviço não encontrado (GET /templates)"})
		return
	}
	if in.Competence == "" {
		in.Competence = s.now().In(s.location).Format("2006-01-02")
	}
	if in.Amount == "" {
		in.Amount = template.DefaultAmount.InputValue()
	}
	if in.Description == "" {
		competence, _ := time.ParseInLocation("2006-01-02", in.Competence, s.location)
		in.Description = domain.RenderDescription(template.Description, competence)
	}
	invoice, err := s.service.Emit(ctx, app.EmitInput{
		CustomerID: in.CustomerID, TemplateID: in.TemplateID, Amount: in.Amount, Competence: in.Competence,
		Description: in.Description, IdempotencyKey: in.IdempotencyKey, ConfirmProduction: in.ConfirmProduction,
	})
	if invoice.ID == 0 {
		apiFail(w, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("%s/api/v1/invoices/%d", s.baseURL, invoice.ID))
	out := toAPIInvoice(invoice)
	if err != nil && invoice.Status == domain.StatusPending {
		out.Message = "sem resposta definitiva da SEFIN (" + err.Error() + "): consulte a nota antes de qualquer nova emissão"
		writeAPI(w, http.StatusAccepted, out)
		return
	}
	writeAPI(w, http.StatusCreated, out)
}

// apiCancelRequest é o corpo do POST /invoices/{id}/cancel.
type apiCancelRequest struct {
	Reason            string `json:"reason"`
	Description       string `json:"description"`
	ConfirmProduction bool   `json:"confirm_production"`
}

// apiCancel registra o cancelamento na SEFIN. Aceito, 200 com a nota
// cancelada; recusado pela SEFIN, 422 com a nota (ainda autorizada) e o motivo.
func (s *Server) apiCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in apiCancelRequest
	if !decodeBody(w, r, &in) {
		return
	}
	if s.canceller == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiError{Error: "cancelamento desligado neste painel"})
		return
	}
	report, err := s.service.Cancel(r.Context(), s.canceller, id, in.Reason, in.Description, s.appVersion, in.ConfirmProduction)
	if err != nil {
		var fields domain.FieldErrors
		if report.Invoice.ID == 0 || errors.As(err, &fields) || errors.Is(err, app.ErrProductionNotConfirmed) || errors.Is(err, domain.ErrNotCancellable) {
			apiFail(w, err)
			return
		}
		// A nota existe e o pedido era válido: falhou a conversa com a SEFIN.
		log.Printf("api: cancelar nota #%d: %v", id, err)
		out := toAPIInvoice(report.Invoice)
		writeAPI(w, http.StatusBadGateway, apiError{Error: "a SEFIN não respondeu ao cancelamento: tente de novo", Invoice: &out})
		return
	}
	out := toAPIInvoice(report.Invoice)
	if !report.Registered {
		writeAPI(w, http.StatusUnprocessableEntity, apiError{Error: "a SEFIN recusou o cancelamento: " + out.Message, Invoice: &out})
		return
	}
	writeAPI(w, http.StatusOK, out)
}

// apiDANFSe entrega o PDF do DANFSe de uma nota autorizada ou cancelada.
func (s *Server) apiDANFSe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.danfsePDF == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiError{Error: "DANFSe desligado neste painel"})
		return
	}
	invoice, err := s.service.Repo.Invoice(r.Context(), id)
	if err != nil {
		apiFail(w, err)
		return
	}
	if invoice.AccessKey == "" || (invoice.Status != domain.StatusAuthorized && invoice.Status != domain.StatusCancelled) {
		writeAPI(w, http.StatusConflict, apiError{Error: "só nota autorizada ou cancelada tem DANFSe"})
		return
	}
	pdf, name, err := s.danfsePDF(r.Context(), invoice)
	if err != nil {
		apiFail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(pdf)
}
