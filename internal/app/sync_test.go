package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// fakeConsulter responde chave por DPS e documentos por chave.
type fakeConsulter struct {
	keys      map[string]string
	documents map[string]ports.DFeBatch
	failDPS   bool
	failDocs  bool
}

// AccessKeyByDPS devolve a chave configurada, ErrNotFound ou falha.
func (f fakeConsulter) AccessKeyByDPS(_ context.Context, dpsID string) (string, error) {
	if f.failDPS {
		return "", errors.New("SEFIN fora")
	}
	if key, ok := f.keys[dpsID]; ok {
		return key, nil
	}
	return "", ports.ErrNotFound
}

// Documents devolve o lote configurado ou falha.
func (f fakeConsulter) Documents(_ context.Context, accessKey string) (ports.DFeBatch, error) {
	if f.failDocs {
		return ports.DFeBatch{}, errors.New("ADN fora")
	}
	return f.documents[accessKey], nil
}

// TestSync: pendente achada vira autorizada com número; pendente não achada
// fica; autorizada com evento de cancelamento vira cancelada; simulada e de
// outro ambiente são ignoradas.
func TestSync(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(memory.DemoIssuer())
	add := func(invoice domain.Invoice) int64 {
		invoice.Series = "1"
		_ = repo.SaveInvoice(ctx, &invoice)
		return invoice.ID
	}
	found := add(domain.Invoice{Environment: domain.EnvHomologation, DPSNumber: 10, Status: domain.StatusPending, ErrorMessage: "timeout"})
	missing := add(domain.Invoice{Environment: domain.EnvHomologation, DPSNumber: 11, Status: domain.StatusPending})
	cancelled := add(domain.Invoice{Environment: domain.EnvHomologation, AccessKey: "KC", Status: domain.StatusAuthorized, NFSeNumber: "5"})
	stable := add(domain.Invoice{Environment: domain.EnvHomologation, AccessKey: "KS", Status: domain.StatusAuthorized, NFSeNumber: "6"})
	add(domain.Invoice{Environment: domain.EnvHomologation, DPSNumber: 12, Status: domain.StatusPending, Simulated: true})
	add(domain.Invoice{Environment: domain.EnvProduction, DPSNumber: 13, Status: domain.StatusPending})

	notes := map[string]domain.ImportedNFSe{"KF": {Invoice: domain.Invoice{NFSeNumber: "77"}}}
	consulter := fakeConsulter{
		keys: map[string]string{domain.DPSID("3505708", domain.KindCNPJ, "11222333000181", "1", 10): "KF"},
		documents: map[string]ports.DFeBatch{
			"KF": {Documents: []ports.DFeDocument{{Type: "NFSE", XML: []byte("nfse:KF")}}},
			"KC": {Documents: []ports.DFeDocument{{Type: "NFSE", XML: []byte("nfse:quebrada")}, {Type: "EVENTO", XML: []byte("evento:KC")}}},
			"KS": {Documents: []ports.DFeDocument{{Type: "EVENTO", XML: []byte("evento:OUTRA")}}},
		},
	}
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	report, err := service.Sync(ctx, consulter, fakeParsers(notes), domain.EnvHomologation)
	if err != nil {
		t.Fatal(err)
	}
	if report != (SyncReport{Checked: 4, Authorized: 1, Cancelled: 1, Unchanged: 2}) {
		t.Fatalf("relatório: %+v", report)
	}
	expect := map[int64]domain.InvoiceStatus{found: domain.StatusAuthorized, missing: domain.StatusPending, cancelled: domain.StatusCancelled, stable: domain.StatusAuthorized}
	for id, status := range expect {
		if invoice, _ := repo.Invoice(ctx, id); invoice.Status != status {
			t.Errorf("nota %d: %s", id, invoice.Status)
		}
	}
	if invoice, _ := repo.Invoice(ctx, found); invoice.AccessKey != "KF" || invoice.NFSeNumber != "77" || invoice.ErrorMessage != "" {
		t.Fatalf("pendente resolvida: %+v", invoice)
	}
}

// TestSyncFailures: falha da consulta conta como falha sem mudar a nota;
// falha ao ler documentos mantém a nota; sem emitente, erro.
func TestSyncFailures(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(memory.DemoIssuer())
	pending := domain.Invoice{Environment: domain.EnvHomologation, Series: "1", DPSNumber: 10, Status: domain.StatusPending}
	_ = repo.SaveInvoice(ctx, &pending)
	authorized := domain.Invoice{Environment: domain.EnvHomologation, AccessKey: "K", Status: domain.StatusAuthorized}
	_ = repo.SaveInvoice(ctx, &authorized)
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	report, err := service.Sync(ctx, fakeConsulter{failDPS: true, failDocs: true}, fakeParsers(nil), domain.EnvHomologation)
	if err != nil || report.Failed != 1 || report.Unchanged != 1 {
		t.Fatalf("falhas: %+v, %v", report, err)
	}
	if _, err := (&Service{Repo: memory.New(domain.Issuer{})}).Sync(ctx, fakeConsulter{}, fakeParsers(nil), domain.EnvHomologation); err == nil || !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("sem emitente: %v", err)
	}
}
