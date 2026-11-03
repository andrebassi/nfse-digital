package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// fakePortal devolve lotes fixos, um por chamada, e registra os NSU pedidos.
type fakePortal struct {
	batches []ports.DFeBatch
	asked   []int64
	err     error
}

// Distribution devolve o próximo lote configurado.
func (f *fakePortal) Distribution(_ context.Context, afterNSU int64) (ports.DFeBatch, error) {
	f.asked = append(f.asked, afterNSU)
	if f.err != nil {
		return ports.DFeBatch{}, f.err
	}
	if len(f.asked) > len(f.batches) {
		return ports.DFeBatch{}, nil
	}
	return f.batches[len(f.asked)-1], nil
}

// note monta uma NFS-e importada fictícia do emitente, tomador e mês dados.
func note(issuerCNPJ, takerCNPJ, key string, month time.Month) domain.ImportedNFSe {
	imported := importedFixture()
	imported.Issuer.CNPJ = issuerCNPJ
	imported.Customer.Document = takerCNPJ
	imported.Invoice.AccessKey = key
	imported.Invoice.Competence = time.Date(2026, month, 1, 0, 0, 0, 0, brt)
	imported.Invoice.IssuedAt = time.Date(2026, month, 5, 0, 0, 0, 0, brt)
	return imported
}

// fakeParsers interpreta os "XML" do teste: "nfse:<chave>" e "evento:<chave>".
func fakeParsers(notes map[string]domain.ImportedNFSe) PortalParsers {
	return PortalParsers{
		NFSe: func(data []byte) (domain.ImportedNFSe, error) {
			note, ok := notes[strings.TrimPrefix(string(data), "nfse:")]
			if !ok {
				return note, errors.New("ilegível")
			}
			return note, nil
		},
		Event: func(data []byte) (string, bool, error) {
			return strings.TrimPrefix(string(data), "evento:"), strings.HasPrefix(string(data), "evento:"), nil
		},
	}
}

// TestScanAndApplyPortal: separa emitidas de recebidas, aplica cancelamento,
// agrupa por tomador e serviço, pagina pelo NSU e não duplica ao reaplicar.
func TestScanAndApplyPortal(t *testing.T) {
	const issuer, taker, other = "11222333000181", "56789012000100", "34567890000130"
	notes := map[string]domain.ImportedNFSe{
		"K1": note(issuer, taker, "K1", time.August),
		"K2": note(issuer, taker, "K2", time.September),
		"K3": note(issuer, taker, "K3", time.October),
		"R1": note(other, issuer, "R1", time.October),
		"S1": note(issuer, "45678901000175", "S1", time.March),
	}
	portal := &fakePortal{batches: []ports.DFeBatch{
		{More: true, Documents: []ports.DFeDocument{{NSU: 1, Type: "NFSE", XML: []byte("nfse:K1")}, {NSU: 2, Type: "NFSE", XML: []byte("nfse:K2")}, {NSU: 3, Type: "NFSE", XML: []byte("nfse:R1")}}},
		{More: true, Documents: []ports.DFeDocument{{NSU: 4, Type: "NFSE", XML: []byte("nfse:K3")}, {NSU: 5, Type: "EVENTO", XML: []byte("evento:K2")}, {NSU: 6, Type: "NFSE", XML: []byte("nfse:quebrada")}, {NSU: 7, Type: "NFSE", XML: []byte("nfse:S1")}}},
	}}
	repo := memory.New(domain.Issuer{})
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	scan, err := service.ScanPortal(context.Background(), portal, fakeParsers(notes), issuer)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Emitted != 4 || scan.AsTaker != 1 || scan.Cancelled != 1 || scan.LastNSU != 7 || len(scan.Groups) != 2 {
		t.Fatalf("varredura: %+v", scan)
	}
	if got := portal.asked; len(got) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 7 {
		t.Fatalf("NSU pedidos: %v", got)
	}
	main := scan.Groups[0]
	if main.Count != 3 || !main.Monthly || main.LastNote.Invoice.AccessKey != "K3" || scan.Groups[1].Monthly {
		t.Fatalf("grupos: %+v", scan.Groups)
	}

	imported, err := service.ApplyPortal(context.Background(), scan, []string{main.Key})
	if err != nil || imported != 3 {
		t.Fatalf("aplicar: %d, %v", imported, err)
	}
	invoices, _ := repo.Invoices(context.Background())
	cancelled := 0
	for _, invoice := range invoices {
		if invoice.Status == domain.StatusCancelled {
			cancelled++
		}
	}
	if len(invoices) != 3 || cancelled != 1 {
		t.Fatalf("notas gravadas: %d, canceladas %d", len(invoices), cancelled)
	}
	if again, _ := service.ApplyPortal(context.Background(), scan, []string{"all"}); again != 1 {
		t.Fatalf("reaplicar com all deveria importar só o grupo novo: %d", again)
	}
	rescan, _ := service.ScanPortal(context.Background(), &fakePortal{batches: portal.batches}, fakeParsers(notes), issuer)
	if !rescan.Groups[0].Known {
		t.Fatal("tomador já importado deveria aparecer como conhecido")
	}
}

