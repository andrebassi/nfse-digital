package domain

import (
	"strings"
	"time"
)

// CertificateExpiryWarning é a antecedência do aviso de vencimento do A1:
// 30 dias dá tempo de comprar e importar o novo antes de parar de emitir.
const CertificateExpiryWarning = 30 * 24 * time.Hour

// CertificateInfo é o que a tela e a CLI mostram do certificado, sem a chave.
type CertificateInfo struct {
	HolderName string
	CNPJ       string
	IssuerName string
	NotBefore  time.Time
	NotAfter   time.Time
}

// Expired diz se o certificado já venceu em `now`.
func (c CertificateInfo) Expired(now time.Time) bool { return now.After(c.NotAfter) }

// ExpiresSoon diz se faltam menos de 30 dias para vencer (e ainda não venceu).
func (c CertificateInfo) ExpiresSoon(now time.Time) bool {
	return !c.Expired(now) && c.NotAfter.Sub(now) < CertificateExpiryWarning
}

// DaysLeft devolve os dias inteiros até o vencimento (negativo se já venceu).
func (c CertificateInfo) DaysLeft(now time.Time) int {
	return int(c.NotAfter.Sub(now).Hours() / 24)
}

// SplitICPCommonName separa o CN do e-CNPJ da ICP-Brasil, que vem como
// "RAZÃO SOCIAL:CNPJ". Sem os dois pontos, ou com um CNPJ inválido depois
// deles, devolve o nome inteiro e CNPJ vazio.
func SplitICPCommonName(commonName string) (name, cnpj string) {
	index := strings.LastIndex(commonName, ":")
	if index < 0 {
		return commonName, ""
	}
	document, err := ParseDocument(commonName[index+1:])
	if err != nil || document.Kind() != KindCNPJ {
		return commonName, ""
	}
	return commonName[:index], document.Digits()
}

// CertificateStatus é o que o painel sabe do certificado sem abrir o pass:
// os dados públicos da última leitura e a autorização de consulta ao portal.
type CertificateStatus struct {
	Info CertificateInfo
	// CheckedAt é quando o certificado foi lido pela última vez (zero = nunca).
	CheckedAt time.Time
	// PortalAuthorizedAt é quando o dono autorizou a consulta ao Portal
	// Nacional com o certificado (zero = não autorizada).
	PortalAuthorizedAt time.Time
}

// PortalAuthorized diz se a consulta ao Portal Nacional está autorizada.
func (s CertificateStatus) PortalAuthorized() bool {
	return !s.PortalAuthorizedAt.IsZero()
}
