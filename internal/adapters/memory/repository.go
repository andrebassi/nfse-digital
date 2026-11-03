// Package memory guarda os dados na memória do processo. É o dublê de banco
// dos testes automáticos (com os dados fictícios de seed.go); o painel usa
// sempre o SQLite.
package memory

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"nfse/internal/adapters/textsearch"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Repository implementa ports.Repository com mapas protegidos por um RWMutex:
// o painel atende requisições em paralelo, e a reserva de número não pode repetir.
type Repository struct {
	mu     sync.RWMutex
	issuer domain.Issuer
	// accounts são as contas cadastradas. A memória guarda UMA empresa só
	// (testes automáticos); o isolamento por conta é do SQLite.
	accounts  []domain.Account
	customers map[int64]domain.Customer
	templates map[int64]domain.Template
	invoices  map[int64]domain.Invoice
	schedules map[int64]domain.Schedule
	contacts  map[string]domain.AgentContact
	cert      domain.CertificateStatus
	tokens    map[string]domain.APIToken
	passkeys  []domain.Passkey
	sealed    map[int64][]byte
	// sender, customerEmails e invoiceEmails são o envio por e-mail.
	sender         *domain.EmailSender
	customerEmails map[int64]domain.CustomerEmail
	invoiceEmails  []domain.InvoiceEmail
	lastID         int64
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Repository = (*Repository)(nil)

// New cria o repositório vazio com o emitente informado.
func New(issuer domain.Issuer) *Repository {
	return &Repository{
		issuer:    issuer,
		customers: map[int64]domain.Customer{},
		templates: map[int64]domain.Template{},
		invoices:  map[int64]domain.Invoice{},
		schedules: map[int64]domain.Schedule{},
		contacts:  map[string]domain.AgentContact{},
		tokens:    map[string]domain.APIToken{},
		sealed:    map[int64][]byte{},

		customerEmails: map[int64]domain.CustomerEmail{},
	}
}

// nextID gera IDs únicos entre todas as tabelas; chamar com o lock de escrita.
func (r *Repository) nextID() int64 {
	r.lastID++
	return r.lastID
}

// Issuer devolve o emitente; ErrNotFound enquanto não há CNPJ configurado,
// igual ao SQLite.
func (r *Repository) Issuer(context.Context) (domain.Issuer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.issuer.CNPJ == "" {
		return domain.Issuer{}, ports.ErrNotFound
	}
	return r.issuer, nil
}

// SaveIssuer substitui o emitente.
func (r *Repository) SaveIssuer(_ context.Context, issuer domain.Issuer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.issuer = issuer
	return nil
}

// Account devolve a conta do context; sem conta no context, a primeira
// cadastrada (os testes usam um dono só). ErrNotFound sem nenhuma.
func (r *Repository) Account(ctx context.Context) (domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id := ports.AccountID(ctx)
	for _, account := range r.accounts {
		if id == 0 || account.ID == id {
			return account, nil
		}
	}
	return domain.Account{}, ports.ErrNotFound
}

// DeleteAccount apaga a conta e, como a memória guarda uma empresa só,
// todos os dados dela.
func (r *Repository) DeleteAccount(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := ports.AccountID(ctx)
	index := slices.IndexFunc(r.accounts, func(account domain.Account) bool { return id == 0 || account.ID == id })
	if index < 0 {
		return ports.ErrNotFound
	}
	r.accounts = slices.Delete(r.accounts, index, index+1)
	r.issuer, r.cert, r.passkeys = domain.Issuer{}, domain.CertificateStatus{}, nil
	r.customers, r.templates = map[int64]domain.Customer{}, map[int64]domain.Template{}
	r.invoices, r.schedules = map[int64]domain.Invoice{}, map[int64]domain.Schedule{}
	r.contacts, r.tokens, r.sealed = map[string]domain.AgentContact{}, map[string]domain.APIToken{}, map[int64][]byte{}
	r.sender, r.customerEmails, r.invoiceEmails = nil, map[int64]domain.CustomerEmail{}, nil
	return nil
}

// CreateAccount cria a conta; e-mail repetido é ErrConflict.
func (r *Repository) CreateAccount(_ context.Context, account *domain.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if existing.Email == account.Email {
			return ports.ErrConflict
		}
	}
	account.ID = r.nextID()
	r.accounts = append(r.accounts, *account)
	return nil
}

// AccountByEmail acha a conta pelo e-mail.
func (r *Repository) AccountByEmail(_ context.Context, email string) (domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, account := range r.accounts {
		if account.Email == email {
			return account, nil
		}
	}
	return domain.Account{}, ports.ErrNotFound
}

// Accounts lista as contas na ordem de cadastro.
func (r *Repository) Accounts(context.Context) ([]domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.accounts), nil
}

