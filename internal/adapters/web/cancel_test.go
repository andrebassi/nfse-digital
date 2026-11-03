package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// recordingCanceller devolve a resposta programada e conta as idas à SEFIN.
type recordingCanceller struct {
	result ports.CancelResult
	err    error
	calls  int
	got    domain.CancelEvent
}

// Cancel cumpre ports.Canceller para o teste.
func (c *recordingCanceller) Cancel(_ context.Context, event domain.CancelEvent) (ports.CancelResult, error) {
	c.calls++
	c.got = event
	return c.result, c.err
}

// saveAuthorized grava uma nota autorizada fictícia, do tomador semeado, no
// ambiente pedido, e devolve o id.
func saveAuthorized(t *testing.T, f fixture, environment domain.Environment) int64 {
	t.Helper()
	customers, _ := f.repo.Customers(context.Background(), "")
	invoice := domain.Invoice{
		Environment: environment, Status: domain.StatusAuthorized, CustomerID: customers[0].ID, Amount: 100,
		AccessKey: "35057082112223330001810000000000000126110000000001", NFSeNumber: "1", Series: "1", DPSNumber: 202634,
	}
	if err := f.repo.SaveInvoice(context.Background(), &invoice); err != nil {
		t.Fatal(err)
	}
	return invoice.ID
}

// cancelForm é o formulário preenchido do jeito que o painel manda.
func cancelForm() url.Values {
	return url.Values{"reason": {"erro_emissao"}, "description": {"Nota de teste emitida por engano, sem serviço."}}
}

// detailPath é a URL do detalhe da nota, para onde o cancelamento volta.
func detailPath(id int64) string { return "/invoices?period=90&id=" + strconv.FormatInt(id, 10) }

// TestCancelHiddenWithoutCanceller: painel sem SEFIN real (protótipo, testes)
// não mostra o formulário e responde 404 ao POST.
func TestCancelHiddenWithoutCanceller(t *testing.T) {
	f := newFixture(t, nil, nil)
	id := saveAuthorized(t, f, domain.EnvHomologation)
	if strings.Contains(f.get(detailPath(id)).Body.String(), "Cancelar nota") {
		t.Fatal("formulário de cancelamento sem canceller")
	}
	if response := f.post("/invoices/"+strconv.FormatInt(id, 10)+"/cancel", cancelForm()); response.Code != http.StatusNotFound {
		t.Fatalf("POST sem canceller: %d", response.Code)
	}
}

// TestCancelHomologation: nota autorizada mostra o formulário; enviar cancela
// na SEFIN, grava "cancelada", leva o verAplic do painel e volta ao detalhe com
// aviso — e a nota cancelada não oferece cancelar de novo.
func TestCancelHomologation(t *testing.T) {
	f := newFixture(t, nil, nil)
	canceller := &recordingCanceller{result: ports.CancelResult{Registered: true}}
	f.server.UseCanceller(canceller, "nfse-digital teste")
	id := saveAuthorized(t, f, domain.EnvHomologation)
	body := f.get(detailPath(id)).Body.String()
	if !strings.Contains(body, "Cancelar nota") || strings.Contains(body, `name="confirm_production"`) {
		t.Fatal("homologação: formulário ausente ou pedindo confirmação de produção")
	}
	response := f.post("/invoices/"+strconv.FormatInt(id, 10)+"/cancel", cancelForm())
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), url.QueryEscape("Nota cancelada na SEFIN")) {
		t.Fatalf("cancelar: %d %q", response.Code, response.Header().Get("Location"))
	}
	stored, _ := f.repo.Invoice(context.Background(), id)
	if stored.Status != domain.StatusCancelled || canceller.calls != 1 || canceller.got.ReasonCode != "1" || canceller.got.AppVersion != "nfse-digital teste" {
		t.Fatalf("nota %+v, evento %+v", stored, canceller.got)
	}
	if strings.Contains(f.get(detailPath(id)).Body.String(), "Cancelar nota") {
		t.Error("nota cancelada ainda oferece cancelar")
	}
}

// TestCancelProductionNeedsConfirmation: em produção o formulário traz a caixa
// e, sem ela marcada, nada vai à SEFIN — o evento é irreversível (story 4.1).
func TestCancelProductionNeedsConfirmation(t *testing.T) {
	f := newFixture(t, nil, nil)
	canceller := &recordingCanceller{result: ports.CancelResult{Registered: true}}
	f.server.UseCanceller(canceller, "v")
	id := saveAuthorized(t, f, domain.EnvProduction)
	if !strings.Contains(f.get(detailPath(id)).Body.String(), `name="confirm_production"`) {
		t.Fatal("produção sem a caixa de confirmação")
	}
	path := "/invoices/" + strconv.FormatInt(id, 10) + "/cancel"
	response := f.post(path, cancelForm())
	if !strings.Contains(response.Header().Get("Location"), url.QueryEscape("marque a confirmação")) || canceller.calls != 0 {
		t.Fatalf("sem confirmação: %q, idas à SEFIN %d", response.Header().Get("Location"), canceller.calls)
	}
	confirmed := cancelForm()
	confirmed.Set("confirm_production", "yes")
	f.post(path, confirmed)
	if stored, _ := f.repo.Invoice(context.Background(), id); stored.Status != domain.StatusCancelled || canceller.calls != 1 {
		t.Fatalf("com confirmação: %+v, idas %d", stored, canceller.calls)
	}
}

// TestCancelRefusalsAndErrors: justificativa curta não sai do painel; recusa da
// SEFIN deixa a nota autorizada com o motivo; falha de rede vira 500; nota
// inexistente vira 404.
func TestCancelRefusalsAndErrors(t *testing.T) {
	f := newFixture(t, nil, nil)
	canceller := &recordingCanceller{result: ports.CancelResult{ErrorCode: "E1235", ErrorMessage: "fora do prazo"}}
	f.server.UseCanceller(canceller, "v")
	id := saveAuthorized(t, f, domain.EnvHomologation)
	path := "/invoices/" + strconv.FormatInt(id, 10) + "/cancel"

	short := cancelForm()
	short.Set("description", "curta")
	if response := f.post(path, short); !strings.Contains(response.Header().Get("Location"), url.QueryEscape("Cancelamento não enviado")) || canceller.calls != 0 {
		t.Fatalf("justificativa curta: %q, idas %d", response.Header().Get("Location"), canceller.calls)
	}
	if response := f.post(path, cancelForm()); !strings.Contains(response.Header().Get("Location"), url.QueryEscape("recusou o cancelamento")) {
		t.Fatalf("recusa: %q", response.Header().Get("Location"))
	}
	if stored, _ := f.repo.Invoice(context.Background(), id); stored.Status != domain.StatusAuthorized || stored.ErrorCode != "E1235" {
		t.Fatalf("recusada: %+v", stored)
	}
	canceller.err = errors.New("SEFIN fora do ar")
	if response := f.post(path, cancelForm()); response.Code != http.StatusInternalServerError {
		t.Errorf("falha de rede: %d", response.Code)
	}
	if response := f.post("/invoices/99999/cancel", cancelForm()); response.Code != http.StatusNotFound {
		t.Errorf("inexistente: %d", response.Code)
	}
}
