package sefin

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"

	"nfse/internal/adapters/dpsxml"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Canceller implementa ports.Canceller: assina o pedido de evento com o A1 e
// envia à SEFIN do ambiente da nota.
type Canceller struct {
	Certificates CertificateLoader
	// AllowProduction libera a SEFIN de produção, como no Emitter.
	AllowProduction bool
	// BaseURLOverride e Roots servem ao teste (servidor TLS falso).
	BaseURLOverride string
	Roots           *x509.CertPool
	// LastSentXML e LastRaw guardam o último pedido e a última resposta crua,
	// para a CLI mostrar quando a SEFIN recusar.
	LastSentXML []byte
	LastRaw     []byte
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Canceller = (*Canceller)(nil)

// Cancel leva o pedido à SEFIN. Produção exige AllowProduction, como na
// emissão: cancelar nota com valor fiscal é tão irreversível quanto emitir.
func (c *Canceller) Cancel(ctx context.Context, event domain.CancelEvent) (ports.CancelResult, error) {
	if event.Environment == domain.EnvProduction && !c.AllowProduction {
		return ports.CancelResult{}, ErrProductionBlocked
	}
	certificate, err := c.Certificates.Load(ctx)
	if err != nil {
		return ports.CancelResult{}, err
	}
	if certificate.Info.CNPJ != event.AuthorCNPJ {
		return ports.CancelResult{}, fmt.Errorf("o certificado é do CNPJ %s, mas o autor do cancelamento é %s", domain.FormatDocument(certificate.Info.CNPJ), domain.FormatDocument(event.AuthorCNPJ))
	}
	xml, packed, err := dpsxml.EncodeCancel(event, certificate.Signer, certificate.TLS.Certificate)
	if err != nil {
		return ports.CancelResult{}, err
	}
	c.LastSentXML = xml
	baseURL := BaseURL(event.Environment)
	if c.BaseURLOverride != "" {
		baseURL = c.BaseURLOverride
	}
	result, err := NewClient(baseURL, certificate.TLS, c.Roots).SubmitEvent(ctx, event.AccessKey, packed)
	c.LastRaw = result.Raw
	if err != nil {
		return ports.CancelResult{}, err
	}
	if result.Registered() {
		return ports.CancelResult{Registered: true, EventXML: result.EventXML}, nil
	}
	messages := []string{}
	for _, sefinErr := range result.Errors {
		messages = append(messages, strings.TrimSpace(fmt.Sprintf("[%s] %s %s", sefinErr.Code, sefinErr.Description, sefinErr.Complement)))
	}
	return ports.CancelResult{ErrorCode: result.Errors[0].Code, ErrorMessage: strings.Join(messages, " | ")}, nil
}
