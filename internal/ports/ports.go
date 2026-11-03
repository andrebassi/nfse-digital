// Package ports define as interfaces que o núcleo usa para falar com o mundo
// de fora: onde os dados ficam e quem emite a nota.
package ports

import (
	"context"
	"errors"
	"time"

	"nfse/internal/domain"
)

// ErrNotFound indica registro inexistente; a tela transforma em 404.
var ErrNotFound = errors.New("registro não encontrado")

// ErrConflict indica registro que o banco recusou por já existir outro igual
// (índice único), como a segunda nota do mesmo agendamento no mesmo mês.
var ErrConflict = errors.New("registro já existe")

// accountKey é a chave da conta no context.
type accountKey struct{}

// WithAccount marca o context com a conta dona dos dados. Todo método do
// repositório que lê ou grava dado de empresa usa esta conta; sem ela, não
// acha nada (falha fechado: nunca mistura dados de duas contas).
func WithAccount(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, accountKey{}, accountID)
}

// AccountID devolve a conta do context, ou zero se não houver.
func AccountID(ctx context.Context) int64 {
	id, _ := ctx.Value(accountKey{}).(int64)
	return id
}

// clientKey é a chave do endereço de quem fez o pedido no context.
type clientKey struct{}

// WithClient marca o context com o endereço do cliente (IP do navegador, no
// painel público). O bloqueio de login conta erros por este endereço.
func WithClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

// Client devolve o endereço do cliente, ou "" quando o pedido é local.
func Client(ctx context.Context) string {
	client, _ := ctx.Value(clientKey{}).(string)
	return client
}

// Repository guarda contas e, por conta, emitente, tomadores, modelos, notas
// e agendamentos. A conta vem do context (WithAccount).
type Repository interface {
	Issuer(ctx context.Context) (domain.Issuer, error)
	SaveIssuer(ctx context.Context, issuer domain.Issuer) error

	// Account devolve a conta do context, ou ErrNotFound.
	Account(ctx context.Context) (domain.Account, error)
	// DeleteAccount apaga a conta da operação e TODO dado dela (emitente,
	// tomadores, modelos, notas, agendamentos, certificado, tokens, passkeys),
	// sem tocar em outra conta. ErrNotFound se a conta não existe.
	DeleteAccount(ctx context.Context) error
	// CreateAccount cria uma conta nova (e-mail repetido é ErrConflict).
	CreateAccount(ctx context.Context, account *domain.Account) error
	// AccountByEmail acha a conta pelo e-mail já normalizado.
	AccountByEmail(ctx context.Context, email string) (domain.Account, error)
	// Accounts lista todas as contas (agendador, login por passkey).
	Accounts(ctx context.Context) ([]domain.Account, error)

	Customers(ctx context.Context, query string) ([]domain.Customer, error)
	Customer(ctx context.Context, id int64) (domain.Customer, error)
	SaveCustomer(ctx context.Context, customer *domain.Customer) error

	Templates(ctx context.Context, query string) ([]domain.Template, error)
	Template(ctx context.Context, id int64) (domain.Template, error)
	SaveTemplate(ctx context.Context, template *domain.Template) error

	Invoices(ctx context.Context) ([]domain.Invoice, error)
	Invoice(ctx context.Context, id int64) (domain.Invoice, error)
	SaveInvoice(ctx context.Context, invoice *domain.Invoice) error

	Schedules(ctx context.Context) ([]domain.Schedule, error)
	SaveSchedule(ctx context.Context, schedule *domain.Schedule) error
	// DeleteSchedule apaga o agendamento; as notas que ele gerou ficam.
	DeleteSchedule(ctx context.Context, id int64) error
	// ClaimNotification marca o aviso do mês como enviado e diz se foi esta
	// chamada que marcou, para dois processos nunca avisarem duas vezes.
	ClaimNotification(ctx context.Context, scheduleID int64, period string) (bool, error)

	// RecordAgentContact grava o último contato do agente pelo MCP;
	// AgentContacts lista do mais recente para o mais antigo.
	RecordAgentContact(ctx context.Context, contact domain.AgentContact) error
	AgentContacts(ctx context.Context) ([]domain.AgentContact, error)

	// CertificateStatus devolve os dados públicos do certificado e a
	// autorização do portal (vazio, sem erro, antes do primeiro registro).
	CertificateStatus(ctx context.Context) (domain.CertificateStatus, error)
	SaveCertificateStatus(ctx context.Context, status domain.CertificateStatus) error

	// Tokens de API do MCP por HTTP: só o hash é guardado.
	CreateAPIToken(ctx context.Context, hash string, token *domain.APIToken) error
	APITokens(ctx context.Context) ([]domain.APIToken, error)
	// APITokenByHash acha o token pelo hash; ErrNotFound se não existe.
	APITokenByHash(ctx context.Context, hash string) (domain.APIToken, error)
	TouchAPIToken(ctx context.Context, id int64, at time.Time) error
	DeleteAPIToken(ctx context.Context, id int64) error

	// SealedCertificate guarda o certificado A1 da conta já cifrado pelo
	// adapter (.pfx + senha num blob só); ErrNotFound se a conta não enviou.
	SealedCertificate(ctx context.Context) ([]byte, error)
	SaveSealedCertificate(ctx context.Context, sealed []byte) error

	// Passkeys do dono: SavePasskey cria (ID zero) ou atualiza (contador e
	// último uso); DeletePasskeys apaga todas ("Remover passkeys").
	SavePasskey(ctx context.Context, passkey *domain.Passkey) error
	Passkeys(ctx context.Context) ([]domain.Passkey, error)
	DeletePasskeys(ctx context.Context) error

	// EmailSender é o Gmail que envia as notas da conta (senha já selada);
	// ErrNotFound antes do primeiro cadastro.
	EmailSender(ctx context.Context) (domain.EmailSender, error)
	SaveEmailSender(ctx context.Context, sender domain.EmailSender) error
	// CustomerEmail é o envio por e-mail de um tomador; ErrNotFound se nunca
	// foi salvo (o app usa o padrão).
	CustomerEmail(ctx context.Context, customerID int64) (domain.CustomerEmail, error)
	SaveCustomerEmail(ctx context.Context, settings domain.CustomerEmail) error
	// RecordInvoiceEmail grava um envio (ou a falha dele); InvoiceEmails lista
	// os de uma nota, do mais recente para o mais antigo.
	RecordInvoiceEmail(ctx context.Context, sent *domain.InvoiceEmail) error
	InvoiceEmails(ctx context.Context, invoiceID int64) ([]domain.InvoiceEmail, error)

	// ReserveDPSNumber devolve a série e o próximo número e já avança o
	// contador, para duas emissões nunca usarem o mesmo número.
	ReserveDPSNumber(ctx context.Context) (series string, number int64, err error)
}

