package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"nfse/internal/adapters/maildocs"
	"nfse/internal/adapters/secretbox"
	"nfse/internal/adapters/sefin"
	"nfse/internal/adapters/smtpmail"
	"nfse/internal/app"
	"nfse/internal/domain"
)

// emailKeyFile é a chave que sela a senha de app do Gmail no painel local
// (sem NFSE_CERT_KEY). Fica ao lado do banco padrão, com permissão 600.
func emailKeyFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "nfse-digital", "secret.key")
}

// withEmail liga o envio por e-mail no caso de uso: Gmail por SMTP
// (NFSE_SMTP_ADDR troca o servidor, para teste local), a senha de app selada
// pela chave do servidor — NFSE_CERT_KEY no Fly, o arquivo local fora dele —
// e os anexos gerados aqui (o XML ausente é buscado na SEFIN pela chave).
// accountID é a conta fixa de quem não traz conta no context (agendador,
// MCP local); zero = sempre a do context (painel).
func withEmail(service *app.Service, accountID int64) error {
	var key []byte
	var err error
	if encoded := os.Getenv("NFSE_CERT_KEY"); encoded != "" {
		key, err = secretbox.ParseKey(encoded)
	} else {
		key, err = secretbox.LoadOrCreateKey(emailKeyFile())
	}
	if err != nil {
		return err
	}
	service.Mailer = smtpmail.Gmail{Addr: os.Getenv("NFSE_SMTP_ADDR"), Timeout: 30 * time.Second}
	service.Sealer = secretbox.Box{Key: key, Purpose: "gmail", Account: accountID}
	service.Documents = maildocs.Documents{FetchXML: func(ctx context.Context, invoice domain.Invoice) ([]byte, error) {
		certificate, err := certificatesFor(accountID).Load(ctx)
		if err != nil {
			return nil, err
		}
		consulter := sefin.Consulter{Reader: sefin.NewReader(certificate.TLS, nil), SefinURL: sefin.BaseURL(invoice.Environment)}
		return consulter.NFSeXML(ctx, invoice.AccessKey)
	}}
	return nil
}
