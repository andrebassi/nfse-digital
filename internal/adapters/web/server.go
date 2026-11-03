// Package web é o painel local: HTML renderizado no servidor, sem JavaScript,
// no visual do shadcn admin. Escuta só em 127.0.0.1.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// pageNames são as páginas do painel; cada uma é montada com o layout comum.
var pageNames = []string{
	"invoices", "emit", "schedules", "schedule_form", "customers", "customer_form",
	"templates", "template_form", "settings", "integrations", "import", "portal", "environment", "customer_email",
}

// authPageNames são as telas de entrar e criar conta, com o layout sem barra lateral.
var authPageNames = []string{"login", "signup"}

// Server guarda o que as rotas precisam: casos de uso, repositório, páginas
// já compiladas e o token CSRF deste processo.
type Server struct {
	service  *app.Service
	repo     ports.Repository
	pages    map[string]*template.Template
	csrf     string
	hosts    map[string]bool
	location *time.Location
	now      func() time.Time
	sessions *sessionStore
	// danfsePDF gera o PDF do DANFSe da nota; nil esconde o download.
	danfsePDF func(ctx context.Context, invoice domain.Invoice) (pdf []byte, name string, err error)
	// canceller registra o evento de cancelamento na SEFIN; nil esconde o botão.
	canceller ports.Canceller
	// appVersion vai no verAplic do evento de cancelamento, como na emissão.
	appVersion string
	// portal liga a importação do portal e a sincronização; nil esconde as duas.
	portal *PortalOps
	// integration alimenta a tela de Integrações (binário, ferramentas, skill).
	integration Integration
	// certificate liga o envio e a conferência do A1; nil esconde os dois.
	certificate *CertificateOps
	// mcpHandler é o servidor MCP por HTTP; nil deixa /mcp fora do ar.
	mcpHandler http.Handler
	// baseURL é o endereço do painel, para os comandos da tela de Integrações.
	baseURL string
	// passkeys é o WebAuthn do painel (cadastro e login por passkey).
	passkeys *passkeyRP
	// fragments são pedaços de página que o JavaScript pede e injeta
	// (partials/), renderizados com o mesmo escape das páginas.
	fragments *template.Template
	// mcpSessions amarra cada sessão MCP (Mcp-Session-Id) à conta que a abriu.
	mcpSessions sync.Map
	// corsOrigins são as origens que podem chamar /api/v1 do navegador
	// (a documentação); vazio = nenhuma.
	corsOrigins map[string]bool
	// secure marca os cookies como Secure: ligado com host público (HTTPS
	// terminado no proxy do Fly); desligado em http://127.0.0.1, onde o
	// navegador descartaria um cookie Secure.
	httpsOnly bool
}

// UsePublicHost publica o painel num domínio HTTPS (deploy no Fly): o host
// entra na lista de permitidos, os comandos de Integrações e a passkey passam
// a usar https://host, e os cookies ganham Secure. Host com esquema, porta ou
// caminho é recusado: a passkey exige o domínio puro.
func (s *Server) UsePublicHost(host string) error {
	if host == "" || strings.ContainsAny(host, ":/ ") {
		return fmt.Errorf("NFSE_PUBLIC_HOST inválido: %q (só o domínio, ex.: nfse.exemplo.com)", host)
	}
	origin := "https://" + host
	passkeys, err := newPasskeyRP(host, origin)
	if err != nil {
		return fmt.Errorf("configurar passkeys: %w", err)
	}
	s.passkeys = passkeys
	s.hosts[host] = true
	s.baseURL = origin
	s.httpsOnly = true
	return nil
}

// setCookie grava o cookie com Secure quando o painel está em HTTPS.
func (s *Server) setCookie(w http.ResponseWriter, cookie *http.Cookie) {
	cookie.Secure = s.httpsOnly
	http.SetCookie(w, cookie)
}

