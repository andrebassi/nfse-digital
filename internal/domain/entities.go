package domain

import (
	"slices"
	"strings"
	"time"
)

// Environment é o ambiente da SEFIN em que a nota é emitida.
type Environment string

const (
	// EnvHomologation é a produção restrita: nota de teste, sem valor fiscal.
	EnvHomologation Environment = "homologation"
	// EnvProduction emite nota com valor fiscal.
	EnvProduction Environment = "production"
)

// Label devolve o nome do ambiente como aparece na tela.
func (e Environment) Label() string {
	if e == EnvProduction {
		return "Produção"
	}
	return "Homologação"
}

// Issuer é a empresa que emite as notas (o prestador).
type Issuer struct {
	LegalName             string
	CNPJ                  string
	MunicipalRegistration string
	CityCode              string
	CityName              string
	SimplesOption         string
	SimplesRegime         string
	SpecialRegime         string
	// SimplesTaxRate é o percentual aproximado dos tributos pela alíquota do
	// Simples (pTotTribSN). A SEFIN exige para ME/EPP (E0712 quando a DPS
	// manda indTotTrib no lugar); para os outros regimes não vai no XML.
	SimplesTaxRate Percent
	Environment    Environment
	Series         string
	NextDPSNumber  int64
}

// Customer é o tomador do serviço, quem recebe a nota.
type Customer struct {
	ID         int64
	Foreign    bool
	Document   string
	Name       string
	Email      string
	CityCode   string
	CityName   string
	ZIP        string
	District   string
	Street     string
	Number     string
	Complement string
	Country    string
}

// Template é o modelo de serviço: o que se presta, como é tributado e o valor de costume.
type Template struct {
	ID               int64
	Name             string
	Description      string
	NationalTaxCode  string
	MunicipalTaxCode string
	NBS              string
	ServiceCityCode  string
	ServiceCityName  string
	DefaultAmount    Money
	SimplesTaxRate   string
	ISSQNTaxation    string
	ISSQNRetention   string
	Export           bool
	CustomerID       int64

	// Grupo IBS/CBS da reforma tributária; vazio usa os padrões de dps.go.
	IBSCBSOperation string
	IBSCBSCST       string
	IBSCBSClass     string
}

// Invoice é uma tentativa de emissão de NFS-e, com o retorno da SEFIN.
type Invoice struct {
	ID           int64
	Environment  Environment
	Series       string
	DPSNumber    int64
	CustomerID   int64
	TemplateID   int64
	Amount       Money
	Competence   time.Time
	IssuedAt     time.Time
	Description  string
	ServiceCode  string
	Status       InvoiceStatus
	AccessKey    string
	NFSeNumber   string
	ErrorCode    string
	ErrorMessage string
	Simulated    bool
	// XML é o XML da NFS-e autorizada, guardado para gerar o DANFSe localmente.
	XML []byte
	// ScheduleID e SchedulePeriod ligam a nota ao agendamento e ao mês
	// (AAAA-MM) que a gerou; o banco recusa duas notas do mesmo par.
	ScheduleID     int64
	SchedulePeriod string
	// IdempotencyKey é a chave que o agente manda no MCP; o banco recusa duas
	// notas com a mesma chave.
	IdempotencyKey string
}

// FieldErrors junta os erros de validação por campo do formulário, para a
// tela devolver todos de uma vez em vez de um por envio.
type FieldErrors map[string]string

// Error junta as mensagens numa linha só, para log e teste.
func (f FieldErrors) Error() string {
	messages := make([]string, 0, len(f))
	for field, message := range f {
		messages = append(messages, field+": "+message)
	}
	slices.Sort(messages)
	return strings.Join(messages, "; ")
}

// orNil devolve nil quando não há erro, para o chamador testar com `err != nil`.
func (f FieldErrors) orNil() error {
	if len(f) == 0 {
		return nil
	}
	return f
}

// Validate confere o tomador antes de gravar. Tomador no exterior não tem
// CPF/CNPJ, mas precisa do país.
func (c Customer) Validate() error {
	errs := FieldErrors{}
	if strings.TrimSpace(c.Name) == "" {
		errs["name"] = "informe o nome ou a razão social"
	}
	if c.Foreign {
		if strings.TrimSpace(c.Country) == "" {
			errs["country"] = "informe o país do tomador no exterior"
		}
	} else if _, err := ParseDocument(c.Document); err != nil {
		errs["document"] = "CPF/CNPJ inválido"
	}
	if c.Email != "" && !strings.Contains(c.Email, "@") {
		errs["email"] = "e-mail inválido"
	}
	return errs.orNil()
}