// Customers lista os tomadores em ordem de nome, filtrando por trecho de nome ou documento.
func (r *Repository) Customers(_ context.Context, query string) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []domain.Customer{}
	for _, customer := range r.customers {
		if textsearch.Matches(query, customer.Name, customer.Document, domain.FormatDocument(customer.Document), customer.Email) {
			result = append(result, customer)
		}
	}
	slices.SortFunc(result, func(a, b domain.Customer) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// Customer busca um tomador pelo ID.
func (r *Repository) Customer(_ context.Context, id int64) (domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	customer, ok := r.customers[id]
	if !ok {
		return domain.Customer{}, ports.ErrNotFound
	}
	return customer, nil
}

// SaveCustomer cria (ID zero) ou atualiza um tomador; atualizar ID inexistente é erro.
func (r *Repository) SaveCustomer(_ context.Context, customer *domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.customers[customer.ID]; customer.ID != 0 && !ok {
		return ports.ErrNotFound
	}
	// Migração 012: o mesmo CPF/CNPJ é um tomador só.
	for id, other := range r.customers {
		if id != customer.ID && customer.Document != "" && other.Document == customer.Document {
			return ports.ErrConflict
		}
	}
	if customer.ID == 0 {
		customer.ID = r.nextID()
	}
	r.customers[customer.ID] = *customer
	return nil
}

// Templates lista os modelos em ordem de nome, filtrando por nome, descrição ou código.
func (r *Repository) Templates(_ context.Context, query string) ([]domain.Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := []domain.Template{}
	for _, template := range r.templates {
		customerName := r.customers[template.CustomerID].Name
		if textsearch.Matches(query, template.Name, template.Description, template.NationalTaxCode, domain.ServiceLabel(template.NationalTaxCode), customerName) {
			result = append(result, template)
		}
	}
	slices.SortFunc(result, func(a, b domain.Template) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// Template busca um modelo pelo ID.
func (r *Repository) Template(_ context.Context, id int64) (domain.Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	template, ok := r.templates[id]
	if !ok {
		return domain.Template{}, ports.ErrNotFound
	}
	return template, nil
}

// SaveTemplate cria (ID zero) ou atualiza um modelo.
func (r *Repository) SaveTemplate(_ context.Context, template *domain.Template) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if template.ID == 0 {
		template.ID = r.nextID()
	} else if _, ok := r.templates[template.ID]; !ok {
		return ports.ErrNotFound
	}
	r.templates[template.ID] = *template
	return nil
}

// Invoices lista as notas da mais recente para a mais antiga.
func (r *Repository) Invoices(context.Context) ([]domain.Invoice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Invoice, 0, len(r.invoices))
	for _, invoice := range r.invoices {
		result = append(result, invoice)
	}
	slices.SortFunc(result, func(a, b domain.Invoice) int { return b.IssuedAt.Compare(a.IssuedAt) })
	return result, nil
}

// Invoice busca uma nota pelo ID.
func (r *Repository) Invoice(_ context.Context, id int64) (domain.Invoice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	invoice, ok := r.invoices[id]
	if !ok {
		return domain.Invoice{}, ports.ErrNotFound
	}
	return invoice, nil
}

// SaveInvoice cria (ID zero) ou atualiza uma nota.
func (r *Repository) SaveInvoice(_ context.Context, invoice *domain.Invoice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Mesmas regras dos índices únicos do SQLite: uma nota por agendamento e
	// mês, e uma nota por chave de idempotência.
	for id, other := range r.invoices {
		if id == invoice.ID {
			continue
		}
		sameRun := invoice.ScheduleID > 0 && other.ScheduleID == invoice.ScheduleID && other.SchedulePeriod == invoice.SchedulePeriod
		sameKey := invoice.IdempotencyKey != "" && other.IdempotencyKey == invoice.IdempotencyKey
		// Migração 012: a mesma NFS-e (chave de acesso) uma vez só.
		sameNFSe := invoice.AccessKey != "" && other.AccessKey == invoice.AccessKey
		if sameRun || sameKey || sameNFSe {
			return ports.ErrConflict
		}
	}
	if invoice.ID == 0 {
		invoice.ID = r.nextID()
	} else if _, ok := r.invoices[invoice.ID]; !ok {
		return ports.ErrNotFound
	}
	r.invoices[invoice.ID] = *invoice
	return nil
}

// Schedules lista os agendamentos por dia do mês.
func (r *Repository) Schedules(context.Context) ([]domain.Schedule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Schedule, 0, len(r.schedules))
	for _, schedule := range r.schedules {
		result = append(result, schedule)
	}
	slices.SortFunc(result, func(a, b domain.Schedule) int { return a.Day - b.Day })
	return result, nil
}

// SaveSchedule cria (ID zero) ou atualiza um agendamento.
func (r *Repository) SaveSchedule(_ context.Context, schedule *domain.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if schedule.ID == 0 {
		schedule.ID = r.nextID()
	} else if _, ok := r.schedules[schedule.ID]; !ok {
		return ports.ErrNotFound
	}
	r.schedules[schedule.ID] = *schedule
	return nil
}

