package ntfy

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNotify: título codificado com acento, corpo da mensagem, recusa do
// servidor e servidor fora do ar.
func TestNotify(t *testing.T) {
	var title, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title = r.Header.Get("Title")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		if strings.Contains(body, "recusa") {
			http.Error(w, "limite de envio", http.StatusTooManyRequests)
		}
	}))
	defer server.Close()
	notifier := New(server.URL + "/topico")
	ctx := context.Background()

	if err := notifier.Notify(ctx, "NFS-e amanhã", "valor R$ 1,00"); err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(title, "=?UTF-8?B?"), "?=")
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	if string(decoded) != "NFS-e amanhã" || body != "valor R$ 1,00" {
		t.Fatalf("título %q, corpo %q", decoded, body)
	}
	if err := notifier.Notify(ctx, "t", "recusa"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("recusa: %v", err)
	}
	if err := New("http://127.0.0.1:1/x").Notify(ctx, "t", "m"); err == nil {
		t.Fatal("fora do ar deveria falhar")
	}
	if err := New("://").Notify(ctx, "t", "m"); err == nil {
		t.Fatal("URL inválida deveria falhar")
	}
}
