package memory

import (
	"context"
	"time"

	"nfse/internal/domain"
)

// DemoIssuer é o emitente fictício do protótipo. Os CNPJs destes dados de teste
// são inventados (dígito verificador válido, empresa inexistente); os dados
// reais do dono só entram pelo banco local, nunca pelo código. O município é
// Barueri de propósito: é onde a SEFIN real devolve E0039, e o simulador
// reproduz essa resposta.
func DemoIssuer() domain.Issuer {
	return domain.Issuer{
		LegalName:             "EXEMPLO TECNOLOGIA LTDA",
		CNPJ:                  "11222333000181",
		MunicipalRegistration: "000000",
		CityCode:              "3505708",
		CityName:              "Barueri/SP",
		SimplesOption:         "me-epp",
		SimplesRegime:         "federal-municipal-sn",
		SpecialRegime:         "none",
		// ME/EPP exige a alíquota do Simples na DPS (E0712); 6,00% é fictício.
		SimplesTaxRate: 600,
		Environment:    domain.EnvHomologation,
		Series:         "1",
		NextDPSNumber:  1001,
	}
}

// NewSeeded cria o repositório com tomadores, modelos e notas fictícios, para
// o protótipo mostrar telas cheias sem expor cliente de verdade.
func NewSeeded(location *time.Location) *Repository {
	repo := New(DemoIssuer())
	ctx := context.Background()

	customers := []domain.Customer{
		{Name: "AURORA SISTEMAS LTDA", Document: "12345678000195", Email: "financeiro@aurora.example"},
		{Name: "BOREAL GAMES LTDA", Document: "23456789000195", Email: "contas@boreal.example"},
		{Name: "CEDRO SOFTWARE LTDA", Document: "34567890000130", Email: "nf@cedro.example"},
		{Name: "DUNA SERVIÇOS LTDA", Document: "45678901000175", Email: "fiscal@duna.example"},
		{
			Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100", Email: "notas@ipe.example",
			CityCode: "3549904", CityName: "São José dos Campos/SP", ZIP: "12200000", District: "CENTRO",
			Street: "RUA DAS FLORES", Number: "100", Complement: "SALA 1",
		},
	}
	ids := map[string]int64{}
	for i := range customers {
		// O erro é impossível aqui: ID zero sempre cria.
		_ = repo.SaveCustomer(ctx, &customers[i])
		ids[customers[i].Document] = customers[i].ID
	}

	pix := "Pagamento via PIX: financeiro@exemplo.example"
	templates := []domain.Template{
		{Name: "Aurora mensal", Description: "Referente a prestação de serviço no mês de {mes}/{ano}. " + pix, NationalTaxCode: "010701", DefaultAmount: 1700000, CustomerID: ids["12345678000195"]},
		{Name: "Boreal rescisão", Description: "Referente a rescisão de contrato, parcela 2/2. " + pix, NationalTaxCode: "010601", DefaultAmount: 2520000, CustomerID: ids["23456789000195"]},
		{Name: "Cedro mensal", Description: "Referente a prestação de serviço no mês de {mes}/{ano}. " + pix, NationalTaxCode: "010701", DefaultAmount: 2288500, CustomerID: ids["34567890000130"]},
		{Name: "Duna mensal", Description: "Referente a prestação de serviço no mês de {mes}/{ano}. " + pix, NationalTaxCode: "010601", DefaultAmount: 1000000, CustomerID: ids["45678901000175"]},
		{Name: "Ipê mensal", Description: "Referente a prestação de serviço no mês de {mes}/{ano}. " + pix, NationalTaxCode: "010701", MunicipalTaxCode: "220", NBS: "115013000", DefaultAmount: 1900000, ISSQNTaxation: "1", ISSQNRetention: "1", CustomerID: ids["56789012000100"]},
	}
	for i := range templates {
		templates[i].ServiceCityCode = "3505708"
		templates[i].ServiceCityName = "Barueri/SP"
		_ = repo.SaveTemplate(ctx, &templates[i])
	}

	at := func(month time.Month, day int) time.Time { return time.Date(2026, month, day, 10, 0, 0, 0, location) }
	demo := "Nota fictícia de teste."
	invoices := []domain.Invoice{
		{Environment: domain.EnvProduction, Series: "1", NFSeNumber: "35", CustomerID: ids["56789012000100"], TemplateID: templates[4].ID, Amount: 1900000, Competence: at(9, 5), IssuedAt: at(9, 5), Description: demo, ServiceCode: "010701", Status: domain.StatusAuthorized},
		{Environment: domain.EnvProduction, Series: "1", NFSeNumber: "37", CustomerID: ids["12345678000195"], TemplateID: templates[0].ID, Amount: 1700000, Competence: at(10, 2), IssuedAt: at(10, 2), Description: demo, ServiceCode: "010701", Status: domain.StatusAuthorized},
		{Environment: domain.EnvProduction, Series: "1", NFSeNumber: "38", CustomerID: ids["56789012000100"], TemplateID: templates[4].ID, Amount: 1900000, Competence: at(10, 3), IssuedAt: at(10, 3), Description: demo, ServiceCode: "010701", Status: domain.StatusAuthorized},
		{Environment: domain.EnvProduction, Series: "1", NFSeNumber: "39", CustomerID: ids["34567890000130"], TemplateID: templates[2].ID, Amount: 2288500, Competence: at(10, 5), IssuedAt: at(10, 5), Description: demo, ServiceCode: "010701", Status: domain.StatusAuthorized},
		{
			Environment: domain.EnvHomologation, Series: "1", DPSNumber: 1000, CustomerID: ids["56789012000100"], TemplateID: templates[4].ID,
			Amount: 1900000, Competence: at(10, 28), IssuedAt: time.Date(2026, 10, 28, 11, 57, 0, 0, location),
			Description: "Referente a prestação de serviço no mês de setembro/2026. " + pix, ServiceCode: "010701",
			Status: domain.StatusRejected, ErrorCode: "E0039",
			ErrorMessage: "O município emissor informado na DPS deve estar parametrizado para utilizar os emissores públicos nacionais, conforme parametrização do município no Sistema Nacional NFS-e.",
		},
	}
	for i := range invoices {
		_ = repo.SaveInvoice(ctx, &invoices[i])
	}
	return repo
}
