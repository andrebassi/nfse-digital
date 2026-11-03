package passcert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeInsert troca o `pass insert` por um mapa; failOn faz a entrada falhar.
func fakeInsert(t *testing.T, failOn string) map[string]string {
	t.Helper()
	written := map[string]string{}
	original := runPassInsert
	t.Cleanup(func() { runPassInsert = original })
	runPassInsert = func(_ context.Context, entry string, content []byte) error {
		if entry == failOn {
			return ErrPassFailed
		}
		written[entry] = string(content)
		return nil
	}
	return written
}

// TestReplaceWritesWhatLoadReads: o certificado gravado pelo Replace é aberto
// pelo Load com o mesmo resultado — os dois lados falam o mesmo formato.
func TestReplaceWritesWhatLoadReads(t *testing.T) {
	pfx := makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(1, 0, 0), true)
	written := fakeInsert(t, "")
	store := Store{Prefix: "teste/nfse", Now: func() time.Time { return now }}
	info, err := store.Replace(context.Background(), pfx, testPassword, "11222333000181")
	if err != nil || info.CNPJ != "11222333000181" || info.HolderName != "EXEMPLO TECNOLOGIA LTDA" {
		t.Fatalf("Replace: %+v, %v", info, err)
	}
	fakePass(t, written)
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.Info != info {
		t.Fatalf("Load do que o Replace gravou: %+v, %v", loaded.Info, err)
	}
}

// TestReplaceRefusesBeforeWriting: senha errada, vencido e sem CNPJ não gravam
// nada; falha do pass em cada entrada aparece; a senha nunca vai no erro.
func TestReplaceRefusesBeforeWriting(t *testing.T) {
	store := Store{Prefix: "teste/nfse", Now: func() time.Time { return now }}
	good := makePFX(t, "EXEMPLO TECNOLOGIA LTDA:11222333000181", now.AddDate(1, 0, 0), false)
	cases := map[string]struct {
		pfx      []byte
		password string
		want     error
	}{
		"senha errada": {good, "outra-senha", ErrWrongPassword},
		"vencido":      {makePFX(t, "EXEMPLO:11222333000181", now.AddDate(0, 0, -1), false), testPassword, ErrExpired},
		"sem CNPJ":     {makePFX(t, "FULANO DE TAL", now.AddDate(1, 0, 0), false), testPassword, ErrNoCNPJ},
		"não é pfx":    {[]byte("texto qualquer"), testPassword, ErrWrongPassword},
	}
	for name, c := range cases {
		written := fakeInsert(t, "")
		_, err := store.Replace(context.Background(), c.pfx, c.password, "")
		if !errors.Is(err, c.want) || len(written) != 0 {
			t.Errorf("%s: %v, gravou %d entrada(s)", name, err, len(written))
		}
		if err != nil && strings.Contains(err.Error(), c.password) {
			t.Errorf("%s: a senha apareceu no erro", name)
		}
	}
	written := fakeInsert(t, "")
	if _, err := store.Replace(context.Background(), good, testPassword, "56789012000100"); !errors.Is(err, ErrOtherCNPJ) || len(written) != 0 {
		t.Errorf("outro CNPJ: %v, gravou %d", err, len(written))
	}
	for _, entry := range []string{"teste/nfse/cert-a1-pfx", "teste/nfse/cert-a1-password"} {
		fakeInsert(t, entry)
		if _, err := store.Replace(context.Background(), good, testPassword, ""); !errors.Is(err, ErrPassFailed) {
			t.Errorf("falha em %s: %v", entry, err)
		}
	}
}

// TestRunPassInsertUsesStdin: o conteúdo vai pelo stdin do pass (nunca por
// argumento) e a falha do pass vira ErrPassFailed com a mensagem dele.
func TestRunPassInsertUsesStdin(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o700)
	out := filepath.Join(home, "gravado")
	script := "#!/bin/sh\nif [ \"$4\" = falha ]; then echo 'gpg: sem chave' >&2; exit 2; fi\ncat > " + out + "\n"
	if err := os.WriteFile(filepath.Join(bin, "pass"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	if err := runPassInsert(context.Background(), "ok", []byte("conteúdo secreto\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(out); string(data) != "conteúdo secreto\n" {
		t.Fatalf("stdin: %q", data)
	}
	if err := runPassInsert(context.Background(), "falha", nil); !errors.Is(err, ErrPassFailed) || !strings.Contains(err.Error(), "sem chave") {
		t.Fatalf("falha: %v", err)
	}
}
