// Package nfse é o SDK Go da API REST do nfse-digital: emitir, listar,
// consultar e cancelar NFS-e pelo Emissor Nacional (SEFIN).
//
// client.gen.go é GERADO do openapi.json pelo oapi-codegen (`task
// sdk:generate`) e não se edita à mão. Este arquivo só acrescenta o atalho
// NewWithToken.
//
//	client, _ := nfse.NewWithToken("https://nfse.bassi.engineer/api/v1", os.Getenv("NFSE_TOKEN"))
//	res, _ := client.EmitInvoiceWithResponse(ctx, nfse.EmitRequest{
//		IdempotencyKey: "fatura-2026-11-0042", CustomerId: 1, TemplateId: 1,
//	})
package nfse

import (
	"context"
	"net/http"
)

// NewWithToken monta o cliente com o token de API (gerado no painel, em
// Integrações → API REST) no cabeçalho Authorization de toda chamada.
// baseURL é a URL base da API, terminada em /api/v1.
func NewWithToken(baseURL, token string, options ...ClientOption) (*ClientWithResponses, error) {
	bearer := func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	return NewClientWithResponses(baseURL, append(options, WithRequestEditorFn(bearer))...)
}
