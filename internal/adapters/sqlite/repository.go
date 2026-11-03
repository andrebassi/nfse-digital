// Package sqlite guarda os dados do emissor num arquivo SQLite local, com o
// driver em Go puro (sem CGO). É onde os dados reais do dono moram: fora do
// código e fora do git.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registra o driver "sqlite"

	"nfse/internal/adapters/textsearch"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// timeLayout é o formato das datas no banco: RFC 3339 com fração, que ordena
// como texto e guarda o fuso.
const timeLayout = time.RFC3339Nano

// Repository implementa ports.Repository sobre SQLite.
type Repository struct {
	db *sql.DB
	// account é a conta padrão quando o context não traz uma (CLI, agendador
	// por conta). Zero = nenhuma: as consultas de empresa não acham nada.
	account int64
}

// For devolve o mesmo banco com a conta padrão definida. É como a CLI e o
// agendador escolhem de qual empresa são os dados.
func (r *Repository) For(accountID int64) *Repository {
	return &Repository{db: r.db, account: accountID}
}

// acct é a conta da operação: a do context, senão a padrão.
func (r *Repository) acct(ctx context.Context) int64 {
	if id := ports.AccountID(ctx); id != 0 {
		return id
	}
	return r.account
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Repository = (*Repository)(nil)

// DefaultPath é o banco padrão: ~/.local/share/nfse-digital/nfse.db, ou o
// caminho em NFSE_DB.
func DefaultPath() (string, error) {
	if path := os.Getenv("NFSE_DB"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("descobrir a pasta do usuário: %w", err)
	}
	return filepath.Join(home, ".local", "share", "nfse-digital", "nfse.db"), nil
}

// Open abre (ou cria) o banco e aplica as migrações pendentes. A pasta nasce
// com permissão 0700 e o arquivo com 0600: o banco guarda o hash da senha e os
// dados fiscais do dono.
func Open(path string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("criar pasta do banco: %w", err)
	}
	// foreign_keys liga a checagem; WAL deixa o painel ler enquanto grava;
	// busy_timeout espera 5 s em vez de falhar quando duas gravações se cruzam.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}
	repo, err := OpenWithDB(db)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restringir permissão do banco: %w", err)
	}
	return repo, nil
}

// OpenWithDB aplica as migrações numa conexão JÁ ABERTA e devolve o
// repositório. É a parte comum entre o arquivo local (Open) e o Turso remoto
// (adapter turso): muda só como a conexão nasce, nunca o schema nem as
// consultas, que moram aqui, num lugar só. Falhando, fecha a conexão.
func OpenWithDB(db *sql.DB) (*Repository, error) {
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Repository{db: db}, nil
}

// Close fecha a conexão com o banco.
func (r *Repository) Close() error { return r.db.Close() }

// migrate aplica, em ordem de nome, cada arquivo de migrations/ ainda não
// registrado em schema_migrations, cada um na sua transação.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("criar controle de migrações: %w", err)
	}
	entries, _ := migrationsFS.ReadDir("migrations")
	for _, entry := range entries {
		version := entry.Name()
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("consultar migração %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		script, _ := migrationsFS.ReadFile("migrations/" + version)
		if err := inTx(db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(string(script)); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		}); err != nil {
			return fmt.Errorf("aplicar migração %s: %w", version, err)
		}
	}
	return nil
}

// inTx roda a função numa transação: commit se der certo, rollback se falhar.
func inTx(db *sql.DB, work func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// notFound troca sql.ErrNoRows pelo erro do port, que a tela vira 404.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	return err
}

// conflict marca a recusa por índice único como ports.ErrConflict, mantendo a
// mensagem do SQLite para o log dizer qual índice recusou.
func conflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %v", ports.ErrConflict, err)
	}
	return err
}

// boolInt converte bool para o 0/1 do SQLite.
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// formatTime grava a data no formato do banco; data zero vira texto vazio.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(timeLayout)
}

// parseTime lê a data do banco; texto vazio vira data zero.
func parseTime(text string) time.Time {
	value, _ := time.Parse(timeLayout, text)
	return value
}

// accountColumns são as colunas lidas da conta.
const accountColumns = `id, name, email, password_hash, created_at`

// scanAccount lê uma linha de conta.
func scanAccount(row scanner) (domain.Account, error) {
	var account domain.Account
	var created string
	err := row.Scan(&account.ID, &account.Name, &account.Email, &account.PasswordHash, &created)
	account.CreatedAt = parseTime(created)
	return account, notFound(err)
}