// Validate confere o modelo: código nacional com 6 dígitos e NBS com 9, que
// é o tamanho que a SEFIN aceita; NBS e código municipal são opcionais.
func (t Template) Validate() error {
	errs := FieldErrors{}
	if strings.TrimSpace(t.Name) == "" {
		errs["name"] = "informe o nome do modelo"
	}
	if strings.TrimSpace(t.Description) == "" {
		errs["description"] = "informe a descrição do serviço"
	}
	if !isDigits(t.NationalTaxCode, 6) {
		errs["national_tax_code"] = "o código de tributação nacional tem 6 dígitos"
	}
	if t.NBS != "" && !isDigits(t.NBS, 9) {
		errs["nbs"] = "o NBS tem 9 dígitos"
	}
	if !isDigits(t.ServiceCityCode, 7) {
		errs["service_city_code"] = "informe o código IBGE do município da prestação (7 dígitos)"
	}
	if t.DefaultAmount < 0 {
		errs["default_amount"] = "o valor padrão não pode ser negativo"
	}
	return errs.orNil()
}

// Validate confere o emitente: CNPJ válido, município IBGE e numeração positiva.
func (i Issuer) Validate() error {
	errs := FieldErrors{}
	if strings.TrimSpace(i.LegalName) == "" {
		errs["legal_name"] = "informe a razão social"
	}
	if document, err := ParseDocument(i.CNPJ); err != nil || document.Kind() != KindCNPJ {
		errs["cnpj"] = "CNPJ inválido"
	}
	if !isDigits(i.CityCode, 7) {
		errs["city_code"] = "informe o código IBGE do município (7 dígitos)"
	}
	if i.Series == "" || len(i.Series) > 5 || !isDigits(i.Series, len(i.Series)) {
		errs["series"] = "a série tem de 1 a 5 dígitos"
	}
	if i.NextDPSNumber <= 0 {
		errs["next_dps_number"] = "o próximo número da DPS precisa ser maior que zero"
	}
	if i.Environment != EnvHomologation && i.Environment != EnvProduction {
		errs["environment"] = "ambiente desconhecido"
	}
	// Regime fora da tabela só seria descoberto na emissão, como "regime
	// especial desconhecido" no meio do envio; recusar aqui mostra no campo.
	if _, ok := specialRegimeCodes[i.SpecialRegime]; !ok {
		errs["special_regime"] = "regime especial desconhecido"
	}
	if i.SimplesTaxRate < 0 || i.SimplesTaxRate > MaxPercent {
		errs["simples_tax_rate"] = ErrInvalidPercent.Error()
	}
	return errs.orNil()
}

// isDigits diz se o texto tem exatamente `size` dígitos e nada mais.
func isDigits(text string, size int) bool {
	if len(text) != size || size == 0 {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// serviceLabels traduz o código de tributação nacional para o nome do
// serviço; só os que o emitente usa hoje, o resto aparece pelo código.
var serviceLabels = map[string]string{
	"010101": "Análise e desenvolvimento de sistemas",
	"010601": "Assessoria e consultoria em informática",
	"010701": "Suporte técnico em informática, inclusive instalação, configuração e manutenção de programas de computação e bancos de dados",
}

// ServiceLabel devolve o nome do serviço do código nacional, ou o próprio código.
func ServiceLabel(code string) string {
	if label, ok := serviceLabels[code]; ok {
		return label
	}
	return code
}

// FormatServiceCode exibe o código nacional como "01.07.01".
func FormatServiceCode(code string) string {
	if !isDigits(code, 6) {
		return code
	}
	return code[:2] + "." + code[2:4] + "." + code[4:]
}

// AgentContact é o último contato de um agente pelo MCP: qual cliente
// (Claude Code, Codex…), qual ferramenta e quando. Mostra na tela de
// Integrações que a conexão funciona de verdade.
type AgentContact struct {
	Client string
	Tool   string
	At     time.Time
}

// APIToken é um token de API do MCP por HTTP, sem o valor: só o que a lista
// mostra (final do token, criação e último uso).
type APIToken struct {
	ID int64
	// AccountID é a conta dona do token: o MCP por HTTP age em nome dela.
	AccountID  int64
	Hint       string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// Passkey é uma credencial WebAuthn do dono. Data é a credencial serializada
// pelo adapter (chave pública e contador), opaca para o domínio.
type Passkey struct {
	ID           int64
	CredentialID []byte
	Data         []byte
	Label        string
	CreatedAt    time.Time
	LastUsedAt   time.Time
}
