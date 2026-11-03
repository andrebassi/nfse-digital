package vaultcert

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/passcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// password é a senha dos .pfx de teste: o teste confere que ela nunca aparece
// no blob gravado nem numa mensagem de erro.
const password = "senha-secreta-do-pfx-123"

// now é o "agora" fixo dos testes.
var now = time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)

// makePFX gera um e-CNPJ de teste autoassinado e empacota em .pfx.
func makePFX(t *testing.T, commonName string, notAfter time.Time) []byte {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: commonName}, NotBefore: now.AddDate(0, -1, 0), NotAfter: notAfter}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	leaf, _ := x509.ParseCertificate(der)
	pfx, err := pkcs12.Modern.Encode(key, leaf, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	return pfx
}

// newVault monta o cofre com chave aleatória sobre o banco em memória.
func newVault(t *testing.T) (Vault, *memory.Repository) {
	key := make([]byte, keySize)
	_, _ = rand.Read(key)
	repo := memory.New(domain.Issuer{})
	return Vault{Repo: repo, Key: key, Now: func() time.Time { return now }}, repo
}

// TestSealAndOpen: o certificado volta igual; o blob gravado não contém nem a
// senha nem o .pfx em claro; dois envios do mesmo arquivo dão blobs
// diferentes (nonce novo a cada gravação).
func TestSealAndOpen(t *testing.T) {
	vault, repo := newVault(t)
	ctx := ports.WithAccount(context.Background(), 7)
	pfx := makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(1, 0, 0))
	info, err := vault.Replace(ctx, pfx, password, "11222333000181")
	if err != nil || info.CNPJ != "11222333000181" {
		t.Fatalf("selar: %+v, %v", info, err)
	}
	sealed, _ := repo.SealedCertificate(ctx)
	if bytes.Contains(sealed, []byte(password)) || bytes.Contains(sealed, pfx[:64]) || bytes.Contains(sealed, []byte(base64.StdEncoding.EncodeToString(pfx)[:40])) {
		t.Fatal("a senha ou o .pfx ficaram em claro no blob")
	}
	certificate, err := vault.Load(ctx)
	if err != nil || certificate.Info != info || certificate.Signer == nil {
		t.Fatalf("abrir: %+v, %v", certificate.Info, err)
	}
	_, _ = vault.Replace(ctx, pfx, password, "")
	again, _ := repo.SealedCertificate(ctx)
	if bytes.Equal(again, sealed) {
		t.Fatal("dois selos iguais: o nonce não mudou")
	}
	// Conta fixa (agendador) quando o context não traz conta.
	fixed := vault
	fixed.Account = 7
	if _, err := fixed.Load(context.Background()); err != nil {
		t.Fatalf("conta fixa: %v", err)
	}
}

// TestRefusals: certificado inválido não grava; blob de outra conta, chave
// trocada e blob adulterado não abrem; sem conta e sem certificado explicam.
func TestRefusals(t *testing.T) {
	vault, repo := newVault(t)
	ctxA, ctxB := ports.WithAccount(context.Background(), 1), ports.WithAccount(context.Background(), 2)
	good := makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(1, 0, 0))
	cases := map[string]struct {
		pfx      []byte
		password string
		expected string
		want     error
	}{
		"senha errada": {good, "outra-senha", "", passcert.ErrWrongPassword},
		"vencido":      {makePFX(t, "X:11222333000181", now.AddDate(0, 0, -1)), password, "", passcert.ErrExpired},
		"outro CNPJ":   {good, password, "56789012000100", passcert.ErrOtherCNPJ},
	}
	for name, c := range cases {
		_, err := vault.Replace(ctxA, c.pfx, c.password, c.expected)
		if !errors.Is(err, c.want) || strings.Contains(err.Error(), password) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := repo.SealedCertificate(ctxA); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("recusa gravou certificado")
	}
	// No envio pela tela a senha foi DIGITADA: a mensagem fala da senha do
	// certificado, nunca do pass (que nem existe no servidor).
	if _, err := vault.Replace(ctxA, good, "outra-senha", ""); !strings.Contains(err.Error(), "senha do certificado incorreta") || strings.Contains(err.Error(), "pass") {
		t.Fatalf("mensagem de senha errada: %v", err)
	}
	if _, err := vault.Load(ctxA); !errors.Is(err, ErrNoCertificate) {
		t.Fatalf("sem certificado: %v", err)
	}
	if _, err := vault.Load(context.Background()); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("sem conta: %v", err)
	}
	if _, err := vault.Replace(context.Background(), good, password, ""); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("selar sem conta: %v", err)
	}

	_, _ = vault.Replace(ctxA, good, password, "")
	sealed, _ := repo.SealedCertificate(ctxA)
	// O blob da conta 1 copiado para a conta 2 não abre: a conta é dado autenticado.
	_ = repo.SaveSealedCertificate(ctxB, sealed)
	if _, err := vault.Load(ctxB); !errors.Is(err, ErrTampered) {
		t.Fatalf("blob de outra conta: %v", err)
	}
	other, _ := newVault(t)
	other.Repo = repo
	if _, err := other.Load(ctxA); !errors.Is(err, ErrTampered) {
		t.Fatalf("chave trocada: %v", err)
	}
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	_ = repo.SaveSealedCertificate(ctxA, flipped)
	if _, err := vault.Load(ctxA); !errors.Is(err, ErrTampered) {
		t.Fatalf("adulterado: %v", err)
	}
	_ = repo.SaveSealedCertificate(ctxA, []byte("curto"))
	if _, err := vault.Load(ctxA); !errors.Is(err, ErrTampered) {
		t.Fatalf("blob curto: %v", err)
	}
	bad := vault
	bad.Key = []byte("curta")
	if _, err := bad.Load(ctxA); !errors.Is(err, ErrBadKey) {
		t.Fatalf("chave curta no Load: %v", err)
	}
	if _, err := bad.Replace(ctxA, good, password, ""); !errors.Is(err, ErrBadKey) {
		t.Fatalf("chave curta no Replace: %v", err)
	}
}

// TestParseKey: 32 bytes em base64 passam; tamanho errado e texto inválido não.
func TestParseKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if parsed, err := ParseKey(key); err != nil || len(parsed) != 32 {
		t.Fatalf("válida: %v", err)
	}
	for _, bad := range []string{"", "não-é-base64!", base64.StdEncoding.EncodeToString([]byte("curta"))} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrBadKey) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
