// Package secretbox sela segredos pequenos da conta (a senha de app do Gmail)
// com AES-256-GCM para guardar no banco. O dado autenticado leva o propósito
// e o número da conta: um blob copiado para outra conta, ou usado para outro
// fim, não abre.
//
// É o mesmo desenho do vaultcert (que sela o certificado A1); fica separado
// porque o vaultcert junta a conferência do .pfx ao selo.
package secretbox

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nfse/internal/ports"
)

// keySize é o tamanho da chave: 32 bytes = AES-256.
const keySize = 32

// randomRead é a fonte de aleatoriedade (chave e nonce); variável para o
// teste simular a falha do sistema.
var randomRead = rand.Read

// Erros que chegam à tela; nenhum carrega o segredo.
var (
	ErrBadKey    = errors.New("chave do cofre inválida: precisa ser 32 bytes em base64")
	ErrTampered  = errors.New("o segredo guardado não abre com a chave deste servidor (chave trocada ou dado adulterado): informe a senha de novo")
	ErrNoAccount = errors.New("segredo sem conta")
)

// Box sela e abre segredos de um propósito ("gmail") da conta do context.
// Account é a conta usada quando o context não traz uma (agendador e CLI,
// que escolhem a conta no repositório), como no vaultcert.
type Box struct {
	Key     []byte
	Purpose string
	Account int64
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.SecretSealer = Box{}

// ParseKey lê a chave em base64 (o formato do NFSE_CERT_KEY).
func ParseKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != keySize {
		return nil, ErrBadKey
	}
	return key, nil
}

// LoadOrCreateKey lê a chave do arquivo e, se ele não existe, gera uma nova
// e grava com permissão 600. Serve ao painel local, que não tem
// NFSE_CERT_KEY: perder o arquivo só obriga a informar a senha de app de novo.
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return ParseKey(string(data))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, keySize)
	if _, err := randomRead(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// O_EXCL: dois processos subindo juntos nunca sobrescrevem a chave um do
	// outro; quem perder a corrida falha e, reiniciado, lê a do vencedor.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	_, err = file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
	return key, err
}

// aead monta o AES-256-GCM com a chave.
func (b Box) aead() (cipher.AEAD, error) {
	if len(b.Key) != keySize {
		return nil, ErrBadKey
	}
	// Com 32 bytes conferidos acima, NewCipher não falha (só recusa tamanho).
	block, _ := aes.NewCipher(b.Key)
	return cipher.NewGCM(block)
}

// associated é o dado autenticado: propósito e conta.
func (b Box) associated(ctx context.Context) ([]byte, error) {
	account := ports.AccountID(ctx)
	if account == 0 {
		account = b.Account
	}
	if account == 0 {
		return nil, ErrNoAccount
	}
	return binary.BigEndian.AppendUint64(fmt.Appendf(nil, "nfse-digital:%s:", b.Purpose), uint64(account)), nil
}

// Seal cifra o segredo; o nonce aleatório vai na frente do texto cifrado.
func (b Box) Seal(ctx context.Context, plain []byte) ([]byte, error) {
	aead, err := b.aead()
	if err != nil {
		return nil, err
	}
	data, err := b.associated(ctx)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := randomRead(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, data), nil
}

// Open decifra o que Seal selou para a mesma conta e propósito.
func (b Box) Open(ctx context.Context, sealed []byte) ([]byte, error) {
	aead, err := b.aead()
	if err != nil {
		return nil, err
	}
	data, err := b.associated(ctx)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, ErrTampered
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], data)
	if err != nil {
		return nil, ErrTampered
	}
	return plain, nil
}
