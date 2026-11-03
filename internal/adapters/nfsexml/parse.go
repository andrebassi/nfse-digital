// Package nfsexml lê um XML de NFS-e (autorizada) ou de DPS do padrão
// nacional e extrai o que serve de cadastro. Aceita os dois leiautes em
// vigor (produção de 09/02/2026 e produção restrita de 20/10/2026).
package nfsexml

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"nfse/internal/domain"
)

// ErrNotNFSe indica arquivo que não é NFS-e nem DPS do padrão nacional.
var ErrNotNFSe = errors.New("o arquivo não é um XML de NFS-e ou DPS do padrão nacional")

// Os tipos abaixo espelham só os campos usados. encoding/xml casa pelo nome
// local, então o namespace da SEFIN não precisa ser repetido em cada tag.

// nfseDoc é o envelope <NFSe>.
type nfseDoc struct {
	XMLName   xml.Name  `xml:"NFSe"`
	Inf       infNFSe   `xml:"infNFSe"`
	Signature *struct{} `xml:"Signature"`
}

// infNFSe é a parte da NFS-e gerada pela SEFIN.
type infNFSe struct {
	ID     string `xml:"Id,attr"`
	Number string `xml:"nNFSe"`
	CityOf string `xml:"xLocEmi"`
	Status string `xml:"cStat"`
	Emit   emit   `xml:"emit"`
	DPS    dpsDoc `xml:"DPS"`
}

// emit é o emitente como a SEFIN registrou.
type emit struct {
	CNPJ    string  `xml:"CNPJ"`
	IM      string  `xml:"IM"`
	Name    string  `xml:"xNome"`
	Address address `xml:"enderNac"`
}

// address cobre endereço nacional do emitente (enderNac) e do tomador (end).
type address struct {
	CityCode   string `xml:"cMun"`
	NatCity    string `xml:"endNac>cMun"`
	ZIP        string `xml:"CEP"`
	NatZIP     string `xml:"endNac>CEP"`
	Street     string `xml:"xLgr"`
	Number     string `xml:"nro"`
	Complement string `xml:"xCpl"`
	District   string `xml:"xBairro"`
}

// dpsDoc é o envelope <DPS>, sozinho ou dentro da NFS-e.
type dpsDoc struct {
	XMLName   xml.Name  `xml:"DPS"`
	Inf       infDPS    `xml:"infDPS"`
	Signature *struct{} `xml:"Signature"`
}

// infDPS são os campos da declaração.
type infDPS struct {
	ID          string `xml:"Id,attr"`
	Environment string `xml:"tpAmb"`
	IssuedAt    string `xml:"dhEmi"`
	Series      string `xml:"serie"`
	Number      int64  `xml:"nDPS"`
	Competence  string `xml:"dCompet"`
	CityCode    string `xml:"cLocEmi"`
	Prest       struct {
		CNPJ    string `xml:"CNPJ"`
		IM      string `xml:"IM"`
		RegTrib struct {
			SimplesOption string `xml:"opSimpNac"`
			SimplesRegime string `xml:"regApTribSN"`
			SpecialRegime string `xml:"regEspTrib"`
		} `xml:"regTrib"`
	} `xml:"prest"`
	Toma struct {
		CNPJ    string  `xml:"CNPJ"`
		CPF     string  `xml:"CPF"`
		Name    string  `xml:"xNome"`
		Email   string  `xml:"email"`
		Address address `xml:"end"`
	} `xml:"toma"`
	Serv struct {
		CityCode string `xml:"locPrest>cLocPrestacao"`
		CServ    struct {
			NationalTaxCode  string `xml:"cTribNac"`
			MunicipalTaxCode string `xml:"cTribMun"`
			Description      string `xml:"xDescServ"`
			NBS              string `xml:"cNBS"`
		} `xml:"cServ"`
		Complement string `xml:"infoCompl>xInfComp"`
	} `xml:"serv"`
	Valores struct {
		Amount    string `xml:"vServPrest>vServ"`
		Taxation  string `xml:"trib>tribMun>tribISSQN"`
		Retention string `xml:"trib>tribMun>tpRetISSQN"`
	} `xml:"valores"`
	IBSCBS struct {
		Operation string `xml:"cIndOp"`
		// Produção: valores/trib/gIBSCBS/{CST,cClassTrib}; restrita: valores/trib/{CST,cClassTrib}.
		NestedCST   string `xml:"valores>trib>gIBSCBS>CST"`
		NestedClass string `xml:"valores>trib>gIBSCBS>cClassTrib"`
		FlatCST     string `xml:"valores>trib>CST"`
		FlatClass   string `xml:"valores>trib>cClassTrib"`
	} `xml:"IBSCBS"`
}

// codes de volta: código da SEFIN para o valor guardado nas configurações.
var (
	simplesOptions = map[string]string{"1": "none", "2": "mei", "3": "me-epp"}
	simplesRegimes = map[string]string{"1": "federal-municipal-sn", "2": "federal-sn", "3": "outside-sn"}
	specialRegimes = map[string]string{"0": "none", "1": "cooperative-act", "2": "estimate", "3": "municipal-microenterprise",
		"4": "notary", "5": "self-employed", "6": "professional-society", "9": "other"}
)