// TestApplyPortalIdempotent: importar de novo — em sequência, cinco de uma
// vez em paralelo (clique repetido, duas abas) e depois de o dono renomear o
// modelo — nunca duplica nota, tomador nem modelo, e o nome que ele deu fica.
// Também confere o que a tela mostra: lotes lidos e as mais recentes.
func TestApplyPortalIdempotent(t *testing.T) {
	const issuer, taker = "11222333000181", "56789012000100"
	notes := map[string]domain.ImportedNFSe{
		"K1": note(issuer, taker, "K1", time.August),
		"K2": note(issuer, taker, "K2", time.September),
		"S1": note(issuer, "45678901000175", "S1", time.March),
	}
	batches := []ports.DFeBatch{
		{More: true, Documents: []ports.DFeDocument{{NSU: 1, Type: "NFSE", XML: []byte("nfse:K1")}, {NSU: 2, Type: "NFSE", XML: []byte("nfse:S1")}}},
		{Documents: []ports.DFeDocument{{NSU: 3, Type: "NFSE", XML: []byte("nfse:K2")}}},
	}
	repo := memory.New(domain.Issuer{})
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	ctx := context.Background()
	scan, err := service.ScanPortal(ctx, &fakePortal{batches: batches}, fakeParsers(notes), issuer)
	if err != nil || scan.Batches != 2 {
		t.Fatalf("lotes lidos: %d, %v", scan.Batches, err)
	}
	if recent := scan.Recent(2); len(recent) != 2 || recent[0].Invoice.AccessKey != "K2" || recent[1].Invoice.AccessKey != "K1" {
		t.Fatalf("mais recentes: %+v", recent)
	}
	if len(scan.Recent(10)) != 3 {
		t.Fatal("Recent além do total deveria devolver todas")
	}

	counts := func() (int, int, int) {
		invoices, _ := repo.Invoices(ctx)
		customers, _ := repo.Customers(ctx, "")
		templates, _ := repo.Templates(ctx, "")
		return len(invoices), len(customers), len(templates)
	}
	if imported, err := service.ApplyPortal(ctx, scan, []string{"all"}); err != nil || imported != 3 {
		t.Fatalf("primeira importação: %d, %v", imported, err)
	}
	invoices, customers, templates := counts()
	if invoices != 3 || customers != 2 || templates != 2 {
		t.Fatalf("depois da primeira: %d notas, %d tomadores, %d modelos", invoices, customers, templates)
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if imported, err := service.ApplyPortal(ctx, scan, []string{"all"}); err != nil || imported != 0 {
				t.Errorf("reimportação em paralelo gravou %d (%v)", imported, err)
			}
		})
	}
	wg.Wait()

	all, _ := repo.Templates(ctx, "")
	renamed := all[0]
	renamed.Name = "Nome que o dono escolheu"
	_ = repo.SaveTemplate(ctx, &renamed)
	if imported, err := service.ApplyPortal(ctx, scan, []string{"all"}); err != nil || imported != 0 {
		t.Fatalf("depois de renomear: %d, %v", imported, err)
	}
	if a, b, c := counts(); a != invoices || b != customers || c != templates {
		t.Fatalf("duplicou: %d notas, %d tomadores, %d modelos (eram %d, %d, %d)", a, b, c, invoices, customers, templates)
	}
	if kept, _ := repo.Template(ctx, renamed.ID); kept.Name != "Nome que o dono escolheu" {
		t.Fatalf("a importação trocou o nome que o dono deu: %q", kept.Name)
	}
}

