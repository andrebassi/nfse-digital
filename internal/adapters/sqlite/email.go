package sqlite

import (
	"context"

	"nfse/internal/domain"
)

// EmailSender devolve o Gmail que envia as notas da conta.
func (r *Repository) EmailSender(ctx context.Context) (domain.EmailSender, error) {
	var sender domain.EmailSender
	var verified string
	err := r.db.QueryRowContext(ctx, `SELECT address, name, sealed_password, verified_at FROM email_senders WHERE account_id = ?`, r.acct(ctx)).
		Scan(&sender.Address, &sender.Name, &sender.SealedPassword, &verified)
	sender.VerifiedAt = parseTime(verified)
	return sender, notFound(err)
}

// SaveEmailSender grava (ou substitui) o remetente da conta.
func (r *Repository) SaveEmailSender(ctx context.Context, sender domain.EmailSender) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO email_senders (account_id, address, name, sealed_password, verified_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (account_id) DO UPDATE SET address = excluded.address, name = excluded.name,
		sealed_password = excluded.sealed_password, verified_at = excluded.verified_at`,
		r.acct(ctx), sender.Address, sender.Name, sender.SealedPassword, formatTime(sender.VerifiedAt))
	return err
}

// CustomerEmail devolve o envio por e-mail salvo de um tomador da conta.
func (r *Repository) CustomerEmail(ctx context.Context, customerID int64) (domain.CustomerEmail, error) {
	settings := domain.CustomerEmail{CustomerID: customerID}
	var auto, pdf, xml int
	err := r.db.QueryRowContext(ctx, `SELECT auto_send, to_addresses, cc_addresses, subject, body, attach_pdf, attach_xml
		FROM customer_emails WHERE account_id = ? AND customer_id = ?`, r.acct(ctx), customerID).
		Scan(&auto, &settings.To, &settings.CC, &settings.Subject, &settings.Body, &pdf, &xml)
	settings.AutoSend, settings.AttachPDF, settings.AttachXML = auto == 1, pdf == 1, xml == 1
	return settings, notFound(err)
}

// SaveCustomerEmail grava (ou substitui) o envio por e-mail do tomador.
func (r *Repository) SaveCustomerEmail(ctx context.Context, s domain.CustomerEmail) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO customer_emails (account_id, customer_id, auto_send, to_addresses, cc_addresses, subject, body, attach_pdf, attach_xml)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, customer_id) DO UPDATE SET auto_send = excluded.auto_send, to_addresses = excluded.to_addresses,
		cc_addresses = excluded.cc_addresses, subject = excluded.subject, body = excluded.body,
		attach_pdf = excluded.attach_pdf, attach_xml = excluded.attach_xml`,
		r.acct(ctx), s.CustomerID, boolInt(s.AutoSend), s.To, s.CC, s.Subject, s.Body, boolInt(s.AttachPDF), boolInt(s.AttachXML))
	return err
}

// RecordInvoiceEmail grava um envio (ou a falha dele) e preenche o ID.
func (r *Repository) RecordInvoiceEmail(ctx context.Context, sent *domain.InvoiceEmail) error {
	return r.insert(ctx, &sent.ID, `INSERT INTO invoice_emails (account_id, invoice_id, sent_at, to_addresses, automatic, error) VALUES (?, ?, ?, ?, ?, ?)`,
		r.acct(ctx), sent.InvoiceID, formatTime(sent.SentAt), sent.To, boolInt(sent.Automatic), sent.Error)
}

// InvoiceEmails lista os envios de uma nota, do mais recente para o mais
// antigo (pelo ID, que cresce com o tempo, e não pelo texto da data).
func (r *Repository) InvoiceEmails(ctx context.Context, invoiceID int64) ([]domain.InvoiceEmail, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, invoice_id, sent_at, to_addresses, automatic, error FROM invoice_emails
		WHERE account_id = ? AND invoice_id = ? ORDER BY id DESC`, r.acct(ctx), invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []domain.InvoiceEmail
	for rows.Next() {
		var sent domain.InvoiceEmail
		var at string
		var automatic int
		if err := rows.Scan(&sent.ID, &sent.InvoiceID, &at, &sent.To, &automatic, &sent.Error); err != nil {
			return nil, err
		}
		sent.SentAt, sent.Automatic = parseTime(at), automatic == 1
		list = append(list, sent)
	}
	return list, rows.Err()
}
