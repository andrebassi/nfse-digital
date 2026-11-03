package domain

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// EmailSender é a conta Gmail que manda as notas, uma por conta do emissor
// (vale para todos os tomadores). A senha de app nunca fica em claro: o
// adapter de cofre sela antes de gravar e só abre na hora de enviar.
type EmailSender struct {
	Address string
	Name    string
	// SealedPassword é a senha de app do Google selada pelo cofre.
	SealedPassword []byte
	// VerifiedAt é quando o login no Gmail funcionou pela última vez (ao salvar).
	VerifiedAt time.Time
}

// Configured diz se dá para enviar: endereço e senha guardados.
func (s EmailSender) Configured() bool {
	return s.Address != "" && len(s.SealedPassword) > 0
}

// CustomerEmail é o envio por e-mail de um tomador: para quem vai, o texto e
// os anexos. AutoSend manda cada nota autorizada sozinho; desligado, só pelo
// botão "Enviar por e-mail" da nota.
type CustomerEmail struct {
	CustomerID int64
	AutoSend   bool
	To         string
	CC         string
	Subject    string
	Body       string
	AttachPDF  bool
	AttachXML  bool
}

// DefaultEmailSubject e DefaultEmailBody são o texto inicial de todo tomador,
// o mesmo que o SaaS usado antes oferece (medido em 02/11/2026).
const (
	DefaultEmailSubject = "NFS-e nº {numero} - {emitente}"
	DefaultEmailBody    = "Olá,\n\nSegue em anexo a nota fiscal de serviço nº {numero}, emitida para {tomador}, competência {mesCompetencia}, no valor de {valor}.\n\nA autenticidade da nota pode ser consultada no portal da NFS-e Nacional:\n{linkConsulta}\n\nAtenciosamente,\n{emitente}"
)

// DefaultCustomerEmail é a configuração de quem nunca salvou o envio: vai
// para o e-mail do cadastro, com os dois anexos e sem envio automático.
func DefaultCustomerEmail(customer Customer) CustomerEmail {
	return CustomerEmail{
		CustomerID: customer.ID, To: customer.Email, Subject: DefaultEmailSubject, Body: DefaultEmailBody,
		AttachPDF: true, AttachXML: true,
	}
}

