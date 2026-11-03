package secretbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nfse/internal/ports"
)

// testKey é uma chave fixa de 32 bytes para os testes.
var testKey = bytes.Repeat([]byte{7}, keySize)

// TestSealOpen: o que sela abre na mesma conta; outra conta, outro
// propósito, blob alterado ou curto não abrem; sem conta e com chave errada
// nem selam.
func TestSealOpen(t *testing.T) {
	box := Box{Key: testKey, Purpose: "gmail"}
	alice := ports.WithAccount(context.Background(), 1)
	sealed, err := box.Seal(alice, []byte("abcdefghijklmnop"))
	if err != nil || bytes.Contains(sealed, []byte("abcdefghijklmnop")) {
		t.Fatalf("selar: %v (texto em claro no blob?)", err)
	}
	if plain, err := box.Open(alice, sealed); err != nil || string(plain) != "abcdefghijklmnop" {
		t.Fatalf("abrir: %q, %v", plain, err)
	}
	bruno := ports.WithAccount(context.Background(), 2)
	other := Box{Key: testKey, Purpose: "outro"}
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	for name, try := range map[string]func() error{
		"outra conta":     func() error { _, err := box.Open(bruno, sealed); return err },
		"outro propósito": func() error { _, err := other.Open(alice, sealed); return err },
		"adulterado":      func() error { _, err := box.Open(alice, tampered); return err },
		"curto":           func() error { _, err := box.Open(alice, []byte{1}); return err },
	} {
		if err := try(); !errors.Is(err, ErrTampered) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := box.Seal(context.Background(), []byte("x")); !errors.Is(err, ErrNoAccount) {
		t.Errorf("selar sem conta: %v", err)
	}
	if _, err := box.Open(context.Background(), sealed); !errors.Is(err, ErrNoAccount) {
		t.Errorf("abrir sem conta: %v", err)
	}
	// Sem conta no context, vale a conta fixa (agendador): abre o da conta 1.
	if plain, err := (Box{Key: testKey, Purpose: "gmail", Account: 1}).Open(context.Background(), sealed); err != nil || string(plain) != "abcdefghijklmnop" {
		t.Errorf("conta fixa: %q %v", plain, err)
	}
	short := Box{Key: []byte("curta")}
	if _, err := short.Seal(alice, nil); !errors.Is(err, ErrBadKey) {
		t.Errorf("selar com chave curta: %v", err)
	}
	if _, err := short.Open(alice, sealed); !errors.Is(err, ErrBadKey) {
		t.Errorf("abrir com chave curta: %v", err)
	}
}

// TestRandomFailure: sem aleatoriedade do sistema, nem chave nova nem selo
// saem — nada é gravado com chave ou nonce previsível.
func TestRandomFailure(t *testing.T) {
	randomRead = func([]byte) (int, error) { return 0, errors.New("sem entropia") }
	defer func() { randomRead = rand.Read }()
	path := filepath.Join(t.TempDir(), "secret.key")
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("gerou chave sem entropia")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("arquivo de chave gravado mesmo com a falha")
	}
	box := Box{Key: testKey, Purpose: "gmail"}
	if _, err := box.Seal(ports.WithAccount(context.Background(), 1), []byte("x")); err == nil {
		t.Fatal("selou sem entropia")
	}
}

// TestParseKey: 32 bytes em base64 (com espaço em volta) passam; o resto é ErrBadKey.
func TestParseKey(t *testing.T) {
	if key, err := ParseKey(" " + base64.StdEncoding.EncodeToString(testKey) + "\n"); err != nil || !bytes.Equal(key, testKey) {
		t.Fatalf("chave boa: %v", err)
	}
	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString([]byte("curta"))} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrBadKey) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestLoadOrCreateKey: a primeira chamada gera o arquivo com permissão 600
// (criando o diretório); a segunda lê a mesma chave; arquivo corrompido e
// caminho ilegível são erro, nunca uma chave nova por cima.
func TestLoadOrCreateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secret.key")
	first, err := LoadOrCreateKey(path)
	if err != nil || len(first) != keySize {
		t.Fatalf("gerar: %v", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("permissão %v", info.Mode().Perm())
	}
	if second, err := LoadOrCreateKey(path); err != nil || !bytes.Equal(first, second) {
		t.Fatalf("reler: %v", err)
	}
	broken := filepath.Join(t.TempDir(), "broken.key")
	_ = os.WriteFile(broken, []byte("lixo"), 0o600)
	if _, err := LoadOrCreateKey(broken); !errors.Is(err, ErrBadKey) {
		t.Fatalf("corrompida: %v", err)
	}
	if _, err := LoadOrCreateKey(t.TempDir()); err == nil {
		t.Fatal("diretório no lugar do arquivo devia falhar")
	}
	blocked := filepath.Join(t.TempDir(), "arquivo")
	_ = os.WriteFile(blocked, nil, 0o600)
	if _, err := LoadOrCreateKey(filepath.Join(blocked, "secret.key")); err == nil {
		t.Fatal("diretório pai que é arquivo devia falhar")
	}
}
