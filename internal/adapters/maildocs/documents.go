// Package maildocs gera os anexos do e-mail da nota: o DANFSe em PDF e o XML
// da NFS-e. Na nota real, o XML é o da SEFIN (guardado na autorização ou
// buscado pela chave); no e-mail de teste, os dois são de exemplo e marcados
// como simulação — nenhum documento de exemplo sai parecendo nota de verdade.
package maildocs

import (
	"context"
	"fmt"
	"html"
	"strings"

	"nfse/internal/adapters/danfse"
	"nfse/internal/adapters/nfsexml"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// SimulationMark é a marca do DANFSe e do XML de exemplo.
const SimulationMark = "SIMULAÇÃO · SEM VALOR FISCAL"

// Documents cumpre ports.InvoiceDocuments. FetchXML busca o XML na SEFIN pela
// chave quando a nota não o tem guardado (nota importada antes de a
// autorização guardar o XML); nil faz essa nota falhar com mensagem clara.
type Documents struct {
	FetchXML func(ctx context.Context, invoice domain.Invoice) ([]byte, error)
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.InvoiceDocuments = Documents{}

// Invoice devolve o DANFSe e o XML da nota autorizada.
func (d Documents) Invoice(ctx context.Context, invoice domain.Invoice) ([]byte, []byte, error) {
	xml := invoice.XML
	if len(xml) == 0 {
		if d.FetchXML == nil {
			return nil, nil, fmt.Errorf("a nota #%d não tem o XML guardado", invoice.ID)
		}
		var err error
		if xml, err = d.FetchXML(ctx, invoice); err != nil {
			return nil, nil, fmt.Errorf("buscar o XML na SEFIN: %w", err)
		}
	}
	data, err := nfsexml.ParseDANFSe(xml)
	if err != nil {
		return nil, nil, err
	}
	pdf, err := danfse.Render(data)
	return pdf, xml, err
}

// Sample devolve o DANFSe e o XML de exemplo, com a marca de simulação, a
// partir do emitente e do tomador de verdade e da nota de exemplo.
func (Documents) Sample(_ context.Context, issuer domain.Issuer, customer domain.Customer, invoice domain.Invoice) ([]byte, []byte, error) {
	amount := invoice.Amount.FormatDecimal()
	data := nfsexml.DANFSeData{
		AccessKey: invoice.AccessKey, Number: invoice.NFSeNumber, DPSNumber: invoice.NFSeNumber, DPSSeries: "900",
		Competence: invoice.Competence, IssuedAt: invoice.IssuedAt, ProcessedAt: invoice.IssuedAt,
		Environment: "Homologação (exemplo)",
		Provider:    nfsexml.DANFSeParty{Document: issuer.CNPJ, Registration: issuer.MunicipalRegistration, Name: issuer.LegalName, City: issuer.CityName},
		Taker:       nfsexml.DANFSeParty{Document: customer.Document, Name: customer.Name, Email: customer.Email, City: customer.CityCode},
		Description: invoice.Description, Amount: amount, BaseValue: amount, NetValue: amount,
		Complement: "Documento de exemplo gerado para o e-mail de teste. " + SimulationMark + ".",
		Watermark:  SimulationMark,
	}
	pdf, err := danfse.Render(data)
	if err != nil {
		return nil, nil, err
	}
	return pdf, sampleXML(issuer, customer, invoice), nil
}

// sampleXML é um XML curto no formato da NFS-e, com a marca no comentário e
// num campo próprio: quem abrir o anexo vê na primeira linha que é exemplo.
func sampleXML(issuer domain.Issuer, customer domain.Customer, invoice domain.Invoice) []byte {
	escape := html.EscapeString
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString("<!-- " + SimulationMark + ": XML de exemplo do e-mail de teste, não é uma NFS-e -->\n")
	b.WriteString(`<NFSe xmlns="http://www.sped.fazenda.gov.br/nfse" simulacao="true">` + "\n")
	b.WriteString("  <infNFSe Id=\"NFS" + escape(invoice.AccessKey) + "\">\n")
	b.WriteString("    <xObs>" + SimulationMark + "</xObs>\n")
	b.WriteString("    <nNFSe>" + escape(invoice.NFSeNumber) + "</nNFSe>\n")
	b.WriteString("    <emit><CNPJ>" + escape(issuer.CNPJ) + "</CNPJ><xNome>" + escape(issuer.LegalName) + "</xNome></emit>\n")
	b.WriteString("    <toma><CNPJ>" + escape(customer.Document) + "</CNPJ><xNome>" + escape(customer.Name) + "</xNome></toma>\n")
	b.WriteString("    <vServ>" + invoice.Amount.FormatDecimal() + "</vServ>\n")
	b.WriteString("    <xDescServ>" + escape(invoice.Description) + "</xDescServ>\n")
	b.WriteString("  </infNFSe>\n</NFSe>\n")
	return []byte(b.String())
}
