package turso

import (
	"errors"
	"testing"
	"time"
)

// TestIsRemote: URL do Turso vai para o adapter remoto; caminho de arquivo,
// inclusive relativo e com "file:", continua no SQLite local.
func TestIsRemote(t *testing.T) {
	cases := map[string]bool{
		"libsql://nfse-exemplo.turso.io": true,
		"https://nfse-exemplo.turso.io":  true,
		"http://127.0.0.1:8080":          true,
		"/data/nfse.db":                  false,
		"nfse.db":                        false,
		"file:/tmp/nfse.db":              false,
		"":                               false,
	}
	for dsn, want := range cases {
		if got := IsRemote(dsn); got != want {
			t.Errorf("%q: %v, quer %v", dsn, got, want)
		}
	}
}

// TestOpenWithoutToken: sem token, recusa na hora com o nome do secret, em
// vez de um 401 genérico na primeira consulta.
func TestOpenWithoutToken(t *testing.T) {
	if _, err := Open("libsql://nfse-exemplo.turso.io", ""); !errors.Is(err, ErrNoToken) {
		t.Fatalf("sem token: %v", err)
	}
}

// TestOpenUnreachable: host que não existe devolve erro rápido (DNS), nunca
// pânico nem espera infinita — é o caminho de um secret errado no deploy.
func TestOpenUnreachable(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := Open("https://host-que-nao-existe.invalid", "token-de-teste")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("host inalcançável deveria dar erro")
		}
	case <-time.After(connectTimeout + 5*time.Second):
		t.Fatal("Open travou além do prazo de conexão")
	}
}
