package nfsexml

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// DANFSeData são os campos impressos no DANFSe (NT-008 v1.02), lidos do XML
// da NFS-e autorizada. Texto pronto para imprimir: datas e valores já
// formatados ficam por conta do renderizador.
type DANFSeData struct {
	AccessKey        string
	Number           string
	DPSNumber        string
	DPSSeries        string
	Competence       time.Time
	IssuedAt         time.Time
	ProcessedAt      time.Time
	Environment      string
	Provider         DANFSeParty
	Taker            DANFSeParty
	ServiceCode      string
	ServiceName      string
	CityCode         string
	MunicipalTaxCode string
	Description      string
	Complement       string
	IncidenceCity    string
	Amount           string
	BaseValue        string
	Rate             string
	ISSQN            string
	NetValue         string
	Retention        string
	IBSCBSTotal      string
	IBSValue         string
	CBSValue         string
	// Watermark, quando preenchido, sai em diagonal sobre a página: é o
	// "SIMULAÇÃO · SEM VALOR FISCAL" do DANFSe de exemplo do e-mail de teste.
	// Nunca vem do XML: só quem monta o exemplo preenche.
	Watermark string
}

// DANFSeParty é prestador ou tomador como aparecem no documento.
type DANFSeParty struct {
	Document     string
	Registration string
	Name         string
	Address      string
	City         string
	Email        string
}

// danfseDoc espelha os campos do XML usados no DANFSe.
type danfseDoc struct {
	Inf struct {
		ID            string `xml:"Id,attr"`
		Number        string `xml:"nNFSe"`
		CityName      string `xml:"xLocEmi"`
		IncidenceName string `xml:"xLocIncid"`
		ServiceName   string `xml:"xTribNac"`
		AmbGer        string `xml:"ambGer"`
		ProcessedAt   string `xml:"dhProc"`
		Emit          struct {
			CNPJ     string `xml:"CNPJ"`
			IM       string `xml:"IM"`
			Name     string `xml:"xNome"`
			Street   string `xml:"enderNac>xLgr"`
			Number   string `xml:"enderNac>nro"`
			Compl    string `xml:"enderNac>xCpl"`
			District string `xml:"enderNac>xBairro"`
			UF       string `xml:"enderNac>UF"`
			ZIP      string `xml:"enderNac>CEP"`
			Email    string `xml:"email"`
		} `xml:"emit"`
		Values struct {
			Base  string `xml:"vBC"`
			Rate  string `xml:"pAliqAplic"`
			ISSQN string `xml:"vISSQN"`
			Net   string `xml:"vLiq"`
		} `xml:"valores"`
		IBSCBS struct {
			Total string `xml:"totCIBS>vTotNF"`
			IBS   string `xml:"totCIBS>gIBS>vIBSTot"`
			CBS   string `xml:"totCIBS>gCBS>vCBS"`
		} `xml:"IBSCBS"`
		DPS struct {
			Inf infDPS `xml:"infDPS"`
		} `xml:"DPS"`
	} `xml:"infNFSe"`
}

// ParseDANFSe lê do XML da NFS-e os campos do DANFSe.
func ParseDANFSe(data []byte) (DANFSeData, error) {
	var doc danfseDoc
	if err := xml.Unmarshal(data, &doc); err != nil || doc.Inf.ID == "" {
		return DANFSeData{}, fmt.Errorf("%w: não é uma NFS-e autorizada", ErrNotNFSe)
	}
	inf, dps := doc.Inf, doc.Inf.DPS.Inf
	issued, _ := time.Parse(time.RFC3339, dps.IssuedAt)
	processed, _ := time.Parse(time.RFC3339, inf.ProcessedAt)
	competence, _ := time.Parse("2006-01-02", dps.Competence)
	taker := dps.Toma
	takerAddress := joinNonEmpty(", ", taker.Address.Street, taker.Address.Number, taker.Address.Complement, taker.Address.District)
	environment := "Produção"
	if dps.Environment == "2" {
		environment = "Homologação — sem valor fiscal"
	}
	return DANFSeData{
		AccessKey: strings.TrimPrefix(inf.ID, "NFS"), Number: inf.Number, DPSNumber: fmt.Sprint(dps.Number), DPSSeries: dps.Series,
		Competence: competence, IssuedAt: issued, ProcessedAt: processed, Environment: environment,
		Provider: DANFSeParty{
			Document: inf.Emit.CNPJ, Registration: inf.Emit.IM, Name: inf.Emit.Name,
			Address: joinNonEmpty(", ", inf.Emit.Street, inf.Emit.Number, inf.Emit.Compl, inf.Emit.District),
			City:    joinNonEmpty(" / ", titleCity(inf.CityName), inf.Emit.UF), Email: inf.Emit.Email,
		},
		Taker: DANFSeParty{
			Document: taker.CNPJ + taker.CPF, Name: taker.Name, Address: takerAddress, City: taker.Address.NatCity, Email: taker.Email,
		},
		ServiceCode: dps.Serv.CServ.NationalTaxCode, ServiceName: inf.ServiceName, CityCode: dps.Serv.CityCode,
		MunicipalTaxCode: dps.Serv.CServ.MunicipalTaxCode, Description: dps.Serv.CServ.Description, Complement: dps.Serv.Complement,
		IncidenceCity: titleCity(inf.IncidenceName), Amount: dps.Valores.Amount, BaseValue: inf.Values.Base, Rate: inf.Values.Rate,
		ISSQN: inf.Values.ISSQN, NetValue: inf.Values.Net, Retention: map[string]string{"1": "Não retido", "2": "Retido pelo tomador", "3": "Retido pelo intermediário"}[dps.Valores.Retention],
		IBSCBSTotal: inf.IBSCBS.Total, IBSValue: inf.IBSCBS.IBS, CBSValue: inf.IBSCBS.CBS,
	}, nil
}

// joinNonEmpty junta só as partes não vazias.
func joinNonEmpty(separator string, parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, strings.TrimSpace(part))
		}
	}
	return strings.Join(kept, separator)
}