// Account devolve a conta da operação; ErrNotFound se não existe.
func (r *Repository) Account(ctx context.Context) (domain.Account, error) {
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, r.acct(ctx)))
}

// DeleteAccount apaga a conta e todas as linhas dela numa transação só. As
// tabelas vêm do próprio schema (toda tabela com coluna account_id), não de
// uma lista escrita à mão: tabela criada numa migração futura entra sozinha,
// em vez de deixar dado de conta apagada para trás.
func (r *Repository) DeleteAccount(ctx context.Context) error {
	id := r.acct(ctx)
	if id == 0 {
		return ports.ErrNotFound
	}
	tables, err := r.accountTables(ctx)
	if err != nil {
		return err
	}
	return inTx(r.db, func(tx *sql.Tx) error {
		for _, table := range tables {
			// O nome vem do sqlite_master, não de quem pede; as aspas só
			// protegem nome com caractere especial.
			if _, err := tx.ExecContext(ctx, `DELETE FROM "`+table+`" WHERE account_id = ?`, id); err != nil {
				return fmt.Errorf("apagar %s: %w", table, err)
			}
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ports.ErrNotFound
		}
		return nil
	})
}

// accountTables lista as tabelas que guardam dado por conta (coluna account_id).
func (r *Repository) accountTables(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
		WHERE m.type = 'table' AND p.name = 'account_id' ORDER BY m.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// CreateAccount cria a conta e preenche o ID; e-mail repetido é ErrConflict.
func (r *Repository) CreateAccount(ctx context.Context, account *domain.Account) error {
	return r.insert(ctx, &account.ID, `INSERT INTO accounts (name, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		account.Name, account.Email, account.PasswordHash, formatTime(account.CreatedAt))
}

// AccountByEmail acha a conta pelo e-mail (já normalizado).
func (r *Repository) AccountByEmail(ctx context.Context, email string) (domain.Account, error) {
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE email = ?`, email))
}

// Accounts lista todas as contas, da mais antiga para a mais nova.
func (r *Repository) Accounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Account{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, account)
	}
	return result, rows.Err()
}

// Issuer devolve o emitente; ErrNotFound enquanto não foi configurado.
func (r *Repository) Issuer(ctx context.Context) (domain.Issuer, error) {
	var issuer domain.Issuer
	var environment string
	err := r.db.QueryRowContext(ctx, `SELECT legal_name, cnpj, municipal_registration, city_code, city_name, simples_option,
		simples_regime, special_regime, simples_tax_rate, environment, series, next_dps_number FROM issuers WHERE account_id = ?`, r.acct(ctx)).
		Scan(&issuer.LegalName, &issuer.CNPJ, &issuer.MunicipalRegistration, &issuer.CityCode, &issuer.CityName, &issuer.SimplesOption,
			&issuer.SimplesRegime, &issuer.SpecialRegime, &issuer.SimplesTaxRate, &environment, &issuer.Series, &issuer.NextDPSNumber)
	issuer.Environment = domain.Environment(environment)
	return issuer, notFound(err)
}

