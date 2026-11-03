// Package vaultcert guarda o certificado A1 de cada conta cifrado no banco,
// para o painel emitir num servidor sem pass (deploy no Fly). O .pfx e a
// senha são selados JUNTOS num blob AES-256-GCM: nenhum dos dois fica em
// claro no banco, e só existem decifrados na memória, na hora de assinar.
package vaultcert

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nfse/internal/adapters/passcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// keySize é o tamanho da chave mestra: 32 bytes = AES-256.
const keySize = 32

// Erros que a tela mostra; nenhum carrega a senha nem bytes do certificado.
var (
	ErrBadKey      = errors.New("chave mestra do cofre inválida: NFSE_CERT_KEY precisa ser 32 bytes em base64")
	ErrNoCertificate = errors.New("esta conta ainda não enviou o certificado A1 (Configurações → Certificado)")
	ErrTampered    = errors.New("o certificado guardado não abre com a chave do servidor (chave trocada ou dado adulterado)")
	ErrNoAccount   = errors.New("operação de certificado sem conta")
)

// ParseKey lê a chave mestra em base64 (o formato do secret).
func ParseKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != keySize {
		return nil, ErrBadKey
	}
	return key, nil
}

// Vault abre e grava o certificado da conta da operação.
type Vault struct {
	Repo ports.Repository
	Key  []byte
	Now  func() time.Time
	// Account é a conta usada quando o context não traz uma (agendador).
	Account int64
}

// secret é o que vai selado: o arquivo e a senha, sempre juntos.
type secret struct {
	PFX      []byte `json:"pfx"`
	Password string `json:"password"`
}

// account é a conta da operação: a do context, senão a fixa.
func (v Vault) account(ctx context.Context) (int64, error) {
	if id := ports.AccountID(ctx); id != 0 {
		return id, nil
	}
	if v.Account != 0 {
		return v.Account, nil
	}
	return 0, ErrNoAccount
}

// aead monta o AES-256-GCM com a chave mestra.
func (v Vault) aead() (cipher.AEAD, error) {
	if len(v.Key) != keySize {
		return nil, ErrBadKey
	}
	block, err := aes.NewCipher(v.Key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// associated é o dado autenticado: o número da conta. Um blob selado para
// uma conta não abre se for copiado para a linha de outra conta.
func associated(accountID int64) []byte {
	return binary.BigEndian.AppendUint64([]byte("nfse-digital:cert:"), uint64(accountID))
}

// Replace confere o .pfx com a senha (vencido, sem CNPJ e — com expectedCNPJ
// — CNPJ de outra empresa são recusados antes de gravar) e grava os dois
// selados. Nada é gravado se a conferência falhar.
func (v Vault) Replace(ctx context.Context, pfx []byte, password, expectedCNPJ string) (domain.CertificateInfo, error) {
	accountID, err := v.account(ctx)
	if err != nil {
		return domain.CertificateInfo{}, err
	}
	certificate, err := passcert.Decode(pfx, password, v.Now())
	if err != nil {
		return domain.CertificateInfo{}, err
	}
	if expectedCNPJ != "" && certificate.Info.CNPJ != expectedCNPJ {
		return domain.CertificateInfo{}, fmt.Errorf("%w (%s)", passcert.ErrOtherCNPJ, domain.FormatDocument(certificate.Info.CNPJ))
	}
	aead, err := v.aead()
	if err != nil {
		return domain.CertificateInfo{}, err
	}
	plain, _ := json.Marshal(secret{PFX: pfx, Password: password})
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return domain.CertificateInfo{}, err
	}
	// O nonce vai na frente do texto cifrado: cada gravação usa um novo.
	sealed := aead.Seal(nonce, nonce, plain, associated(accountID))
	if err := v.Repo.SaveSealedCertificate(ports.WithAccount(ctx, accountID), sealed); err != nil {
		return domain.CertificateInfo{}, err
	}
	return certificate.Info, nil
}

// Load abre o certificado da conta (cumpre sefin.CertificateLoader).
func (v Vault) Load(ctx context.Context) (passcert.Certificate, error) {
	accountID, err := v.account(ctx)
	if err != nil {
		return passcert.Certificate{}, err
	}
	sealed, err := v.Repo.SealedCertificate(ports.WithAccount(ctx, accountID))
	if errors.Is(err, ports.ErrNotFound) {
		return passcert.Certificate{}, ErrNoCertificate
	}
	if err != nil {
		return passcert.Certificate{}, err
	}
	aead, err := v.aead()
	if err != nil {
		return passcert.Certificate{}, err
	}
	if len(sealed) < aead.NonceSize() {
		return passcert.Certificate{}, ErrTampered
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], associated(accountID))
	if err != nil {
		return passcert.Certificate{}, ErrTampered
	}
	var opened secret
	if err := json.Unmarshal(plain, &opened); err != nil {
		return passcert.Certificate{}, ErrTampered
	}
	return passcert.Decode(opened.PFX, opened.Password, v.Now())
}
