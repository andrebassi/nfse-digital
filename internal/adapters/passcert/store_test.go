package passcert

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// testPassword é a senha dos .pfx gerados no teste; o teste genérico confere
// que ela nunca aparece numa mensagem de erro.
const testPassword = "senha-do-pfx-de-teste"

// now é o "agora" fixo dos testes.
var now = time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)

// makePFX gera um certificado assinado por uma AC de teste e empacota em
// .pfx. legacy usa o formato RC2/3DES antigo, como os A1 da ICP-Brasil.
func makePFX(t *testing.T, commonName string, notAfter time.Time, legacy bool) []byte {
	t.Helper()
	caKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "AC DE TESTE"}, NotBefore: now.AddDate(-1, 0, 0), NotAfter: now.AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: commonName}, NotBefore: now.AddDate(0, -1, 0), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &key.PublicKey, caKey)
	leaf, _ := x509.ParseCertificate(leafDER)

	encoder := pkcs12.Modern
	if legacy {
		encoder = pkcs12.LegacyRC2
	}
	pfx, err := encoder.Encode(key, leaf, []*x509.Certificate{ca}, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return pfx
}

// fakePass troca o pass por um mapa de entradas; entrada ausente falha como o pass real.
func fakePass(t *testing.T, entries map[string]string) {
	t.Helper()
	original := runPass
	t.Cleanup(func() { runPass = original })
	runPass = func(_ context.Context, entry string) ([]byte, error) {
		value, ok := entries[entry]
		if !ok {
			return nil, ErrPassFailed
		}
		return []byte(value), nil
	}
}

// encodePFX imita o que o import-cert.sh grava: base64 quebrado em linhas e com \n final.
func encodePFX(pfx []byte) string {
	encoded := base64.StdEncoding.EncodeToString(pfx)
	var lines []string
	for len(encoded) > 76 {
		lines = append(lines, encoded[:76])
		encoded = encoded[76:]
	}
	return strings.Join(append(lines, encoded), "\n") + "\n"
}

// TestLoadValidCertificate: e-CNPJ válido abre, com CNPJ, titular, AC e a cadeia no TLS.
func TestLoadValidCertificate(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		pfx := makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(0, 2, 0), legacy)
		fakePass(t, map[string]string{"x/cert-a1-pfx": encodePFX(pfx), "x/cert-a1-password": testPassword + "\n"})
		certificate, err := Store{Prefix: "x", Now: func() time.Time { return now }}.Load(context.Background())
		if err != nil {
			t.Fatalf("legacy=%v: %v", legacy, err)
		}
		info := certificate.Info
		if info.CNPJ != "11222333000181" || info.HolderName != "EXEMPLO TECNOLOGIA LTDA" || info.IssuerName != "AC DE TESTE" {
			t.Fatalf("info: %+v", info)
		}
		if len(certificate.TLS.Certificate) != 2 || certificate.Signer == nil || certificate.Leaf == nil {
			t.Fatal("TLS sem a cadeia ou sem a chave")
		}
	}
}

// TestLoadFailures cobre cada recusa e confere que nenhuma mensagem contém a senha.
func TestLoadFailures(t *testing.T) {
	valid := encodePFX(makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(0, 2, 0), false))
	cases := []struct {
		name    string
		entries map[string]string
		want    error
	}{
		{"pass sem o pfx", map[string]string{}, ErrPassFailed},
		{"base64 inválido", map[string]string{"x/cert-a1-pfx": "não é base64!", "x/cert-a1-password": testPassword}, ErrBadPFX},
		{"pass sem a senha", map[string]string{"x/cert-a1-pfx": valid}, ErrPassFailed},
		{"senha errada", map[string]string{"x/cert-a1-pfx": valid, "x/cert-a1-password": "outra-senha"}, ErrWrongPassword},
		{"sem CNPJ", map[string]string{"x/cert-a1-pfx": encodePFX(makePFX(t, "FULANO DE TAL", now.AddDate(0, 2, 0), false)), "x/cert-a1-password": testPassword}, ErrNoCNPJ},
		{"vencido", map[string]string{"x/cert-a1-pfx": encodePFX(makePFX(t, "EXEMPLO:11222333000181", now.AddDate(0, 0, -1), false)), "x/cert-a1-password": testPassword}, ErrExpired},
	}
	for _, c := range cases {
		fakePass(t, c.entries)
		_, err := Store{Prefix: "x", Now: func() time.Time { return now }}.Load(context.Background())
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if err != nil && strings.Contains(err.Error(), testPassword) {
			t.Errorf("%s: a mensagem de erro vazou a senha", c.name)
		}
	}
}

// TestRunPassUsesLocalBinFirst roda o runPass de verdade contra um `pass`
// falso, com o PATH do teste isolado (sem o pass real), e prova que o filho
// recebe ~/.local/bin na frente do PATH, onde mora o roteador gpg2.
func TestRunPassUsesLocalBinFirst(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o700)
	script := "#!/bin/sh\nif [ \"$2\" = ok ]; then printf '%s' \"${PATH%%:*}\"; exit 0; fi\necho 'gpg: decryption failed' >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(bin, "pass"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	output, err := runPass(context.Background(), "ok")
	if err != nil || string(output) != bin {
		t.Fatalf("sucesso: %q, %v", output, err)
	}
	_, err = runPass(context.Background(), "falha")
	if !errors.Is(err, ErrPassFailed) || !strings.Contains(err.Error(), "decryption failed") {
		t.Fatalf("falha: %v", err)
	}
}