// DeleteSchedule apaga o agendamento; as notas que ele gerou ficam.
func (r *Repository) DeleteSchedule(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schedules[id]; !ok {
		return ports.ErrNotFound
	}
	delete(r.schedules, id)
	return nil
}

// ClaimNotification marca o aviso do mês sob o lock e diz se marcou agora.
func (r *Repository) ClaimNotification(_ context.Context, id int64, period string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule, ok := r.schedules[id]
	if !ok || schedule.NotifiedPeriod == period {
		return false, nil
	}
	schedule.NotifiedPeriod = period
	r.schedules[id] = schedule
	return true, nil
}

// ReserveDPSNumber devolve o número atual e avança o contador sob o mesmo lock.
func (r *Repository) ReserveDPSNumber(context.Context) (string, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.issuer.CNPJ == "" {
		return "", 0, ports.ErrNotFound
	}
	number := r.issuer.NextDPSNumber
	r.issuer.NextDPSNumber++
	return r.issuer.Series, number, nil
}

// RecordAgentContact grava o último contato do cliente MCP.
func (r *Repository) RecordAgentContact(_ context.Context, contact domain.AgentContact) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contacts[contact.Client] = contact
	return nil
}

// AgentContacts lista os contatos, do mais recente para o mais antigo.
func (r *Repository) AgentContacts(context.Context) ([]domain.AgentContact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.AgentContact, 0, len(r.contacts))
	for _, contact := range r.contacts {
		result = append(result, contact)
	}
	slices.SortFunc(result, func(a, b domain.AgentContact) int { return b.At.Compare(a.At) })
	return result, nil
}

// CertificateStatus devolve o estado guardado (vazio no começo).
func (r *Repository) CertificateStatus(context.Context) (domain.CertificateStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}

// SaveCertificateStatus substitui o estado guardado.
func (r *Repository) SaveCertificateStatus(_ context.Context, status domain.CertificateStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cert = status
	return nil
}

// CreateAPIToken guarda o token pelo hash e preenche o ID.
func (r *Repository) CreateAPIToken(ctx context.Context, hash string, token *domain.APIToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tokens[hash]; exists {
		return ports.ErrConflict
	}
	token.ID, token.AccountID = r.nextID(), ports.AccountID(ctx)
	r.tokens[hash] = *token
	return nil
}

// APITokens lista os tokens, do mais novo para o mais antigo.
func (r *Repository) APITokens(context.Context) ([]domain.APIToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.APIToken, 0, len(r.tokens))
	for _, token := range r.tokens {
		result = append(result, token)
	}
	slices.SortFunc(result, func(a, b domain.APIToken) int { return int(b.ID - a.ID) })
	return result, nil
}

// APITokenByHash acha o token pelo hash.
func (r *Repository) APITokenByHash(_ context.Context, hash string) (domain.APIToken, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	token, ok := r.tokens[hash]
	if !ok {
		return domain.APIToken{}, ports.ErrNotFound
	}
	return token, nil
}

// TouchAPIToken registra o último uso.
func (r *Repository) TouchAPIToken(_ context.Context, id int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for hash, token := range r.tokens {
		if token.ID == id {
			token.LastUsedAt = at
			r.tokens[hash] = token
			return nil
		}
	}
	return ports.ErrNotFound
}

// DeleteAPIToken revoga o token.
func (r *Repository) DeleteAPIToken(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for hash, token := range r.tokens {
		if token.ID == id {
			delete(r.tokens, hash)
			return nil
		}
	}
	return ports.ErrNotFound
}

// SavePasskey cria (ID zero, recusando credencial repetida) ou atualiza.
func (r *Repository) SavePasskey(_ context.Context, p *domain.Passkey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.passkeys {
		if p.ID != 0 && existing.ID == p.ID {
			r.passkeys[i].Data, r.passkeys[i].LastUsedAt = p.Data, p.LastUsedAt
			return nil
		}
		if p.ID == 0 && bytes.Equal(existing.CredentialID, p.CredentialID) {
			return ports.ErrConflict
		}
	}
	if p.ID != 0 {
		return ports.ErrNotFound
	}
	p.ID = r.nextID()
	r.passkeys = append(r.passkeys, *p)
	return nil
}

// Passkeys lista as passkeys na ordem de cadastro.
func (r *Repository) Passkeys(context.Context) ([]domain.Passkey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.passkeys), nil
}

// DeletePasskeys apaga todas as passkeys.
func (r *Repository) DeletePasskeys(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.passkeys = nil
	return nil
}

// SealedCertificate devolve o certificado cifrado da conta do context.
func (r *Repository) SealedCertificate(ctx context.Context) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sealed, ok := r.sealed[ports.AccountID(ctx)]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return slices.Clone(sealed), nil
}

// SaveSealedCertificate grava o certificado cifrado da conta do context.
func (r *Repository) SaveSealedCertificate(ctx context.Context, sealed []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed[ports.AccountID(ctx)] = slices.Clone(sealed)
	return nil
}
