package sefin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// EventField é o campo JSON que leva o pedido de evento em gzip+base64 no
// POST /nfse/{chave}/eventos.
const EventField = "pedidoRegistroEventoXmlGZipB64"

// EventResult é a resposta do registro de evento já interpretada.
type EventResult struct {
	StatusCode int
	// EventXML é o XML do evento registrado, quando a SEFIN o devolve.
	EventXML []byte
	Errors   []SefinError
	Raw      []byte
}

// Registered diz se a SEFIN aceitou o evento: HTTP 2xx sem lista de erro.
func (r EventResult) Registered() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300 && len(r.Errors) == 0
}

// SubmitEvent envia o pedido de evento (já assinado, gzip+base64) para a nota
// da chave. Falha de rede e 5xx voltam como ErrTransient: o chamador decide
// se repete, porque registrar o mesmo cancelamento duas vezes a SEFIN recusa.
func (c *Client) SubmitEvent(ctx context.Context, accessKey, packed string) (EventResult, error) {
	body, _ := json.Marshal(map[string]string{EventField: packed})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/nfse/"+accessKey+"/eventos", bytes.NewReader(body))
	if err != nil {
		return EventResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return EventResult{}, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	result := EventResult{StatusCode: response.StatusCode, Raw: raw}
	if response.StatusCode >= 500 {
		return result, fmt.Errorf("%w: HTTP %d", ErrTransient, response.StatusCode)
	}
	result.Errors = parseErrors(raw)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var payload map[string]any
		if json.Unmarshal(raw, &payload) == nil {
			if encoded := stringField(payload, "eventoXmlGZipB64"); encoded != "" {
				if xml, err := unpack(encoded); err == nil {
					result.EventXML = xml
				}
			}
		}
		return result, nil
	}
	if len(result.Errors) == 0 {
		result.Errors = []SefinError{{Code: fmt.Sprintf("HTTP%d", response.StatusCode), Description: strings.TrimSpace(string(raw))}}
	}
	return result, nil
}
