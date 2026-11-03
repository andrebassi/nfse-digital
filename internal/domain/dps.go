package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Códigos padrão do grupo IBS/CBS, iguais aos da NFS-e real de 05/10/2026
// (docs/sefin-notes.md, seção 6): finalidade normal, operação 050101,
// destinatário igual ao tomador, CST 000 e classificação 000001.
const (
	DefaultIBSCBSOperation = "050101"
	DefaultIBSCBSCST       = "000"
	DefaultIBSCBSClass     = "000001"
)

// maxAppVersionLength é o limite do verAplic no XSD (TSVerAplic, 20 posições).
// Medido em 29/10/2026: "nfse-digital 0.1.0-prototype" (28) voltou E1235.
const maxAppVersionLength = 20

// ErrForeignNotSupported recusa tomador no exterior: o grupo de comércio
// exterior da DPS ainda não foi implementado.
var ErrForeignNotSupported = errors.New("tomador no exterior ainda não é suportado na DPS")

// DPS é a Declaração de Prestação de Serviço já com os códigos da SEFIN,
// pronta para virar XML. Não sabe nada de XML nem de assinatura.
type DPS struct {
	ID          string
	Environment Environment
	IssuedAt    time.Time
	AppVersion  string
	Series      string
	Number      int64
	Competence  time.Time
	CityCode    string
	Provider    DPSProvider
	Taker       DPSTaker
	Service     DPSService
	Values      DPSValues
	IBSCBS      DPSIBSCBS
}

// DPSProvider é o prestador (o emitente) com os códigos do regime tributário.
type DPSProvider struct {
	CNPJ                  string
	MunicipalRegistration string
	SimplesOption         string
	SimplesRegime         string
	SpecialRegime         string
}

// DPSTaker é o tomador; endereço só vai quando o cadastro tem município e CEP.
type DPSTaker struct {
	DocumentKind DocumentKind
	Document     string
	Name         string
	Email        string
	Address      *DPSAddress
}

// DPSAddress é o endereço nacional do tomador.
type DPSAddress struct {
	CityCode   string
	ZIP        string
	Street     string
	Number     string
	Complement string
	District   string
}

// DPSService é o serviço prestado.
type DPSService struct {
	CityCode         string
	NationalTaxCode  string
	MunicipalTaxCode string
	Description      string
	NBS              string
}

// DPSValues são o valor e a tributação municipal.
type DPSValues struct {
	Amount         Money
	ISSQNTaxation  string
	ISSQNRetention string
	// SimplesTaxRate vai como pTotTribSN quando preenchido (ME/EPP); vazio, a
	// DPS manda indTotTrib 0, a única forma aceita para os outros regimes.
	SimplesTaxRate string
}

// DPSIBSCBS é o grupo da reforma tributária (IBS/CBS).
type DPSIBSCBS struct {
	Operation string
	CST       string
	Class     string
}

// simplesOptionCodes traduz a opção pelo Simples da tela para o código da
// SEFIN (TSOpSimpNac): 1 não optante, 2 MEI, 3 ME/EPP.
var simplesOptionCodes = map[string]string{"none": "1", "mei": "2", "me-epp": "3"}

// simplesRegimeCodes traduz o regime de apuração (TSRegimeApuracaoSimpNac).
var simplesRegimeCodes = map[string]string{"federal-municipal-sn": "1", "federal-sn": "2", "outside-sn": "3"}

// specialRegimeCodes traduz o regime especial (TSRegEspTrib), com os oito
// códigos do XSD. "cooperative" é o valor antigo da tela, que tinha "Ato
// cooperado" e "Cooperativa" como opções separadas mandando o MESMO código 1;
// fica aceito para conta já gravada com ele, mas a tela não o oferece mais.
var specialRegimeCodes = map[string]string{
	"": "0", "none": "0", "cooperative-act": "1", "cooperative": "1", "estimate": "2",
	"municipal-microenterprise": "3", "notary": "4", "self-employed": "5", "professional-society": "6", "other": "9",
}

// DPSID monta o identificador de 45 posições do XSD (TSIdDPS): "DPS" +
// município (7) + tipo de inscrição (1: CPF, 2: CNPJ) + inscrição (14, CPF com
// zeros à esquerda) + série (5) + número (15).
func DPSID(cityCode string, kind DocumentKind, document, series string, number int64) string {
	inscriptionType := "2"
	if kind == KindCPF {
		inscriptionType = "1"
	}
	return fmt.Sprintf("DPS%s%s%014s%05s%015d", cityCode, inscriptionType, document, series, number)
}

