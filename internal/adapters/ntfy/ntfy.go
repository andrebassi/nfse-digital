// Package ntfy manda o aviso do agendador para um tópico do ntfy, o mesmo
// canal que o dono já usa no celular. O e-mail ficou de fora: exigiria SMTP
// configurado só para isto.
package ntfy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Notifier publica no tópico da URL (ex.: https://ntfy.sh/<tópico>).
type Notifier struct {
	URL  string
	HTTP *http.Client
}

// New cria o notificador com prazo de 10 s: aviso que não sai em 10 s não
// pode segurar a rodada do agendador, que roda a cada minuto.
func New(url string) *Notifier {
	return &Notifier{URL: url, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Notify publica título e mensagem; resposta fora de 2xx é erro.
func (n *Notifier) Notify(ctx context.Context, title, message string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, strings.NewReader(message))
	if err != nil {
		return err
	}
	// O ntfy lê o título do cabeçalho; acento em cabeçalho HTTP só passa
	// codificado, e o ntfy aceita a forma RFC 2047.
	request.Header.Set("Title", "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(title))+"?=")
	request.Header.Set("Tags", "receipt")
	response, err := n.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("enviar aviso ao ntfy: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return fmt.Errorf("ntfy respondeu HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