// SaveIssuer grava (ou substitui) o emitente.
func (r *Repository) SaveIssuer(ctx context.Context, issuer domain.Issuer) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO issuers (account_id, legal_name, cnpj, municipal_registration, city_code, city_name,
		simples_option, simples_regime, special_regime, simples_tax_rate, environment, series, next_dps_number)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id) DO UPDATE SET legal_name = excluded.legal_name, cnpj = excluded.cnpj,
		municipal_registration = excluded.municipal_registration, city_code = excluded.city_code, city_name = excluded.city_name,
		simples_option = excluded.simples_option, simples_regime = excluded.simples_regime, special_regime = excluded.special_regime,
		simples_tax_rate = excluded.simples_tax_rate, environment = excluded.environment, series = excluded.series, next_dps_number = excluded.next_dps_number`,
		r.acct(ctx), issuer.LegalName, issuer.CNPJ, issuer.MunicipalRegistration, issuer.CityCode, issuer.CityName, issuer.SimplesOption,
		issuer.SimplesRegime, issuer.SpecialRegime, int64(issuer.SimplesTaxRate), string(issuer.Environment), issuer.Series, issuer.NextDPSNumber)
	return err
}

// ReserveDPSNumber avança o contador e devolve o número reservado num único
// UPDATE ... RETURNING: atômico, duas emissões nunca recebem o mesmo número.
func (r *Repository) ReserveDPSNumber(ctx context.Context) (string, int64, error) {
	var series string
	var number int64
	err := r.db.QueryRowContext(ctx, `UPDATE issuers SET next_dps_number = next_dps_number + 1 WHERE account_id = ?
		RETURNING series, next_dps_number - 1`, r.acct(ctx)).Scan(&series, &number)
	return series, number, notFound(err)
}

// customerColumns é a lista de colunas lida e gravada para tomadores.
const customerColumns = `id, foreign_customer, document, name, email, city_code, city_name, zip, district, street, number, complement, country`

// scanner é o que QueryRow e Rows têm em comum, para um só leitor de linha.
type scanner interface{ Scan(dest ...any) error }

// scanCustomer lê uma linha de tomador.
func scanCustomer(row scanner) (domain.Customer, error) {
	var c domain.Customer
	var foreign int
	err := row.Scan(&c.ID, &foreign, &c.Document, &c.Name, &c.Email, &c.CityCode, &c.CityName, &c.ZIP, &c.District, &c.Street, &c.Number, &c.Complement, &c.Country)
	c.Foreign = foreign == 1
	return c, err
}

// Customers lista os tomadores em ordem de nome. O filtro sem acento roda em
// Go: o LIKE do SQLite não ignora acento, e a lista de um dono é pequena.
func (r *Repository) Customers(ctx context.Context, query string) ([]domain.Customer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE account_id = ? ORDER BY name`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Customer{}
	for rows.Next() {
		customer, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		if textsearch.Matches(query, customer.Name, customer.Document, domain.FormatDocument(customer.Document), customer.Email) {
			result = append(result, customer)
		}
	}
	return result, rows.Err()
}

// Customer busca um tomador pelo ID.
func (r *Repository) Customer(ctx context.Context, id int64) (domain.Customer, error) {
	customer, err := scanCustomer(r.db.QueryRowContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = ? AND account_id = ?`, id, r.acct(ctx)))
	return customer, notFound(err)
}

// SaveCustomer cria (ID zero) ou atualiza um tomador; ID inexistente é ErrNotFound.
func (r *Repository) SaveCustomer(ctx context.Context, c *domain.Customer) error {
	values := []any{boolInt(c.Foreign), c.Document, c.Name, c.Email, c.CityCode, c.CityName, c.ZIP, c.District, c.Street, c.Number, c.Complement, c.Country}
	if c.ID == 0 {
		return r.insert(ctx, &c.ID, `INSERT INTO customers (`+strings.TrimPrefix(customerColumns, "id, ")+`, account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			append(values, r.acct(ctx))...)
	}
	return r.update(ctx, `UPDATE customers SET foreign_customer = ?, document = ?, name = ?, email = ?, city_code = ?, city_name = ?,
		zip = ?, district = ?, street = ?, number = ?, complement = ?, country = ? WHERE id = ? AND account_id = ?`, append(values, c.ID, r.acct(ctx))...)
}

// insert executa o INSERT e devolve o ID gerado no ponteiro.
func (r *Repository) insert(ctx context.Context, id *int64, query string, values ...any) error {
	result, err := r.db.ExecContext(ctx, query, values...)
	if err != nil {
		return conflict(err)
	}
	*id, err = result.LastInsertId()
	return err
}