// Parse lê o XML e devolve o que dá para importar. NFS-e autorizada vira
// nota "authorized" com chave e número; DPS solta vira só cadastro (nota vazia).
func Parse(data []byte) (domain.ImportedNFSe, error) {
	var root struct{ XMLName xml.Name }
	if err := xml.Unmarshal(data, &root); err != nil {
		return domain.ImportedNFSe{}, fmt.Errorf("%w: %v", ErrNotNFSe, err)
	}
	switch root.XMLName.Local {
	case "NFSe":
		var doc nfseDoc
		if err := xml.Unmarshal(data, &doc); err != nil {
			return domain.ImportedNFSe{}, fmt.Errorf("%w: %v", ErrNotNFSe, err)
		}
		imported, err := fromDPS(doc.Inf.DPS.Inf)
		if err != nil {
			return imported, err
		}
		imported.HasSignature = doc.Signature != nil
		// O emitente registrado pela SEFIN tem nome e endereço que a DPS não traz.
		imported.Issuer.LegalName = strings.TrimSpace(doc.Inf.Emit.Name)
		if imported.Issuer.MunicipalRegistration == "" {
			imported.Issuer.MunicipalRegistration = doc.Inf.Emit.IM
		}
		imported.Issuer.CityName = titleCity(doc.Inf.CityOf)
		imported.Invoice.Status = domain.StatusAuthorized
		imported.Invoice.NFSeNumber = doc.Inf.Number
		imported.Invoice.AccessKey = strings.TrimPrefix(doc.Inf.ID, "NFS")
		imported.Invoice.XML = data
		return imported, nil
	case "DPS":
		var doc dpsDoc
		if err := xml.Unmarshal(data, &doc); err != nil {
			return domain.ImportedNFSe{}, fmt.Errorf("%w: %v", ErrNotNFSe, err)
		}
		imported, err := fromDPS(doc.Inf)
		imported.HasSignature = doc.Signature != nil
		imported.Invoice = domain.Invoice{}
		return imported, err
	default:
		return domain.ImportedNFSe{}, fmt.Errorf("%w: elemento raiz <%s>", ErrNotNFSe, root.XMLName.Local)
	}
}

// fromDPS converte a declaração nos cadastros e na nota.
func fromDPS(inf infDPS) (domain.ImportedNFSe, error) {
	if inf.ID == "" || inf.Prest.CNPJ == "" {
		return domain.ImportedNFSe{}, fmt.Errorf("%w: DPS sem Id ou sem CNPJ do prestador", ErrNotNFSe)
	}
	amount, err := domain.ParseBRL(inf.Valores.Amount)
	if err != nil {
		return domain.ImportedNFSe{}, fmt.Errorf("valor do serviço ilegível: %w", err)
	}
	issuedAt, _ := time.Parse(time.RFC3339, inf.IssuedAt)
	competence, _ := time.Parse("2006-01-02", inf.Competence)
	environment := domain.EnvHomologation
	if inf.Environment == "1" {
		environment = domain.EnvProduction
	}
	toma := inf.Toma
	takerAddress := toma.Address
	imported := domain.ImportedNFSe{
		Issuer: domain.Issuer{
			CNPJ: inf.Prest.CNPJ, MunicipalRegistration: inf.Prest.IM, CityCode: inf.CityCode,
			SimplesOption: simplesOptions[inf.Prest.RegTrib.SimplesOption], SimplesRegime: simplesRegimes[inf.Prest.RegTrib.SimplesRegime],
			SpecialRegime: specialRegimes[inf.Prest.RegTrib.SpecialRegime], Environment: domain.EnvHomologation,
			Series: inf.Series, NextDPSNumber: inf.Number + 1,
		},
		Customer: domain.Customer{
			Document: toma.CNPJ + toma.CPF, Name: strings.TrimSpace(toma.Name), Email: toma.Email,
			CityCode: takerAddress.NatCity, ZIP: takerAddress.NatZIP, Street: takerAddress.Street, Number: takerAddress.Number,
			Complement: takerAddress.Complement, District: takerAddress.District,
		},
		Template: domain.Template{
			Description: strings.TrimSpace(inf.Serv.CServ.Description), NationalTaxCode: inf.Serv.CServ.NationalTaxCode,
			MunicipalTaxCode: inf.Serv.CServ.MunicipalTaxCode, NBS: inf.Serv.CServ.NBS, ServiceCityCode: inf.Serv.CityCode,
			DefaultAmount: amount, ISSQNTaxation: inf.Valores.Taxation, ISSQNRetention: inf.Valores.Retention,
			IBSCBSOperation: inf.IBSCBS.Operation, IBSCBSCST: inf.IBSCBS.NestedCST + inf.IBSCBS.FlatCST,
			IBSCBSClass: inf.IBSCBS.NestedClass + inf.IBSCBS.FlatClass,
		},
		Invoice: domain.Invoice{
			Environment: environment, Series: inf.Series, DPSNumber: inf.Number, Amount: amount, Competence: competence,
			IssuedAt: issuedAt, Description: strings.TrimSpace(inf.Serv.CServ.Description), ServiceCode: inf.Serv.CServ.NationalTaxCode,
			Status: domain.StatusPending,
		},
	}
	return imported, nil
}

// connectors ficam minúsculas no nome do município ("São José dos Campos").
var connectors = map[string]bool{"da": true, "das": true, "de": true, "do": true, "dos": true, "e": true}

// titleCity passa "BARUERI" para "Barueri", com preposição em minúscula.
func titleCity(name string) string {
	words := strings.Fields(strings.ToLower(name))
	for i, word := range words {
		if !connectors[word] {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}
