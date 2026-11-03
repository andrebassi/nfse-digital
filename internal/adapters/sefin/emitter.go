package sefin

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"nfse/internal/adapters/dpsxml"
	"nfse/internal/adapters/passcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// ErrProductionBlocked é o segundo cadeado da produção: mesmo que a guarda do
// app (app.EnsureEnvironment) falhe, o adapter só fala com a SEFIN de
// produção quando quem o montou ligou AllowProduction — e a CLI só liga junto
// com --production.
var ErrProductionBlocked = errors.New("emissão em produção bloqueada: o emissor não foi montado com permissão de produção")

// CertificateLoader abre o A1; é o passcert.Store em produção e um fake no teste.
type CertificateLoader interface {
	Load(ctx context.Context) (passcert.Certificate, error)
}

// Emitter implementa ports.Emitter de verdade: monta a DPS, assina com o A1
// e envia à SEFIN do ambiente da nota.
type Emitter struct {
	Certificates CertificateLoader
	AppVersion   string
	// AllowProduction libera a SEFIN de produção (nota com valor fiscal).
	AllowProduction bool
	// BaseURLOverride e Roots servem ao teste (servidor TLS falso).
	BaseURLOverride string
	Roots           *x509.CertPool
	// LastSentXML guarda o último XML enviado, para a CLI mostrar no smoke test.
	LastSentXML []byte
	// LastRaw guarda o último corpo cru recebido da SEFIN.
	LastRaw []byte
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Emitter = (*Emitter)(nil)

// Emit leva a nota até a SEFIN. A chave do certificado é aberta a cada
// emissão (aprovação no celular ou YubiKey) e não fica guardada no processo.
func (e *Emitter) Emit(ctx context.Context, request ports.EmitRequest) (ports.EmitResult, error) {
	if request.Invoice.Environment == domain.EnvProduction && !e.AllowProduction {
		return ports.EmitResult{}, ErrProductionBlocked
	}
	dps, err := domain.BuildDPS(request.Issuer, request.Customer, request.Template, request.Invoice, e.AppVersion)
	if err != nil {
		return ports.EmitResult{}, err
	}
	certificate, err := e.Certificates.Load(ctx)
	if err != nil {
		return ports.EmitResult{}, err
	}
	// CNPJ do certificado diferente do emitente seria recusado pela SEFIN;
	// recusar aqui poupa a ida e explica o motivo.
	if certificate.Info.CNPJ != request.Issuer.CNPJ {
		return ports.EmitResult{}, fmt.Errorf("o certificado é do CNPJ %s, mas o emitente é %s", domain.FormatDocument(certificate.Info.CNPJ), domain.FormatDocument(request.Issuer.CNPJ))
	}
	xml, packed, err := dpsxml.Encode(dps, certificate.Signer, certificate.TLS.Certificate)
	if err != nil {
		return ports.EmitResult{}, err
	}
	e.LastSentXML = xml

	baseURL := BaseURL(request.Invoice.Environment)
	if e.BaseURLOverride != "" {
		baseURL = e.BaseURLOverride
	}
	result, err := NewClient(baseURL, certificate.TLS, e.Roots).Submit(ctx, packed)
	e.LastRaw = result.Raw
	switch {
	case errors.Is(err, ErrDuplicate):
		// A DPS já existe lá: a nota fica pendente até a consulta (story 3.4).
		return ports.EmitResult{}, err
	case err != nil:
		return ports.EmitResult{}, err
	case result.AccessKey != "":
		return ports.EmitResult{Status: domain.StatusAuthorized, AccessKey: result.AccessKey, NFSeNumber: result.NFSeNumber, XML: result.NFSeXML}, nil
	default:
		first := result.Errors[0]
		messages := []string{}
		for _, sefinErr := range result.Errors {
			messages = append(messages, strings.TrimSpace(fmt.Sprintf("[%s] %s %s", sefinErr.Code, sefinErr.Description, sefinErr.Complement)))
		}
		return ports.EmitResult{Status: domain.StatusRejected, ErrorCode: first.Code, ErrorMessage: strings.Join(messages, " | ")}, nil
	}
}