// update executa o UPDATE e devolve ErrNotFound quando nenhuma linha mudou
// (e ErrConflict quando a mudança bate num índice único).
func (r *Repository) update(ctx context.Context, query string, values ...any) error {
	result, err := r.db.ExecContext(ctx, query, values...)
	if err != nil {
		return conflict(err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// templateColumns é a lista de colunas lida e gravada para modelos.
const templateColumns = `id, name, description, national_tax_code, municipal_tax_code, nbs, service_city_code, service_city_name,
	default_amount_cents, simples_tax_rate, issqn_taxation, issqn_retention, export, customer_id, ibscbs_operation, ibscbs_cst, ibscbs_class`

// scanTemplate lê uma linha de modelo.
func scanTemplate(row scanner) (domain.Template, error) {
	var t domain.Template
	var export int
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.NationalTaxCode, &t.MunicipalTaxCode, &t.NBS, &t.ServiceCityCode, &t.ServiceCityName,
		&t.DefaultAmount, &t.SimplesTaxRate, &t.ISSQNTaxation, &t.ISSQNRetention, &export, &t.CustomerID, &t.IBSCBSOperation, &t.IBSCBSCST, &t.IBSCBSClass)
	t.Export = export == 1
	return t, err
}

// Templates lista os modelos em ordem de nome, filtrando por nome, descrição,
// código, nome do serviço ou nome do tomador.
func (r *Repository) Templates(ctx context.Context, query string) ([]domain.Template, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+templateColumns+`, COALESCE((SELECT name FROM customers c WHERE c.id = t.customer_id), '')
		FROM templates t WHERE t.account_id = ? ORDER BY name`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Template{}
	for rows.Next() {
		var t domain.Template
		var export int
		var customerName string
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.NationalTaxCode, &t.MunicipalTaxCode, &t.NBS, &t.ServiceCityCode, &t.ServiceCityName,
			&t.DefaultAmount, &t.SimplesTaxRate, &t.ISSQNTaxation, &t.ISSQNRetention, &export, &t.CustomerID, &t.IBSCBSOperation, &t.IBSCBSCST, &t.IBSCBSClass, &customerName); err != nil {
			return nil, err
		}
		t.Export = export == 1
		if textsearch.Matches(query, t.Name, t.Description, t.NationalTaxCode, domain.ServiceLabel(t.NationalTaxCode), customerName) {
			result = append(result, t)
		}
	}
	return result, rows.Err()
}

// Template busca um modelo pelo ID.
func (r *Repository) Template(ctx context.Context, id int64) (domain.Template, error) {
	template, err := scanTemplate(r.db.QueryRowContext(ctx, `SELECT `+templateColumns+` FROM templates WHERE id = ? AND account_id = ?`, id, r.acct(ctx)))
	return template, notFound(err)
}

// SaveTemplate cria (ID zero) ou atualiza um modelo.
func (r *Repository) SaveTemplate(ctx context.Context, t *domain.Template) error {
	values := []any{t.Name, t.Description, t.NationalTaxCode, t.MunicipalTaxCode, t.NBS, t.ServiceCityCode, t.ServiceCityName,
		int64(t.DefaultAmount), t.SimplesTaxRate, t.ISSQNTaxation, t.ISSQNRetention, boolInt(t.Export), t.CustomerID, t.IBSCBSOperation, t.IBSCBSCST, t.IBSCBSClass}
	if t.ID == 0 {
		return r.insert(ctx, &t.ID, `INSERT INTO templates (name, description, national_tax_code, municipal_tax_code, nbs, service_city_code,
			service_city_name, default_amount_cents, simples_tax_rate, issqn_taxation, issqn_retention, export, customer_id,
			ibscbs_operation, ibscbs_cst, ibscbs_class, account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(values, r.acct(ctx))...)
	}
	return r.update(ctx, `UPDATE templates SET name = ?, description = ?, national_tax_code = ?, municipal_tax_code = ?, nbs = ?,
		service_city_code = ?, service_city_name = ?, default_amount_cents = ?, simples_tax_rate = ?, issqn_taxation = ?,
		issqn_retention = ?, export = ?, customer_id = ?, ibscbs_operation = ?, ibscbs_cst = ?, ibscbs_class = ? WHERE id = ? AND account_id = ?`,
		append(values, t.ID, r.acct(ctx))...)
}

// invoiceColumns é a lista de colunas lida e gravada para notas.
const invoiceColumns = `id, environment, series, dps_number, customer_id, template_id, amount_cents, competence, issued_at,
	description, service_code, status, access_key, nfse_number, error_code, error_message, simulated, nfse_xml, schedule_id, schedule_period, idempotency_key`

// scanInvoice lê uma linha de nota.
func scanInvoice(row scanner) (domain.Invoice, error) {
	var i domain.Invoice
	var environment, status, competence, issued string
	var simulated int
	err := row.Scan(&i.ID, &environment, &i.Series, &i.DPSNumber, &i.CustomerID, &i.TemplateID, &i.Amount, &competence, &issued,
		&i.Description, &i.ServiceCode, &status, &i.AccessKey, &i.NFSeNumber, &i.ErrorCode, &i.ErrorMessage, &simulated, &i.XML, &i.ScheduleID, &i.SchedulePeriod, &i.IdempotencyKey)
	i.Environment, i.Status, i.Simulated = domain.Environment(environment), domain.InvoiceStatus(status), simulated == 1
	i.Competence, i.IssuedAt = parseTime(competence), parseTime(issued)
	return i, err
}

// Invoices lista as notas da mais recente para a mais antiga.
func (r *Repository) Invoices(ctx context.Context) ([]domain.Invoice, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+invoiceColumns+` FROM invoices WHERE account_id = ?`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Invoice{}
	for rows.Next() {
		invoice, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, invoice)
	}
	// Ordena por instante real, não pelo texto: datas em fusos diferentes
	// não ordenariam certo como string.
	slices.SortFunc(result, func(a, b domain.Invoice) int { return b.IssuedAt.Compare(a.IssuedAt) })
	return result, rows.Err()
}

// Invoice busca uma nota pelo ID.
func (r *Repository) Invoice(ctx context.Context, id int64) (domain.Invoice, error) {
	invoice, err := scanInvoice(r.db.QueryRowContext(ctx, `SELECT `+invoiceColumns+` FROM invoices WHERE id = ? AND account_id = ?`, id, r.acct(ctx)))
	return invoice, notFound(err)
}

// SaveInvoice cria (ID zero) ou atualiza uma nota. Número de DPS repetido no
// mesmo ambiente e série é recusado pelo índice único do banco.
func (r *Repository) SaveInvoice(ctx context.Context, i *domain.Invoice) error {
	values := []any{string(i.Environment), i.Series, i.DPSNumber, i.CustomerID, i.TemplateID, int64(i.Amount), formatTime(i.Competence),
		formatTime(i.IssuedAt), i.Description, i.ServiceCode, string(i.Status), i.AccessKey, i.NFSeNumber, i.ErrorCode, i.ErrorMessage, boolInt(i.Simulated), i.XML,
		i.ScheduleID, i.SchedulePeriod, i.IdempotencyKey}
	if i.ID == 0 {
		return r.insert(ctx, &i.ID, `INSERT INTO invoices (environment, series, dps_number, customer_id, template_id, amount_cents, competence,
			issued_at, description, service_code, status, access_key, nfse_number, error_code, error_message, simulated, nfse_xml,
			schedule_id, schedule_period, idempotency_key, account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			append(values, r.acct(ctx))...)
	}
	return r.update(ctx, `UPDATE invoices SET environment = ?, series = ?, dps_number = ?, customer_id = ?, template_id = ?, amount_cents = ?,
		competence = ?, issued_at = ?, description = ?, service_code = ?, status = ?, access_key = ?, nfse_number = ?, error_code = ?,
		error_message = ?, simulated = ?, nfse_xml = ?, schedule_id = ?, schedule_period = ?, idempotency_key = ? WHERE id = ? AND account_id = ?`,
		append(values, i.ID, r.acct(ctx))...)
}

// Schedules lista os agendamentos por dia do mês.
func (r *Repository) Schedules(ctx context.Context) ([]domain.Schedule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, template_id, customer_id, amount_cents, day, competence, description, notify_eve,
		send_to_customer, active, production, notified_period FROM schedules WHERE account_id = ? ORDER BY day, id`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Schedule{}
	for rows.Next() {
		var s domain.Schedule
		var competence string
		var notify, send, active, production int
		if err := rows.Scan(&s.ID, &s.TemplateID, &s.CustomerID, &s.Amount, &s.Day, &competence, &s.Description, &notify, &send, &active,
			&production, &s.NotifiedPeriod); err != nil {
			return nil, err
		}
		s.Competence, s.NotifyEve, s.SendToCustomer, s.Active = domain.CompetenceMode(competence), notify == 1, send == 1, active == 1
		s.Production = production == 1
		result = append(result, s)
	}
	return result, rows.Err()
}

// SaveSchedule cria (ID zero) ou atualiza um agendamento.
func (r *Repository) SaveSchedule(ctx context.Context, s *domain.Schedule) error {
	values := []any{s.TemplateID, s.CustomerID, int64(s.Amount), s.Day, string(s.Competence), s.Description, boolInt(s.NotifyEve), boolInt(s.SendToCustomer), boolInt(s.Active),
		boolInt(s.Production), s.NotifiedPeriod}
	if s.ID == 0 {
		return r.insert(ctx, &s.ID, `INSERT INTO schedules (template_id, customer_id, amount_cents, day, competence, description, notify_eve,
			send_to_customer, active, production, notified_period, account_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(values, r.acct(ctx))...)
	}
	return r.update(ctx, `UPDATE schedules SET template_id = ?, customer_id = ?, amount_cents = ?, day = ?, competence = ?, description = ?,
		notify_eve = ?, send_to_customer = ?, active = ?, production = ?, notified_period = ? WHERE id = ? AND account_id = ?`,
		append(values, s.ID, r.acct(ctx))...)
}

// DeleteSchedule apaga o agendamento. As notas que ele já gerou ficam.
func (r *Repository) DeleteSchedule(ctx context.Context, id int64) error {
	return r.update(ctx, `DELETE FROM schedules WHERE id = ? AND account_id = ?`, id, r.acct(ctx))
}

// ClaimNotification marca o aviso do mês como enviado e diz se esta chamada
// foi a que marcou. É um UPDATE condicional só: dois processos nunca avisam o
// mesmo mês duas vezes.
func (r *Repository) ClaimNotification(ctx context.Context, id int64, period string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE schedules SET notified_period = ? WHERE id = ? AND account_id = ? AND notified_period <> ?`,
		period, id, r.acct(ctx), period)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// RecordAgentContact grava (ou substitui) o último contato do cliente MCP.
func (r *Repository) RecordAgentContact(ctx context.Context, contact domain.AgentContact) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO agent_contacts_by_account (account_id, client, tool, contacted_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (account_id, client) DO UPDATE SET tool = excluded.tool, contacted_at = excluded.contacted_at`,
		r.acct(ctx), contact.Client, contact.Tool, formatTime(contact.At))
	return err
}

