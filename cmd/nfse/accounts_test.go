package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/vaultcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestPassPrefixFor: a conta 1 continua na pasta de sempre (o certificado do
// dono original não muda de lugar); as outras ganham subpasta própria, para
// um certificado nunca sobrescrever o de outra conta; NFSE_PASS_PREFIX troca
// a raiz das duas.
func TestPassPrefixFor(t *testing.T) {
	t.Setenv("NFSE_PASS_PREFIX", "")
	cases := map[int64]string{0: "bassi/nfse", 1: "bassi/nfse", 2: "bassi/nfse/accounts/2", 15: "bassi/nfse/accounts/15"}
	for account, want := range cases {
		if got := passPrefixFor(account); got != want {
			t.Errorf("conta %d: %q, quer %q", account, got, want)
		}
	}
	t.Setenv("NFSE_PASS_PREFIX", "teste/nfse")
	if passPrefixFor(1) != "teste/nfse" || passPrefixFor(3) != "teste/nfse/accounts/3" {
		t.Error("NFSE_PASS_PREFIX não trocou a raiz")
	}
}

// TestCertificatesFollowAccount: o leitor usa a conta do context quando há,
// senão a fixa.
func TestCertificatesFollowAccount(t *testing.T) {
	t.Setenv("NFSE_PASS_PREFIX", "")
	vault = nil
	reader, ok := certificatesFor(1).(accountCertificates)
	if !ok {
		t.Fatal("sem NFSE_CERT_KEY o certificado tem de vir do pass")
	}
	if prefix := reader.store(context.Background()).Prefix; prefix != "bassi/nfse" {
		t.Errorf("conta fixa: %q", prefix)
	}
	if prefix := reader.store(ports.WithAccount(context.Background(), 4)).Prefix; prefix != "bassi/nfse/accounts/4" {
		t.Errorf("conta do context: %q", prefix)
	}
}

// TestConfigureVault: sem NFSE_CERT_KEY o certificado fica no pass; com chave
// válida vai para o cofre, preso à conta pedida; chave inválida é erro (subir
// sem cofre emitiria com o certificado errado).
func TestConfigureVault(t *testing.T) {
	repo := memory.New(domain.Issuer{})
	t.Setenv("NFSE_CERT_KEY", "")
	if err := configureVault(repo); err != nil || vault != nil {
		t.Fatalf("sem chave: %v, %v", vault, err)
	}
	t.Setenv("NFSE_CERT_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err := configureVault(repo); err != nil || vault == nil {
		t.Fatalf("chave válida: %v", err)
	}
	defer func() { vault = nil }()
	scoped, ok := certificatesFor(3).(vaultcert.Vault)
	if !ok || scoped.Account != 3 || vault.Account != 0 {
		t.Fatalf("cofre por conta: %+v", scoped)
	}
	t.Setenv("NFSE_CERT_KEY", "curta")
	if err := configureVault(repo); !errors.Is(err, vaultcert.ErrBadKey) {
		t.Fatalf("chave inválida: %v", err)
	}
}

// TestListenAddress: sem NFSE_LISTEN, loopback na porta da flag; loopback
// explícito passa; fora do loopback só com host público; endereço sem porta
// é erro.
func TestListenAddress(t *testing.T) {
	cases := []struct {
		listen, public, want string
		fails                bool
	}{
		{"", "", "127.0.0.1:8735", false},
		{"127.0.0.1:9000", "", "127.0.0.1:9000", false},
		{"0.0.0.0:8080", "", "", true},
		{"[::]:8080", "", "", true},
		{"painel.local:8080", "", "", true},
		{"0.0.0.0:8080", "nfse.exemplo.example", "0.0.0.0:8080", false},
		{"0.0.0.0", "nfse.exemplo.example", "", true},
	}
	for _, c := range cases {
		got, err := listenAddress(c.listen, 8735, c.public)
		if (err != nil) != c.fails || got != c.want {
			t.Errorf("%q/%q: %q, %v", c.listen, c.public, got, err)
		}
	}
}

// TestChooseAccount: sem e-mail, a primeira conta; com e-mail, a dele; e-mail
// sem conta e banco sem conta nenhuma explicam o que fazer.
func TestChooseAccount(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(domain.Issuer{})
	if _, err := chooseAccount(ctx, repo, ""); err == nil || !strings.Contains(err.Error(), "crie a primeira") {
		t.Fatalf("sem conta: %v", err)
	}
	first, second := domain.Account{Email: "a@exemplo.example"}, domain.Account{Email: "b@exemplo.example"}
	_ = repo.CreateAccount(ctx, &first)
	_ = repo.CreateAccount(ctx, &second)
	if account, _ := chooseAccount(ctx, repo, ""); account.ID != first.ID {
		t.Errorf("padrão: %+v", account)
	}
	if account, _ := chooseAccount(ctx, repo, " B@Exemplo.Example "); account.ID != second.ID {
		t.Errorf("por e-mail: %+v", account)
	}
	if _, err := chooseAccount(ctx, repo, "x@exemplo.example"); err == nil || !strings.Contains(err.Error(), "não há conta") {
		t.Errorf("e-mail sem conta: %v", err)
	}
}