// slowRepo demora um pouco no "já existe?" das notas: abre a janela em que
// duas importações sem fila veriam o banco vazio e gravariam em dobro.
type slowRepo struct {
	*memory.Repository
}

// Invoices espera 5 ms antes de responder.
func (s slowRepo) Invoices(ctx context.Context) ([]domain.Invoice, error) {
	time.Sleep(5 * time.Millisecond)
	return s.Repository.Invoices(ctx)
}

// Customers espera 5 ms: a outra janela, a do "tomador já existe?".
func (s slowRepo) Customers(ctx context.Context, query string) ([]domain.Customer, error) {
	time.Sleep(5 * time.Millisecond)
	return s.Repository.Customers(ctx, query)
}

// TestApplyPortalParallelFirstImport: cinco importações da mesma conta ao
// mesmo tempo num banco vazio (a automática depois do certificado + cliques)
// gravam cada nota uma vez só — é a fila por conta que garante.
func TestApplyPortalParallelFirstImport(t *testing.T) {
	const issuer, taker = "11222333000181", "56789012000100"
	notes := map[string]domain.ImportedNFSe{"K1": note(issuer, taker, "K1", time.August), "K2": note(issuer, taker, "K2", time.September)}
	batches := []ports.DFeBatch{{Documents: []ports.DFeDocument{{NSU: 1, Type: "NFSE", XML: []byte("nfse:K1")}, {NSU: 2, Type: "NFSE", XML: []byte("nfse:K2")}}}}
	// 20 rodadas, cada uma num banco novo: a corrida depende do escalonador,
	// e uma rodada só deixava o defeito passar batido às vezes.
	for round := range 20 {
		repo := slowRepo{memory.New(domain.Issuer{})}
		service := &Service{Repo: repo, Now: fixedNow, Location: brt}
		ctx := context.Background()
		scan, _ := service.ScanPortal(ctx, &fakePortal{batches: batches}, fakeParsers(notes), issuer)
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				// Nenhuma pode falhar: sem a fila, as que chegam depois batem no
				// índice único e o dono veria erro numa importação que deu certo.
				if _, err := service.ApplyPortal(ctx, scan, []string{"all"}); err != nil {
					t.Errorf("rodada %d: importação simultânea falhou: %v", round, err)
				}
			})
		}
		wg.Wait()
		if invoices, _ := repo.Invoices(ctx); len(invoices) != 2 {
			t.Fatalf("rodada %d: gravou %d notas, eram 2", round, len(invoices))
		}
	}
}

// TestScanPortalErrors: erro do ADN e erro ao gravar param a operação.
func TestScanPortalErrors(t *testing.T) {
	service := &Service{Repo: memory.New(domain.Issuer{}), Now: fixedNow, Location: brt}
	if _, err := service.ScanPortal(context.Background(), &fakePortal{err: errors.New("ADN fora")}, fakeParsers(nil), "x"); err == nil {
		t.Fatal("erro do ADN deveria subir")
	}
	bad := note("11222333000181", "123", "B1", time.May)
	scan := PortalScan{Groups: []PortalGroup{{Key: "g", Count: 1, notes: []domain.ImportedNFSe{bad}}}}
	if _, err := service.ApplyPortal(context.Background(), scan, []string{"g"}); err == nil {
		t.Fatal("tomador inválido deveria falhar")
	}
}
