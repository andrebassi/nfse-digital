package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
)

// importedFixture devolve uma NFS-e importada fictícia, autorizada.
func importedFixture() domain.ImportedNFSe {
	return domain.ImportedNFSe{
		Issuer: domain.Issuer{
			LegalName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181", CityCode: "3505708", SimplesOption: "me-epp",
			SpecialRegime: "none", Environment: domain.EnvHomologation, Series: "1", NextDPSNumber: 2000,
		},
		Customer: domain.Customer{Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100", Email: "notas@ipe.example", Street: "RUA DAS FLORES"},
		Template: domain.Template{Description: "Suporte", NationalTaxCode: "010701", ServiceCityCode: "3505708", DefaultAmount: 1900000},
		Invoice: domain.Invoice{
			Environment: domain.EnvProduction, Series: "1", DPSNumber: 1999, Amount: 1900000, Status: domain.StatusAuthorized,
			NFSeNumber: "38", AccessKey: "35057081211222333000181000000000003826101922066208", IssuedAt: time.Date(2026, 10, 5, 19, 22, 0, 0, brt),
		},
	}
}

// TestImportIntoEmptyDatabase: banco vazio ganha emitente, tomador, modelo e a
// nota; importar de novo não duplica nada e a numeração não anda para trás.
func TestImportIntoEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(domain.Issuer{})
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	report, err := service.Import(ctx, importedFixture(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issuer != "criado a partir do XML" || report.Customer != "criado: IPÊ INFORMÁTICA LTDA" ||
		report.Template != "criado: Importado — IPÊ INFORMÁTICA LTDA" || report.Invoice != "importada: nº 38" {
		t.Fatalf("relatório: %+v", report)
	}
	invoices, _ := repo.Invoices(ctx)
	if len(invoices) != 1 || invoices[0].DPSNumber != 0 || invoices[0].CustomerID == 0 || invoices[0].TemplateID == 0 {
		t.Fatalf("nota: %+v", invoices)
	}

	again, err := service.Import(ctx, importedFixture(), "", false)
	if err != nil || again.Issuer != "mantido (já configurado)" || again.Customer != "atualizado: IPÊ INFORMÁTICA LTDA" ||
		again.Template != "atualizado: Importado — IPÊ INFORMÁTICA LTDA" || again.Invoice != "já existia: nº 38" {
		t.Fatalf("segunda importação: %+v, %v", again, err)
	}
	if customers, _ := repo.Customers(ctx, ""); len(customers) != 1 {
		t.Fatal("tomador duplicado")
	}
}

// TestImportOverwriteKeepsEnvironmentAndNumber: sobrescrever o emitente mantém
// o ambiente e o maior próximo número.
func TestImportOverwriteKeepsEnvironmentAndNumber(t *testing.T) {
	ctx := context.Background()
	existing := memory.DemoIssuer()
	existing.NextDPSNumber, existing.Environment = 5000, domain.EnvHomologation
	repo := memory.New(existing)
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	imported := importedFixture()
	imported.Issuer.LegalName = "NOME NOVO"
	report, err := service.Import(ctx, imported, "Ipê mensal", true)
	if err != nil || report.Issuer != "atualizado a partir do XML" || report.Template != "criado: Ipê mensal" {
		t.Fatalf("%+v, %v", report, err)
	}
	if issuer, _ := repo.Issuer(ctx); issuer.LegalName != "NOME NOVO" || issuer.NextDPSNumber != 5000 {
		t.Fatalf("emitente: %+v", issuer)
	}
}

// TestImportRefusals: outro CNPJ sem sobrescrever, tomador inválido e DPS sem nota.
func TestImportRefusals(t *testing.T) {
	ctx := context.Background()
	other := memory.DemoIssuer()
	other.CNPJ = "56789012000100"
	service := &Service{Repo: memory.New(other), Now: fixedNow, Location: brt}
	if _, err := service.Import(ctx, importedFixture(), "", false); !errors.Is(err, ErrOtherIssuer) {
		t.Fatalf("outro emitente: %v", err)
	}
	bad := importedFixture()
	bad.Customer.Document = "123"
	if _, err := (&Service{Repo: memory.New(domain.Issuer{})}).Import(ctx, bad, "", false); err == nil {
		t.Fatal("tomador inválido passou")
	}
	dps := importedFixture()
	dps.Invoice = domain.Invoice{}
	report, err := (&Service{Repo: memory.New(domain.Issuer{})}).Import(ctx, dps, "", false)
	if err != nil || report.Invoice != "sem nota (o arquivo é uma DPS, não uma NFS-e autorizada)" {
		t.Fatalf("DPS: %+v, %v", report, err)
	}
}
