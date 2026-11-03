package nfse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewWithToken: toda chamada leva o Bearer; a resposta 201 da emissão
// volta tipada em JSON201, e a recusa 422 em JSON422 com os campos.
func TestNewWithToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer nf_teste" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body EmitRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.IdempotencyKey == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"dados inválidos","fields":{"idempotency_key":"obrigatória"}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"status":"authorized","status_label":"autorizada","environment":"homologation","dps_series":"900","dps_number":1,"amount":"1500.00","customer_id":1,"template_id":1,"description":"x","idempotency_key":"` + body.IdempotencyKey + `"}`))
	}))
	defer server.Close()

	client, err := NewWithToken(server.URL, "nf_teste")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := client.EmitInvoiceWithResponse(ctx, EmitRequest{IdempotencyKey: "fatura-1", CustomerId: 1, TemplateId: 1})
	if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil || created.JSON201.Id != 42 || *created.JSON201.IdempotencyKey != "fatura-1" || created.JSON201.Status != Authorized {
		t.Fatalf("emissão: %v %+v", err, created)
	}
	refused, _ := client.EmitInvoiceWithResponse(ctx, EmitRequest{CustomerId: 1, TemplateId: 1})
	if refused.JSON422 == nil || (*refused.JSON422.Fields)["idempotency_key"] == "" {
		t.Fatalf("recusa: %+v", refused)
	}
	other, _ := NewWithToken(server.URL, "nf_errado")
	if res, _ := other.GetAccountWithResponse(ctx); res.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("token errado: %d", res.StatusCode())
	}
}
