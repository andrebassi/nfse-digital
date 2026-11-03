package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"

	"nfse/internal/app"
	"nfse/internal/domain"
)

// PortalOps são as leituras da base nacional que o painel dispara. Ambas só
// LEEM da SEFIN/ADN com o certificado; nada é enviado em nome do emitente.
type PortalOps struct {
	Scan func(ctx context.Context, environment domain.Environment) (app.PortalScan, error)
	Sync func(ctx context.Context, environment domain.Environment) (app.SyncReport, error)
}

// portalResult alimenta o cartão "Consulta concluída" (partials/portal_result).
type portalResult struct {
	Found     int
	Batches   int
	Templates int
	Imported  int
	// Segments é um quadrado por nota encontrada (true = cancelada, mais escuro).
	Segments []bool
	Recent   []portalRow
	Error    string
}

// portalRow é uma linha de "Encontradas agora".
type portalRow struct {
	Number, Customer, Competence, Amount string
}

// recentShown é quantas notas a lista "Encontradas agora" mostra.
const recentShown = 4

// maxSegments limita a barra de progresso: com centenas de notas os quadrados
// ficariam finos demais para ler; acima disso a barra mostra as mais recentes.
const maxSegments = 60

// UsePortal liga a importação do portal e a sincronização.
func (s *Server) UsePortal(ops PortalOps) {
	s.portal = &ops
}

// portalPage é a tela que abre já importando: o destino depois do primeiro
// certificado. Com JavaScript, o portal.js dispara a importação sozinho;
// sem ele, o botão "Importar agora" faz o mesmo POST.
func (s *Server) portalPage(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, http.StatusOK, "portal", "Importar do Portal Nacional", nil)
}

// portalImport lê toda a distribuição do CNPJ em PRODUÇÃO (onde estão as
// notas reais; ler não emite nada) e grava tudo de uma vez: tomadores,
// modelos e notas, sem duplicar as já importadas. O resultado volta como o
// fragmento do cartão (pedido do portal.js) ou como página inteira.
func (s *Server) portalImport(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		http.NotFound(w, r)
		return
	}
	result, status := s.runPortalImport(r.Context(), hideValues(r))
	if r.Header.Get("X-Fragment") == "portal_result" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := s.fragments.ExecuteTemplate(w, "portal_result", result); err != nil {
			log.Printf("renderizar portal_result: %v", err)
		}
		return
	}
	s.render(w, r, status, "portal", "Importar do Portal Nacional", result)
}

// runPortalImport faz a varredura e a gravação e monta o cartão. A recusa
// (sem autorização, portal fora) vira mensagem no cartão, nunca 500. hide
// mascara os valores, como o olho do cabeçalho faz no resto do painel.
func (s *Server) runPortalImport(ctx context.Context, hide bool) (portalResult, int) {
	if allowed, err := s.portalAllowed(ctx); err != nil || !allowed {
		return portalResult{Error: portalNotAuthorized}, http.StatusForbidden
	}
	scan, err := s.portal.Scan(ctx, domain.EnvProduction)
	if err != nil {
		return portalResult{Error: "não foi possível ler o Portal Nacional: " + err.Error()}, http.StatusBadGateway
	}
	imported, err := s.service.ApplyPortal(ctx, scan, []string{"all"})
	if err != nil {
		return portalResult{Error: "a leitura deu certo, mas a gravação falhou: " + err.Error()}, http.StatusInternalServerError
	}
	result := portalResult{Found: scan.Emitted, Batches: scan.Batches, Templates: len(scan.Groups), Imported: imported}
	notes := scan.Recent(scan.Emitted)
	for index, note := range notes {
		if index < maxSegments {
			result.Segments = append(result.Segments, note.Invoice.Status == domain.StatusCancelled)
		}
		if index < recentShown {
			result.Recent = append(result.Recent, portalRow{
				Number:     note.Invoice.NFSeNumber,
				Customer:   note.Customer.Name,
				Competence: note.Invoice.Competence.Format("01/2006"),
				Amount:     funcs["brl"].(func(bool, domain.Money) string)(hide, note.Invoice.Amount),
			})
		}
	}
	return result, http.StatusOK
}

// syncSubmit sincroniza as notas de CADA ambiente que tem nota no banco, não
// só o do emitente: notas importadas do portal são de produção mesmo com o
// emitente configurado em homologação. Volta para a lista com o resumo.
func (s *Server) syncSubmit(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		http.NotFound(w, r)
		return
	}
	if allowed, err := s.portalAllowed(r.Context()); err != nil || !allowed {
		http.Redirect(w, r, "/invoices?period=90&flash="+url.QueryEscape(portalNotAuthorized), http.StatusSeeOther)
		return
	}
	invoices, err := s.repo.Invoices(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	report := app.SyncReport{}
	for _, environment := range []domain.Environment{domain.EnvHomologation, domain.EnvProduction} {
		if !slices.ContainsFunc(invoices, func(i domain.Invoice) bool { return i.Environment == environment }) {
			continue
		}
		partial, syncErr := s.portal.Sync(r.Context(), environment)
		report.Checked += partial.Checked
		report.Authorized += partial.Authorized
		report.Cancelled += partial.Cancelled
		report.Unchanged += partial.Unchanged
		report.Failed += partial.Failed
		err = errors.Join(err, syncErr)
	}
	message := fmt.Sprintf("Sincronizado: %d consultadas, %d autorizadas agora, %d canceladas agora, %d sem mudança, %d com falha",
		report.Checked, report.Authorized, report.Cancelled, report.Unchanged, report.Failed)
	if err != nil {
		message = "Falha ao sincronizar: " + err.Error()
	}
	http.Redirect(w, r, "/invoices?period=90&flash="+url.QueryEscape(message), http.StatusSeeOther)
}

// portalNotAuthorized orienta quando a consulta ao portal não foi autorizada.
const portalNotAuthorized = "A consulta ao Portal Nacional não está autorizada: autorize em Configurações → Certificado"
