// Package passcert carrega o certificado A1 do pass direto para a memória.
// O .pfx e a senha nunca tocam o disco: saem do `pass show` pelo stdout e
// viram uma chave privada na memória do processo.
package passcert

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"nfse/internal/domain"
)

// DefaultPrefix é a pasta do pass onde a importação gravou o certificado.
const DefaultPrefix = "bassi/nfse"

// Erros que a CLI e a tela mostram; nenhum carrega a senha nem bytes da chave.
var (
	ErrPassFailed    = errors.New("não consegui ler o pass (aprovação negada, celular fora ou entrada inexistente)")
	ErrBadPFX        = errors.New("o .pfx guardado no pass não é base64 válido")
	ErrWrongPassword = errors.New("senha do certificado incorreta: confira a senha do arquivo .pfx")
	ErrExpired       = errors.New("o certificado A1 venceu")
	ErrNoCNPJ        = errors.New("o certificado não traz CNPJ (não é um e-CNPJ da ICP-Brasil)")
	ErrOtherCNPJ     = errors.New("o certificado é de outro CNPJ, não do emitente configurado")
)

// Certificate é o A1 aberto: o par para o mTLS, o certificado e quem assina.
type Certificate struct {
	TLS    tls.Certificate
	Leaf   *x509.Certificate
	Signer crypto.Signer
	Info   domain.CertificateInfo
}

// runPass executa `pass show <entrada>` e devolve o stdout. É variável para o
// teste substituir o pass por bytes fixos. O PATH começa em ~/.local/bin
// porque lá está o roteador gpg2 do dono (YubiKey ou aprovação no celular);
// um gpg2 do Nix na frente desviaria o pedido para o agente errado.
var runPass = func(ctx context.Context, entry string) ([]byte, error) {
	command := exec.CommandContext(ctx, "pass", "show", entry)
	home, _ := os.UserHomeDir()
	command.Env = append(os.Environ(), "PATH="+home+"/.local/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrPassFailed, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// Store lê o certificado de <Prefix>/cert-a1-pfx (base64) e a senha de
// <Prefix>/cert-a1-password.
type Store struct {
	Prefix string
	Now    func() time.Time
}

// Load abre o certificado. Recusa vencido e sem CNPJ: nenhum dos dois serve
// para assinar DPS.
func (s Store) Load(ctx context.Context) (Certificate, error) {
	encoded, err := runPass(ctx, s.Prefix+"/cert-a1-pfx")
	if err != nil {
		return Certificate{}, err
	}
	pfx, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(encoded)), ""))
	if err != nil {
		return Certificate{}, ErrBadPFX
	}
	password, err := runPass(ctx, s.Prefix+"/cert-a1-password")
	if err != nil {
		return Certificate{}, err
	}
	// O pass grava a senha com quebra de linha no fim; só ela sai.
	secret := strings.TrimRight(string(password), "\r\n")
	return Decode(pfx, secret, s.Now())
}

// Decode abre o .pfx com a senha e monta o Certificate. Separado de Load para
// testar sem pass. O erro nunca inclui a senha nem o conteúdo do arquivo.
func Decode(pfx []byte, password string, now time.Time) (Certificate, error) {
	key, leaf, chain, err := pkcs12.DecodeChain(pfx, password)
	if err != nil {
		return Certificate{}, ErrWrongPassword
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return Certificate{}, fmt.Errorf("chave privada de tipo %T não assina", key)
	}
	holder, cnpj := domain.SplitICPCommonName(leaf.Subject.CommonName)
	if cnpj == "" {
		return Certificate{}, ErrNoCNPJ
	}
	info := domain.CertificateInfo{HolderName: holder, CNPJ: cnpj, IssuerName: leaf.Issuer.CommonName, NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}
	if info.Expired(now) {
		return Certificate{}, fmt.Errorf("%w em %s", ErrExpired, leaf.NotAfter.Format("02/01/2006"))
	}
	// A cadeia vai junto no TLS: a SEFIN valida o certificado até a AC raiz.
	raw := [][]byte{leaf.Raw}
	for _, ca := range chain {
		raw = append(raw, ca.Raw)
	}
	return Certificate{
		TLS:    tls.Certificate{Certificate: raw, PrivateKey: signer, Leaf: leaf},
		Leaf:   leaf,
		Signer: signer,
		Info:   info,
	}, nil
}

// runPassInsert grava a entrada com `pass insert -m -f`, recebendo o conteúdo
// pelo stdin (nunca por argumento, que apareceria no `ps`). Gravar só usa a
// chave PÚBLICA do cofre: não pede aprovação no celular. Variável para o teste.
var runPassInsert = func(ctx context.Context, entry string, content []byte) error {
	command := exec.CommandContext(ctx, "pass", "insert", "-m", "-f", entry)
	home, _ := os.UserHomeDir()
	command.Env = append(os.Environ(), "PATH="+home+"/.local/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	command.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: %s", ErrPassFailed, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Replace confere o .pfx com a senha (abre em memória, recusa vencido, sem
// CNPJ e — com expectedCNPJ preenchido — de outro CNPJ) e só então grava os
// dois no pass, nas mesmas entradas que Load lê. Nada é gravado se a
// conferência falhar.
func (s Store) Replace(ctx context.Context, pfx []byte, password, expectedCNPJ string) (domain.CertificateInfo, error) {
	certificate, err := Decode(pfx, password, s.Now())
	if err != nil {
		return domain.CertificateInfo{}, err
	}
	if expectedCNPJ != "" && certificate.Info.CNPJ != expectedCNPJ {
		return domain.CertificateInfo{}, fmt.Errorf("%w (%s)", ErrOtherCNPJ, domain.FormatDocument(certificate.Info.CNPJ))
	}
	if err := runPassInsert(ctx, s.Prefix+"/cert-a1-pfx", []byte(base64.StdEncoding.EncodeToString(pfx)+"\n")); err != nil {
		return domain.CertificateInfo{}, err
	}
	if err := runPassInsert(ctx, s.Prefix+"/cert-a1-password", []byte(password+"\n")); err != nil {
		return domain.CertificateInfo{}, err
	}
	return certificate.Info, nil
}
