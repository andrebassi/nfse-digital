// Package sefin fala com a API da SEFIN Nacional por mTLS, usando o
// certificado A1 do emitente como certificado de cliente.
package sefin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nfse/internal/domain"
)

// URLs base da API. A homologação ("produção restrita") não gera nota com
// valor fiscal; a produção gera.
const (
	HomologationURL = "https://sefin.producaorestrita.nfse.gov.br/SefinNacional"
	ProductionURL   = "https://sefin.nfse.gov.br/SefinNacional"
)

// requestTimeout limita cada chamada: a SEFIN costuma responder em segundos,
// e um minuto cobre lentidão sem prender o painel indefinidamente.
const requestTimeout = 60 * time.Second

var (
	// ErrDuplicate é o 409: a DPS com este número já foi registrada.
	ErrDuplicate = errors.New("a SEFIN já tem uma DPS com este número")
	// ErrTransient é falha de rede, timeout ou 5xx: não dá para saber se a
	// nota saiu, então ela fica pendente e é consultada antes de reenviar.
	ErrTransient = errors.New("SEFIN indisponível ou sem resposta")
)

// Client chama a API de um ambiente.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// BaseURL devolve a URL do ambiente.
func BaseURL(environment domain.Environment) string {
	if environment == domain.EnvProduction {
		return ProductionURL
	}
	return HomologationURL
}

// NewClient monta o cliente com o A1 como certificado de cliente. roots nil
// usa as ACs do sistema; o teste passa a AC do servidor falso.
func NewClient(baseURL string, certificate tls.Certificate, roots *x509.CertPool) *Client {
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{certificate},
		// Envia o A1 mesmo quando a lista de ACs do servidor não casa com a cadeia
		// (o .pfx do dono traz só o certificado final): sem isto o Go manda
		// certificado vazio e o ADN derruba o handshake (medido em 30/10/2026).
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &certificate, nil },
		RootCAs:              roots,
		MinVersion:           tls.VersionTLS12,
		// O servidor da SEFIN só pede o certificado de cliente depois do
		// handshake, por renegociação TLS 1.2. Sem isto o Go recusa com
		// "tls: no renegotiation" (medido na homologação em 29/10/2026).
		// Uma vez basta: renegociação livre abriria espaço a abuso do servidor.
		Renegotiation: tls.RenegotiateOnceAsClient,
	}}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Transport: transport, Timeout: requestTimeout}}
}

// SefinError é um erro de validação devolvido pela SEFIN.
type SefinError struct {
	Code        string
	Description string
	Complement  string
}

// SubmitResult é a resposta interpretada do POST /nfse, com o corpo cru
// guardado para conferência (o formato oficial tem Swaggers divergentes).
type SubmitResult struct {
	StatusCode int
	AccessKey  string
	NFSeNumber string
	NFSeXML    []byte
	Errors     []SefinError
	Raw        []byte
}

// Submit envia a DPS (já assinada, gzip+base64) e interpreta a resposta.
func (c *Client) Submit(ctx context.Context, dpsXMLGZipB64 string) (SubmitResult, error) {
	body, _ := json.Marshal(map[string]string{"dpsXmlGZipB64": dpsXMLGZipB64})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/nfse", bytes.NewReader(body))
	if err != nil {
		return SubmitResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	result := SubmitResult{StatusCode: response.StatusCode, Raw: raw}

	switch {
	case response.StatusCode >= 500:
		return result, fmt.Errorf("%w: HTTP %d", ErrTransient, response.StatusCode)
	case response.StatusCode == http.StatusConflict:
		result.Errors = parseErrors(raw)
		return result, ErrDuplicate
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return result, parseSuccess(raw, &result)
	default:
		result.Errors = parseErrors(raw)
		if len(result.Errors) == 0 {
			result.Errors = []SefinError{{Code: fmt.Sprintf("HTTP%d", response.StatusCode), Description: strings.TrimSpace(string(raw))}}
		}
		return result, nil
	}
}

// parseSuccess lê chave de acesso e XML da NFS-e da resposta 2xx.
func parseSuccess(raw []byte, result *SubmitResult) error {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("resposta de sucesso ilegível: %w", err)
	}
	result.AccessKey = stringField(payload, "chaveAcesso")
	if encoded := stringField(payload, "nfseXmlGZipB64"); encoded != "" {
		xml, err := unpack(encoded)
		if err != nil {
			return fmt.Errorf("XML da NFS-e ilegível: %w", err)
		}
		result.NFSeXML = xml
		result.NFSeNumber = between(string(xml), "<nNFSe>", "</nNFSe>")
	}
	if result.AccessKey == "" {
		return errors.New("resposta de sucesso sem chave de acesso")
	}
	return nil
}

// parseErrors aceita as variações relatadas da lista de erros: "erros" ou
// "Erros", itens com "Codigo"/"codigo" e "Descricao"/"descricao"; também um
// objeto único no topo.
func parseErrors(raw []byte) []SefinError {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	var items []any
	for _, key := range []string{"erros", "Erros", "erro", "Erro"} {
		switch value := payload[key].(type) {
		case []any:
			items = value
		case map[string]any:
			items = []any{value}
		}
		if items != nil {
			break
		}
	}
	if items == nil && stringField(payload, "codigo") != "" {
		items = []any{payload}
	}
	result := []SefinError{}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, SefinError{
			Code:        stringField(fields, "codigo"),
			Description: stringField(fields, "descricao"),
			Complement:  stringField(fields, "complemento"),
		})
	}
	return result
}

// stringField lê uma chave sem diferenciar maiúscula na primeira letra.
func stringField(fields map[string]any, key string) string {
	for _, candidate := range []string{key, strings.ToUpper(key[:1]) + key[1:]} {
		if value, ok := fields[candidate].(string); ok {
			return value
		}
	}
	return ""
}

// unpack desfaz o base64 e o gzip.
func unpack(encoded string) ([]byte, error) {
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

// between devolve o texto entre duas marcas, ou vazio.
func between(text, start, end string) string {
	_, after, found := strings.Cut(text, start)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(after, end)
	return value
}
