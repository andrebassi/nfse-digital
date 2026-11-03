package sefin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nfse/internal/domain"
)

// URLs base do Ambiente de Dados Nacional (ADN), onde ficam a distribuição de
// documentos e o DANFSe. Medidas na story 3.x, ver docs/sefin-notes.md.
const (
	HomologationADNURL = "https://adn.producaorestrita.nfse.gov.br"
	ProductionADNURL   = "https://adn.nfse.gov.br"
)

// ADNBaseURL devolve a URL do ADN do ambiente.
func ADNBaseURL(environment domain.Environment) string {
	if environment == domain.EnvProduction {
		return ProductionADNURL
	}
	return HomologationADNURL
}

// ReadResult é a resposta crua de uma leitura: status, tipo e corpo.
type ReadResult struct {
	StatusCode  int
	ContentType string
	Location    string
	Body        []byte
}

// Reader faz só GET, com o A1 como certificado de cliente. Existe separado
// do Client para não haver como uma consulta virar envio por engano.
type Reader struct {
	HTTP *http.Client
}

// NewReader monta o leitor mTLS (mesma configuração TLS do Client). Não segue
// redirecionamento: medido em 30/10/2026, rota errada redireciona para a
// documentação em http sem TLS; seguir esconde a resposta real.
func NewReader(certificate tls.Certificate, roots *x509.CertPool) *Reader {
	client := NewClient("", certificate, roots).HTTP
	tlsConfig := client.Transport.(*http.Transport).TLSClientConfig
	tlsConfig.MaxVersion = tls.VersionTLS12
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Reader{HTTP: client}
}

// Get lê a URL. Só aceita https: o certificado de cliente não vai em texto puro.
func (r *Reader) Get(ctx context.Context, url string) (ReadResult, error) {
	if !strings.HasPrefix(url, "https://") {
		return ReadResult{}, fmt.Errorf("só https é aceito: %s", url)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ReadResult{}, err
	}
	request.Header.Set("Accept", "application/json, application/pdf, */*")
	response, err := r.HTTP.Do(request)
	if err != nil {
		return ReadResult{}, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 20<<20))
	return ReadResult{StatusCode: response.StatusCode, ContentType: response.Header.Get("Content-Type"), Location: response.Header.Get("Location"), Body: body}, nil
}