// EmitRequest é tudo o que o emissor precisa para montar e enviar a DPS.
type EmitRequest struct {
	Issuer   domain.Issuer
	Customer domain.Customer
	Template domain.Template
	Invoice  domain.Invoice
}

// EmitResult é a resposta da SEFIN (ou do simulador) já interpretada.
type EmitResult struct {
	Status       domain.InvoiceStatus
	AccessKey    string
	NFSeNumber   string
	ErrorCode    string
	ErrorMessage string
	Simulated    bool
	// XML é o XML da NFS-e devolvido pela SEFIN na autorização.
	XML []byte
}

// Emitter envia a DPS. No protótipo é simulado; no Epic 2 é o cliente da SEFIN.
type Emitter interface {
	Emit(ctx context.Context, request EmitRequest) (EmitResult, error)
}

// Notifier manda um aviso ao dono (ntfy no celular).
type Notifier interface {
	Notify(ctx context.Context, title, message string) error
}

// CancelResult é a resposta da SEFIN ao pedido de cancelamento.
type CancelResult struct {
	Registered   bool
	ErrorCode    string
	ErrorMessage string
	// EventXML é o XML do evento registrado, quando a SEFIN o devolve.
	EventXML []byte
}

// MailAttachment é um anexo do e-mail da nota (DANFSe em PDF, XML).
type MailAttachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Mail é um e-mail pronto para sair: remetente, destinatários, texto puro e anexos.
type Mail struct {
	From        string
	FromName    string
	To          []string
	CC          []string
	Subject     string
	Body        string
	Attachments []MailAttachment
}

// Mailer envia pelo SMTP do Gmail com a senha de app. Verify só faz o login
// (a conferência ao salvar), sem mandar nada.
type Mailer interface {
	Verify(ctx context.Context, address, password string) error
	Send(ctx context.Context, password string, mail Mail) error
}

// InvoiceDocuments gera os anexos do e-mail: o DANFSe em PDF e o XML da
// NFS-e de uma nota real, ou os de exemplo (marcados como simulação, sem
// valor fiscal) para o e-mail de teste.
type InvoiceDocuments interface {
	Invoice(ctx context.Context, invoice domain.Invoice) (pdf, xml []byte, err error)
	Sample(ctx context.Context, issuer domain.Issuer, customer domain.Customer, invoice domain.Invoice) (pdf, xml []byte, err error)
}

// SecretSealer cifra um segredo da conta do context (a senha de app do
// Gmail) para guardar no banco, e abre na hora de usar. Um blob de uma conta
// não abre na outra.
type SecretSealer interface {
	Seal(ctx context.Context, plain []byte) ([]byte, error)
	Open(ctx context.Context, sealed []byte) ([]byte, error)
}

// Canceller registra o evento de cancelamento na SEFIN.
type Canceller interface {
	Cancel(ctx context.Context, event domain.CancelEvent) (CancelResult, error)
}

// DFeDocument é um documento da distribuição do ADN: NFS-e ou evento.
type DFeDocument struct {
	NSU       int64
	AccessKey string
	Type      string
	XML       []byte
}

// DFeBatch é um lote da distribuição; More diz se há lote seguinte.
type DFeBatch struct {
	Documents []DFeDocument
	More      bool
}

// Consulter lê o estado de uma nota na SEFIN/ADN, só leitura.
type Consulter interface {
	// AccessKeyByDPS devolve a chave da NFS-e gerada pela DPS, ou ErrNotFound.
	AccessKeyByDPS(ctx context.Context, dpsID string) (string, error)
	// Documents devolve a NFS-e e os eventos dela.
	Documents(ctx context.Context, accessKey string) (DFeBatch, error)
}

// PortalReader lê, só leitura, os documentos fiscais do CNPJ no ambiente
// nacional, a partir de um NSU.
type PortalReader interface {
	Distribution(ctx context.Context, afterNSU int64) (DFeBatch, error)
}