// UseCanceller liga o botão "Cancelar nota" no detalhe da nota autorizada.
func (s *Server) UseCanceller(canceller ports.Canceller, appVersion string) {
	s.canceller, s.appVersion = canceller, appVersion
}

// UseDANFSe liga o download do DANFSe na tela de notas.
func (s *Server) UseDANFSe(danfsePDF func(ctx context.Context, invoice domain.Invoice) ([]byte, string, error)) {
	s.danfsePDF = danfsePDF
}

// New monta o servidor para escutar na porta informada de 127.0.0.1. O token
// CSRF nasce aleatório a cada processo: reiniciar invalida formulários abertos.
func New(service *app.Service, port int) (*Server, error) {
	pages := map[string]*template.Template{}
	for _, name := range pageNames {
		page, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html", "templates/partials/*.html")
		if err != nil {
			return nil, fmt.Errorf("compilar página %s: %w", name, err)
		}
		pages[name] = page
	}
	for _, name := range authPageNames {
		page, err := template.New("auth_layout.html").Funcs(funcs).ParseFS(templateFS, "templates/auth_layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("compilar página %s: %w", name, err)
		}
		pages[name] = page
	}
	fragments, err := template.New("partials").Funcs(funcs).ParseFS(templateFS, "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("compilar fragmentos: %w", err)
	}
	passkeys, err := newPasskeyRP("localhost", fmt.Sprintf("http://localhost:%d", port))
	if err != nil {
		return nil, fmt.Errorf("configurar passkeys: %w", err)
	}
	return &Server{
		fragments: fragments,
		passkeys:  passkeys,
		service:   service,
		repo:      service.Repo,
		pages:     pages,
		csrf:      rand.Text(),
		hosts:     map[string]bool{fmt.Sprintf("127.0.0.1:%d", port): true, fmt.Sprintf("localhost:%d", port): true},
		location:  service.Location,
		now:       service.Now,
		sessions:  newSessionStore(),
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
	}, nil
}

// Handler devolve as rotas embrulhadas nos middlewares de segurança.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /invoices", s.invoices)
	mux.HandleFunc("GET /invoices/{id}/danfse", s.danfse)
	mux.HandleFunc("POST /invoices/{id}/cancel", s.cancelSubmit)
	mux.HandleFunc("GET /portal", s.portalPage)
	mux.HandleFunc("POST /portal/import", s.portalImport)
	mux.HandleFunc("POST /sync", s.syncSubmit)
	mux.HandleFunc("POST /preferences/hide-values", s.toggleHideValues)
	mux.HandleFunc("POST /environment/homologation", s.environmentHomologation)
	mux.HandleFunc("GET /environment/production", s.environmentProductionForm)
	mux.HandleFunc("POST /environment/production", s.environmentProduction)
	mux.HandleFunc("GET /emit", s.emitForm)
	mux.HandleFunc("POST /emit", s.emitSubmit)
	mux.HandleFunc("GET /schedules", s.schedules)
	mux.HandleFunc("GET /schedules/new", s.scheduleForm)
	mux.HandleFunc("POST /schedules", s.scheduleSubmit)
	mux.HandleFunc("GET /customers", s.customers)
	mux.HandleFunc("GET /customers/new", s.customerForm)
	mux.HandleFunc("GET /customers/{id}", s.customerForm)
	mux.HandleFunc("POST /customers", s.customerSubmit)
	mux.HandleFunc("GET /customers/{id}/email", s.customerEmailForm)
	mux.HandleFunc("POST /customers/{id}/email", s.customerEmailSubmit)
	mux.HandleFunc("POST /customers/{id}/email/preview", s.customerEmailPreview)
	mux.HandleFunc("POST /invoices/{id}/email", s.invoiceEmail)
	mux.HandleFunc("GET /templates", s.templates)
	mux.HandleFunc("GET /templates/new", s.templateForm)
	mux.HandleFunc("GET /templates/{id}", s.templateForm)
	mux.HandleFunc("POST /templates", s.templateSubmit)
	mux.HandleFunc("GET /templates/import", s.importForm)
	mux.HandleFunc("POST /templates/import", s.importSubmit)
	mux.HandleFunc("GET /settings", s.settings)
	mux.HandleFunc("POST /settings", s.settingsSubmit)
	mux.HandleFunc("GET /settings/certificate", s.settingsSection("certificate"))
	mux.HandleFunc("GET /settings/security", s.settingsSection("security"))
	mux.HandleFunc("POST /settings/certificate", s.certificateReplace)
	mux.HandleFunc("POST /settings/portal", s.portalAuthorization)
	mux.HandleFunc("POST /settings/security/passkeys/options", s.passkeyRegisterOptions)
	mux.HandleFunc("POST /settings/security/passkeys", s.passkeyRegister)
	mux.HandleFunc("POST /settings/security/passkeys/remove", s.passkeysRemove)
	mux.HandleFunc("POST /login/passkey/options", s.passkeyLoginOptions)
	mux.HandleFunc("POST /login/passkey", s.passkeyLogin)
	mux.HandleFunc("GET /integrations", s.integrations)
	mux.HandleFunc("GET /integrations/connect/{agent}", s.integrationsConnect)
	mux.HandleFunc("GET /integrations/connect/{agent}/{client}", s.integrationsConnect)
	mux.HandleFunc("POST /integrations/test", s.integrationsTest)
	mux.HandleFunc("GET /integrations/api", s.integrationsAPI)
	mux.HandleFunc("GET /integrations/api/{op}", s.integrationsAPI)
	mux.HandleFunc("GET /integrations/tokens", s.integrationsSection("tokens"))
	mux.HandleFunc("GET /integrations/agents", s.integrationsSection("agents"))
	mux.HandleFunc("GET /integrations/tools", s.integrationsSection("tools"))
	mux.HandleFunc("GET /integrations/skill.md", s.integrationSkill)
	mux.HandleFunc("POST /integrations/tokens", s.tokenGenerate)
	mux.HandleFunc("POST /integrations/tokens/{id}/revoke", s.tokenRevoke)
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("GET /signup", s.signUpForm)
	mux.HandleFunc("POST /signup", s.signUpSubmit)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("POST /settings/security/delete-account", s.accountDelete)
	panel := s.secure(s.requireSession(s.requireCertificate(mux)))
	api := s.apiRoutes()
	// /mcp e /api são de sistemas, não do navegador: passam pelo portão do
	// token em vez da sessão e do CSRF. Os dois só existem com o MCP por HTTP
	// ligado, que é quando o painel emite tokens.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.mcpHandler != nil && (r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/")) {
			s.mcpGate(w, r)
			return
		}
		if s.mcpHandler != nil && strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		panel.ServeHTTP(w, r)
	})
}

// secure aplica as três defesas do painel local:
//   - Host fora de 127.0.0.1/localhost na porta certa é recusado, contra DNS
//     rebinding (uma página da internet apontando um domínio para 127.0.0.1);
//   - todo POST exige o token CSRF, contra formulário de outro site enviado
//     pelo navegador do dono;
//   - cabeçalhos que proíbem script externo, iframe e adivinhação de tipo.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		header.Set("X-Frame-Options", "DENY")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		if !s.hosts[r.Host] {
			http.Error(w, "host não permitido", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			// Limite antes de ler o formulário: o token CSRF é lido daqui, e ler
			// um upload sem limite ocuparia a memória do painel.
			if r.ContentLength > maxPostSize {
				http.Error(w, "envio grande demais: o limite é 2 MB", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxPostSize)
			// O passkey.js manda JSON, então o token vem no cabeçalho; os
			// formulários comuns continuam mandando no campo csrf.
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				token = r.PostFormValue("csrf")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.csrf)) != 1 {
				http.Error(w, "formulário expirado ou de outra origem: recarregue a página", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// page é o que todo template recebe: dados do layout mais os da página.
type page struct {
	Title   string
	Active  string
	Issuer  domain.Issuer
	Account domain.Account
	CSRF    string
	Hide    bool
	Path    string
	Flash   string
	Data    any
	// Portal mostra o menu do portal e o botão de sincronizar.
	Portal bool
	// Clock é o relógio do cabeçalho ("30 DE OUT. · 01:30").
	Clock string
	// Locked é a conta ainda sem certificado: o menu fica desabilitado e só
	// a aba Certificado abre (requireCertificate barra as outras rotas).
	Locked bool
}

// render executa a página num buffer antes de escrever, para um erro no meio
// do template virar 500 limpo em vez de HTML pela metade.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	issuer, _ := s.repo.Issuer(r.Context())
	account, _ := s.repo.Account(r.Context())
	view := page{
		Title: title, Active: name, Issuer: issuer, Account: account, CSRF: s.csrf, Hide: hideValues(r),
		Path: r.URL.RequestURI(), Flash: r.URL.Query().Get("flash"), Data: data, Portal: s.portal != nil,
		Clock: headerClock(s.now().In(s.location)), Locked: s.needsCertificate(r.Context()),
	}
	var buffer bytes.Buffer
	layout := "layout.html"
	if slices.Contains(authPageNames, name) {
		layout = "auth_layout.html"
	}
	if err := s.pages[name].ExecuteTemplate(&buffer, layout, view); err != nil {
		log.Printf("renderizar %s: %v", name, err)
		http.Error(w, "erro ao montar a página", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buffer.WriteTo(w)
}

// fail responde 404 para registro inexistente e 500 para o resto.
func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ports.ErrNotFound) {
		http.Error(w, "não encontrado", http.StatusNotFound)
		return
	}
	log.Printf("erro: %v", err)
	http.Error(w, "erro interno", http.StatusInternalServerError)
}

// hideValues lê a preferência de ocultar valores do cookie.
func hideValues(r *http.Request) bool {
	cookie, err := r.Cookie("hide_values")
	return err == nil && cookie.Value == "1"
}

// toggleHideValues inverte a preferência e volta para a página de origem.
func (s *Server) toggleHideValues(w http.ResponseWriter, r *http.Request) {
	value := "1"
	if hideValues(r) {
		value = "0"
	}
	s.setCookie(w, &http.Cookie{Name: "hide_values", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600})
	http.Redirect(w, r, safeBack(r.PostFormValue("back")), http.StatusSeeOther)
}

// safeBack só aceita caminho local ("/invoices?..."); "//site" e "/\site" o
// navegador trata como outro host, e virariam redirecionamento aberto.
func safeBack(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return "/invoices"
	}
	return target
}

// formID lê um ID numérico de formulário ou query; inválido vira zero.
func formID(value string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

// funcs são as funções de formatação disponíveis nos templates.
var funcs = template.FuncMap{
	"brl": func(hide bool, value domain.Money) string {
		if hide {
			return "R$ ••••••"
		}
		return value.FormatBRL()
	},
	"doc":       domain.FormatDocument,
	"date":      func(t time.Time) string { return t.Format("02/01/2006") },
	"datetime":  func(t time.Time) string { return t.Format("02/01/2006, 15:04") },
	"monthYear": func(t time.Time) string { return t.Format("01/2006") },
	"isoDate":   func(t time.Time) string { return t.Format("2006-01-02") },
	"brDate": func(iso string) string {
		parsed, err := time.Parse("2006-01-02", iso)
		if err != nil {
			return iso
		}
		return parsed.Format("02/01/2006")
	},
	"service":     domain.ServiceLabel,
	"serviceCode": domain.FormatServiceCode,
	"translate":   domain.TranslateSefinError,
	"badge": func(status domain.InvoiceStatus) string {
		return "badge badge-" + string(status)
	},
}