// AgentContacts lista os contatos, do mais recente para o mais antigo.
func (r *Repository) AgentContacts(ctx context.Context) ([]domain.AgentContact, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT client, tool, contacted_at FROM agent_contacts_by_account WHERE account_id = ?`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.AgentContact{}
	for rows.Next() {
		var contact domain.AgentContact
		var at string
		if err := rows.Scan(&contact.Client, &contact.Tool, &at); err != nil {
			return nil, err
		}
		contact.At = parseTime(at)
		result = append(result, contact)
	}
	// Ordena pelo instante real, não pelo texto (fusos diferentes).
	slices.SortFunc(result, func(a, b domain.AgentContact) int { return b.At.Compare(a.At) })
	return result, rows.Err()
}

// CertificateStatus lê a linha única; sem linha, devolve o estado vazio.
func (r *Repository) CertificateStatus(ctx context.Context) (domain.CertificateStatus, error) {
	var status domain.CertificateStatus
	var notBefore, notAfter, checked, authorized string
	err := r.db.QueryRowContext(ctx, `SELECT holder_name, cnpj, issuer_name, not_before, not_after, checked_at, portal_authorized_at
		FROM certificate_statuses WHERE account_id = ?`, r.acct(ctx)).Scan(&status.Info.HolderName, &status.Info.CNPJ, &status.Info.IssuerName,
		&notBefore, &notAfter, &checked, &authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CertificateStatus{}, nil
	}
	status.Info.NotBefore, status.Info.NotAfter = parseTime(notBefore), parseTime(notAfter)
	status.CheckedAt, status.PortalAuthorizedAt = parseTime(checked), parseTime(authorized)
	return status, err
}

// SaveCertificateStatus grava (ou substitui) a linha única.
func (r *Repository) SaveCertificateStatus(ctx context.Context, status domain.CertificateStatus) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO certificate_statuses (account_id, holder_name, cnpj, issuer_name, not_before, not_after,
		checked_at, portal_authorized_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (account_id) DO UPDATE SET holder_name = excluded.holder_name,
		cnpj = excluded.cnpj, issuer_name = excluded.issuer_name, not_before = excluded.not_before, not_after = excluded.not_after,
		checked_at = excluded.checked_at, portal_authorized_at = excluded.portal_authorized_at`,
		r.acct(ctx), status.Info.HolderName, status.Info.CNPJ, status.Info.IssuerName, formatTime(status.Info.NotBefore), formatTime(status.Info.NotAfter),
		formatTime(status.CheckedAt), formatTime(status.PortalAuthorizedAt))
	return err
}