// BuildDPS junta emitente, tomador, modelo e nota na DPS. Erros de cadastro
// voltam todos juntos como FieldErrors, antes de qualquer ida à SEFIN.
func BuildDPS(issuer Issuer, customer Customer, template Template, invoice Invoice, appVersion string) (DPS, error) {
	if customer.Foreign {
		return DPS{}, ErrForeignNotSupported
	}
	errs := FieldErrors{}
	if err := issuer.Validate(); err != nil {
		errs["issuer"] = "emitente incompleto: " + err.Error()
	}
	takerDocument, err := ParseDocument(customer.Document)
	if err != nil {
		errs["customer"] = "CPF/CNPJ do tomador inválido"
	}
	if !isDigits(template.NationalTaxCode, 6) {
		errs["template"] = "código de tributação nacional do modelo inválido"
	}
	if invoice.Amount <= 0 {
		errs["amount"] = "o valor precisa ser maior que zero"
	}
	if invoice.DPSNumber <= 0 {
		errs["dps_number"] = "número da DPS não reservado"
	}
	if strings.TrimSpace(invoice.Description) == "" {
		errs["description"] = "a descrição do serviço é obrigatória"
	}
	simplesOption, ok := simplesOptionCodes[issuer.SimplesOption]
	if !ok {
		errs["simples_option"] = "opção pelo Simples desconhecida"
	}
	specialRegime, ok := specialRegimeCodes[issuer.SpecialRegime]
	if !ok {
		errs["special_regime"] = "regime especial desconhecido"
	}
	// O regime de apuração só existe para ME/EPP; para os outros fica vazio.
	simplesRegime := ""
	if simplesOption == "3" {
		if simplesRegime, ok = simplesRegimeCodes[issuer.SimplesRegime]; !ok {
			errs["simples_regime"] = "regime de apuração do Simples desconhecido"
		}
	}
	// ME/EPP tem de informar o percentual do Simples: indTotTrib é recusado
	// pela SEFIN com E0712 (medido em produção, 02/11/2026). O do emitente é
	// o padrão; o "% tributos Simples" do modelo, quando preenchido, vale para
	// as notas daquele serviço (anexo do Simples diferente, por exemplo).
	simplesTaxRate := ""
	if simplesOption == "3" {
		rate := issuer.SimplesTaxRate
		if strings.TrimSpace(template.SimplesTaxRate) != "" {
			parsed, err := ParsePercent(template.SimplesTaxRate)
			if err != nil {
				errs["simples_tax_rate"] = "o % tributos Simples do modelo é inválido: " + err.Error()
			}
			rate = parsed
		}
		if rate <= 0 && errs["simples_tax_rate"] == "" {
			errs["simples_tax_rate"] = "informe a alíquota aproximada do Simples (%) nas configurações do emitente"
		}
		simplesTaxRate = rate.FormatDecimal()
	}
	if len(errs) > 0 {
		return DPS{}, errs
	}

	dps := DPS{
		ID:          DPSID(issuer.CityCode, KindCNPJ, issuer.CNPJ, issuer.Series, invoice.DPSNumber),
		Environment: invoice.Environment,
		IssuedAt:    invoice.IssuedAt,
		AppVersion:  truncateRunes(appVersion, maxAppVersionLength),
		Series:      issuer.Series,
		Number:      invoice.DPSNumber,
		Competence:  invoice.Competence,
		CityCode:    issuer.CityCode,
		Provider: DPSProvider{
			CNPJ: issuer.CNPJ, MunicipalRegistration: issuer.MunicipalRegistration,
			SimplesOption: simplesOption, SimplesRegime: simplesRegime, SpecialRegime: specialRegime,
		},
		Taker: DPSTaker{DocumentKind: takerDocument.Kind(), Document: takerDocument.Digits(), Name: strings.TrimSpace(customer.Name), Email: customer.Email},
		Service: DPSService{
			CityCode: firstNonEmpty(template.ServiceCityCode, issuer.CityCode), NationalTaxCode: template.NationalTaxCode,
			MunicipalTaxCode: template.MunicipalTaxCode, Description: strings.TrimSpace(invoice.Description), NBS: template.NBS,
		},
		Values: DPSValues{
			Amount: invoice.Amount, ISSQNTaxation: firstNonEmpty(template.ISSQNTaxation, "1"),
			ISSQNRetention: firstNonEmpty(template.ISSQNRetention, "1"),
			SimplesTaxRate: simplesTaxRate,
		},
		IBSCBS: DPSIBSCBS{
			Operation: firstNonEmpty(template.IBSCBSOperation, DefaultIBSCBSOperation),
			CST:       firstNonEmpty(template.IBSCBSCST, DefaultIBSCBSCST),
			Class:     firstNonEmpty(template.IBSCBSClass, DefaultIBSCBSClass),
		},
	}
	if isDigits(customer.CityCode, 7) && customer.ZIP != "" && customer.Street != "" {
		dps.Taker.Address = &DPSAddress{
			CityCode: customer.CityCode, ZIP: customer.ZIP, Street: customer.Street,
			Number: firstNonEmpty(customer.Number, "S/N"), Complement: customer.Complement, District: customer.District,
		}
	}
	return dps, nil
}

// truncateRunes corta o texto em `limit` caracteres sem partir caractere acentuado.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// firstNonEmpty devolve o primeiro texto não vazio, para os padrões da DPS.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// FormatDecimal escreve o valor no formato da SEFIN: ponto decimal e duas
// casas, sem milhar ("19000.00").
func (m Money) FormatDecimal() string {
	return fmt.Sprintf("%d.%02d", int64(m)/100, int64(m)%100)
}

// AmbientCode é o tpAmb da DPS: 1 produção, 2 homologação.
func (e Environment) AmbientCode() string {
	if e == EnvProduction {
		return "1"
	}
	return "2"
}