// Validate confere destinatários e texto. Envio automático exige Para: sem
// ele, a nota autorizada não teria para onde ir e o erro só apareceria depois.
func (c CustomerEmail) Validate() error {
	errs := FieldErrors{}
	to, err := ParseEmailList(c.To)
	switch {
	case err != nil:
		errs["to"] = err.Error()
	case len(to) == 0:
		errs["to"] = "informe ao menos um e-mail"
	}
	if _, err := ParseEmailList(c.CC); err != nil {
		errs["cc"] = err.Error()
	}
	if strings.TrimSpace(c.Subject) == "" {
		errs["subject"] = "informe o assunto"
	}
	if strings.TrimSpace(c.Body) == "" {
		errs["body"] = "informe a mensagem"
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ParseEmailList lê uma lista separada por vírgula ou ponto e vírgula. Vazia
// é lista vazia, sem erro; endereço inválido diz qual foi. Só o endereço
// puro é aceito ("a@b.com"), sem nome: o nome viria do texto de outra pessoa
// e entraria no cabeçalho do e-mail.
func ParseEmailList(raw string) ([]string, error) {
	var list []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := mail.ParseAddress(part)
		if err != nil || parsed.Address != part || parsed.Name != "" {
			return nil, fmt.Errorf("e-mail inválido: %s", part)
		}
		list = append(list, part)
	}
	return list, nil
}

// ValidEmail diz se o texto é um endereço de e-mail só (sem nome).
func ValidEmail(address string) bool {
	list, err := ParseEmailList(address)
	return err == nil && len(list) == 1 && list[0] == strings.TrimSpace(address)
}

// EmailValues são os valores dos marcadores do assunto e da mensagem, já
// formatados para gente ler.
type EmailValues struct {
	Number          string
	Amount          string
	Competence      string
	CompetenceMonth string
	IssuedAt        string
	Customer        string
	Issuer          string
	IssuerCNPJ      string
	Description     string
	AccessKey       string
	ConsultURL      string
}

// EmailPlaceholders são os marcadores aceitos, na ordem da tela. Os nomes
// são os mesmos do SaaS usado antes: quem migrar o texto de lá não reescreve nada.
var EmailPlaceholders = []string{
	"{numero}", "{valor}", "{competencia}", "{mesCompetencia}", "{dataEmissao}", "{tomador}",
	"{emitente}", "{cnpjEmitente}", "{descricao}", "{chave}", "{linkConsulta}",
}

// consultURLPrefix é a consulta pública da NFS-e Nacional pela chave de
// acesso (tpc=1 = consulta por chave), o mesmo link do SaaS usado antes.
const consultURLPrefix = "https://www.nfse.gov.br/ConsultaPublica/?tpc=1&chave="

// ConsultURL devolve o link público de consulta da nota pela chave.
func ConsultURL(accessKey string) string {
	return consultURLPrefix + accessKey
}

// InvoiceEmailValues monta os valores dos marcadores para uma nota real.
func InvoiceEmailValues(issuer Issuer, customer Customer, invoice Invoice) EmailValues {
	values := EmailValues{
		Number: invoice.NFSeNumber, Amount: invoice.Amount.FormatBRL(), Customer: customer.Name,
		Issuer: issuer.LegalName, IssuerCNPJ: FormatDocument(issuer.CNPJ), Description: invoice.Description,
		AccessKey: invoice.AccessKey, ConsultURL: ConsultURL(invoice.AccessKey),
	}
	if !invoice.Competence.IsZero() {
		values.Competence = invoice.Competence.Format("01/2006")
		values.CompetenceMonth = fmt.Sprintf("%s/%d", monthNames[invoice.Competence.Month()-1], invoice.Competence.Year())
	}
	if !invoice.IssuedAt.IsZero() {
		values.IssuedAt = invoice.IssuedAt.Format("02/01/2006")
	}
	return values
}

// SampleInvoice é a nota de exemplo da prévia e do e-mail de teste: nº 123,
// R$ 1.500,00, competência do mês de now. A chave é fictícia (só zeros depois
// do código do município), para ninguém confundir com uma nota de verdade.
func SampleInvoice(issuer Issuer, customer Customer, now time.Time) Invoice {
	competence := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return Invoice{
		NFSeNumber: "123", Amount: 150000, Competence: competence, IssuedAt: now, CustomerID: customer.ID,
		Description: "Serviço de exemplo - " + monthNames[now.Month()-1], Environment: EnvHomologation,
		AccessKey: sampleAccessKey(issuer), Status: StatusAuthorized,
	}
}

// sampleAccessKey monta uma chave de 50 dígitos com o município e o CNPJ do
// emitente e o resto zerado, no formato que a prévia mostra.
func sampleAccessKey(issuer Issuer) string {
	key := issuer.CityCode + "2" + issuer.CNPJ + "000000000000000123"
	if len(key) < 50 {
		key += strings.Repeat("0", 50-len(key))
	}
	return key[:50]
}

// RenderEmailTemplate troca os marcadores pelos valores. Marcador
// desconhecido fica como está, para o dono ver o erro de digitação na prévia.
func RenderEmailTemplate(text string, v EmailValues) string {
	return strings.NewReplacer(
		"{numero}", v.Number, "{valor}", v.Amount, "{competencia}", v.Competence,
		"{mesCompetencia}", v.CompetenceMonth, "{dataEmissao}", v.IssuedAt, "{tomador}", v.Customer,
		"{emitente}", v.Issuer, "{cnpjEmitente}", v.IssuerCNPJ, "{descricao}", v.Description,
		"{chave}", v.AccessKey, "{linkConsulta}", v.ConsultURL,
	).Replace(text)
}

// InvoiceEmail é o registro de um envio (ou da tentativa): aparece no
// detalhe da nota. Error vazio é enviado.
type InvoiceEmail struct {
	ID        int64
	InvoiceID int64
	SentAt    time.Time
	To        string
	// Automatic marca o envio feito sozinho, logo depois da autorização.
	Automatic bool
	Error     string
}
