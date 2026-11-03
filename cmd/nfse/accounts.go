package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"nfse/internal/adapters/passcert"
	"nfse/internal/adapters/sqlite"
	"nfse/internal/adapters/turso"
	"nfse/internal/adapters/vaultcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// cliAccount é a conta que a linha de comando usa: NFSE_ACCOUNT (e-mail) ou,
// sem ela, a primeira conta criada (o dono original). Preenchida por
// openDatabase.
var cliAccount int64

// openDatabase abre o SQLite do caminho dado (ou o padrão) já preso à conta
// da linha de comando.
func openDatabase(path string) (*sqlite.Repository, error) {
	if path == "" {
		var err error
		if path, err = sqlite.DefaultPath(); err != nil {
			return nil, err
		}
	}
	database, err := openStore(path)
	if err != nil {
		return nil, err
	}
	if err := configureVault(database); err != nil {
		database.Close()
		return nil, err
	}
	account, err := chooseAccount(context.Background(), database, os.Getenv("NFSE_ACCOUNT"))
	if err != nil {
		database.Close()
		return nil, err
	}
	cliAccount = account.ID
	return database.For(account.ID), nil
}

// openStore abre o banco pela forma do NFSE_DB/--db: URL do Turso
// (libsql://, https://) vai para o adapter turso, com o token no secret
// TURSO_AUTH_TOKEN (no Fly, sem volume); qualquer outra coisa é caminho de
// arquivo SQLite, como na máquina do dono.
func openStore(dsn string) (*sqlite.Repository, error) {
	if turso.IsRemote(dsn) {
		return turso.Open(dsn, os.Getenv("TURSO_AUTH_TOKEN"))
	}
	return sqlite.Open(dsn)
}

// chooseAccount acha a conta pelo e-mail, ou a primeira quando o e-mail vem
// vazio. Sem conta nenhuma, orienta a criar uma pelo painel.
func chooseAccount(ctx context.Context, repo ports.Repository, email string) (domain.Account, error) {
	if email != "" {
		account, err := repo.AccountByEmail(ctx, domain.NormalizeEmail(email))
		if errors.Is(err, ports.ErrNotFound) {
			return domain.Account{}, fmt.Errorf("NFSE_ACCOUNT=%s: não há conta com este e-mail", email)
		}
		return account, err
	}
	accounts, err := repo.Accounts(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	if len(accounts) == 0 {
		return domain.Account{}, errors.New("nenhuma conta neste banco: crie a primeira no painel (nfse serve → Criar conta)")
	}
	return accounts[0], nil
}

// passPrefixFor é a pasta do pass do certificado da conta. A conta 1 (o dono
// original) continua em bassi/nfse, onde o certificado sempre esteve; as
// demais ganham uma subpasta, para um certificado nunca sobrescrever outro.
// NFSE_PASS_PREFIX troca a raiz.
func passPrefixFor(accountID int64) string {
	base := passcert.DefaultPrefix
	if prefix := os.Getenv("NFSE_PASS_PREFIX"); prefix != "" {
		base = prefix
	}
	if accountID <= 1 {
		return base
	}
	return fmt.Sprintf("%s/accounts/%d", base, accountID)
}

// passPrefix é a pasta do pass da conta da linha de comando.
func passPrefix() string {
	return passPrefixFor(cliAccount)
}

// certificateStore abre e grava o certificado A1 da conta da operação.
type certificateStore interface {
	Load(ctx context.Context) (passcert.Certificate, error)
	Replace(ctx context.Context, pfx []byte, password, expectedCNPJ string) (domain.CertificateInfo, error)
}

// vault é o cofre cifrado no banco, ligado quando NFSE_CERT_KEY existe (no
// Fly, onde não há pass); nil = certificado no pass, como na máquina do dono.
var vault *vaultcert.Vault

// configureVault liga o cofre se NFSE_CERT_KEY estiver definida. Chave
// presente mas inválida é erro: subir sem cofre emitiria com o pass errado.
func configureVault(repo ports.Repository) error {
	encoded := os.Getenv("NFSE_CERT_KEY")
	if encoded == "" {
		vault = nil
		return nil
	}
	key, err := vaultcert.ParseKey(encoded)
	if err != nil {
		return err
	}
	vault = &vaultcert.Vault{Repo: repo, Key: key, Now: time.Now}
	return nil
}

// accountCertificates abre o certificado da conta da operação no pass: a do
// context (painel, MCP) ou a fixa (CLI, agendador de uma conta).
type accountCertificates struct {
	account int64
}

// certificatesFor devolve onde está o certificado da conta: o cofre, quando
// ligado, ou o pass. Conta fixa zero = só a do context.
func certificatesFor(accountID int64) certificateStore {
	if vault != nil {
		scoped := *vault
		scoped.Account = accountID
		return scoped
	}
	return accountCertificates{account: accountID}
}

// store é o passcert da conta da operação.
func (a accountCertificates) store(ctx context.Context) passcert.Store {
	id := ports.AccountID(ctx)
	if id == 0 {
		id = a.account
	}
	return passcert.Store{Prefix: passPrefixFor(id), Now: time.Now}
}

// Load abre o A1 da conta (cumpre sefin.CertificateLoader).
func (a accountCertificates) Load(ctx context.Context) (passcert.Certificate, error) {
	return a.store(ctx).Load(ctx)
}

// Replace confere e grava o A1 na pasta da conta.
func (a accountCertificates) Replace(ctx context.Context, pfx []byte, password, expectedCNPJ string) (domain.CertificateInfo, error) {
	return a.store(ctx).Replace(ctx, pfx, password, expectedCNPJ)
}