// CreateAPIToken grava o hash do token novo e preenche o ID.
func (r *Repository) CreateAPIToken(ctx context.Context, hash string, token *domain.APIToken) error {
	token.AccountID = r.acct(ctx)
	return r.insert(ctx, &token.ID, `INSERT INTO api_tokens (token_hash, hint, created_at, account_id) VALUES (?, ?, ?, ?)`,
		hash, token.Hint, formatTime(token.CreatedAt), token.AccountID)
}

// APITokens lista os tokens da conta, do mais novo para o mais antigo.
func (r *Repository) APITokens(ctx context.Context) ([]domain.APIToken, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, hint, created_at, last_used_at, account_id FROM api_tokens WHERE account_id = ? ORDER BY id DESC`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.APIToken{}
	for rows.Next() {
		token, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, token)
	}
	return result, rows.Err()
}

// APITokenByHash acha o token pelo hash, em qualquer conta: é assim que o MCP
// por HTTP descobre de quem é a chamada (AccountID vem no token).
func (r *Repository) APITokenByHash(ctx context.Context, hash string) (domain.APIToken, error) {
	token, err := scanAPIToken(r.db.QueryRowContext(ctx, `SELECT id, hint, created_at, last_used_at, account_id FROM api_tokens WHERE token_hash = ?`, hash))
	return token, notFound(err)
}

// scanAPIToken lê uma linha de token.
func scanAPIToken(row scanner) (domain.APIToken, error) {
	var token domain.APIToken
	var created, used string
	err := row.Scan(&token.ID, &token.Hint, &created, &used, &token.AccountID)
	token.CreatedAt, token.LastUsedAt = parseTime(created), parseTime(used)
	return token, err
}

// TouchAPIToken registra o último uso do token.
func (r *Repository) TouchAPIToken(ctx context.Context, id int64, at time.Time) error {
	return r.update(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, formatTime(at), id)
}

// DeleteAPIToken revoga o token apagando o hash: ele deixa de abrir o MCP na hora.
func (r *Repository) DeleteAPIToken(ctx context.Context, id int64) error {
	return r.update(ctx, `DELETE FROM api_tokens WHERE id = ? AND account_id = ?`, id, r.acct(ctx))
}

// SavePasskey cria a passkey (ID zero) ou atualiza credencial e último uso.
func (r *Repository) SavePasskey(ctx context.Context, p *domain.Passkey) error {
	if p.ID == 0 {
		return r.insert(ctx, &p.ID, `INSERT INTO passkeys (credential_id, credential, label, created_at, last_used_at, account_id) VALUES (?, ?, ?, ?, ?, ?)`,
			p.CredentialID, p.Data, p.Label, formatTime(p.CreatedAt), formatTime(p.LastUsedAt), r.acct(ctx))
	}
	return r.update(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ? AND account_id = ?`,
		p.Data, formatTime(p.LastUsedAt), p.ID, r.acct(ctx))
}

// Passkeys lista as passkeys da conta, da mais antiga para a mais nova.
func (r *Repository) Passkeys(ctx context.Context) ([]domain.Passkey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, credential_id, credential, label, created_at, last_used_at FROM passkeys WHERE account_id = ? ORDER BY id`, r.acct(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Passkey{}
	for rows.Next() {
		var p domain.Passkey
		var created, used string
		if err := rows.Scan(&p.ID, &p.CredentialID, &p.Data, &p.Label, &created, &used); err != nil {
			return nil, err
		}
		p.CreatedAt, p.LastUsedAt = parseTime(created), parseTime(used)
		result = append(result, p)
	}
	return result, rows.Err()
}

// DeletePasskeys apaga todas as passkeys; entrar volta a ser só por senha.
func (r *Repository) DeletePasskeys(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM passkeys WHERE account_id = ?`, r.acct(ctx))
	return err
}

// SealedCertificate devolve o certificado cifrado da conta.
func (r *Repository) SealedCertificate(ctx context.Context) ([]byte, error) {
	var sealed []byte
	err := r.db.QueryRowContext(ctx, `SELECT sealed FROM sealed_certificates WHERE account_id = ?`, r.acct(ctx)).Scan(&sealed)
	return sealed, notFound(err)
}

// SaveSealedCertificate grava (ou substitui) o certificado cifrado da conta.
func (r *Repository) SaveSealedCertificate(ctx context.Context, sealed []byte) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO sealed_certificates (account_id, sealed, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (account_id) DO UPDATE SET sealed = excluded.sealed, updated_at = excluded.updated_at`,
		r.acct(ctx), sealed, formatTime(time.Now()))
	return err
}
