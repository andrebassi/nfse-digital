package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// CertificateOps grava o certificado A1 da conta. Replace confere e grava
// (recusando outro CNPJ quando expectedCNPJ vem preenchido); Storage é a
// frase que a tela mostra sobre onde ele fica (pass na máquina do dono,
// cofre cifrado no banco no Fly).
type CertificateOps struct {
	Replace func(ctx context.Context, pfx []byte, password, expectedCNPJ string) (domain.CertificateInfo, error)
	Storage string
}

// needsCertificate diz se a conta da sessão ainda não enviou o certificado.
// Sem envio de certificado ligado (UseCertificate), nada trava.
func (s *Server) needsCertificate(ctx context.Context) bool {
	if s.certificate == nil || ports.AccountID(ctx) == 0 {
		return false
	}
	status, err := s.repo.CertificateStatus(ctx)
	return err == nil && status.Info.CNPJ == ""
}

// certificateFreePath são as rotas que abrem antes do certificado: a aba
// dele, o envio, sair, os arquivos estáticos e as telas de entrada.
func certificateFreePath(path string) bool {
	return path == "/settings/certificate" || path == "/logout" || publicPath(path)
}

// requireCertificate manda para a aba Certificado quem ainda não o enviou.
// Sem ele nada funciona (emitir, importar do portal, sincronizar), então o
// primeiro acesso começa por ali. A trava é aqui, na rota: o menu
// desabilitado da tela é só o reflexo dela.
func (s *Server) requireCertificate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !certificateFreePath(r.URL.Path) && s.needsCertificate(r.Context()) {
			http.Redirect(w, r, "/settings/certificate", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UseCertificate liga o bloco de certificado das Configurações.
func (s *Server) UseCertificate(ops CertificateOps) {
	s.certificate = &ops
}

// maxPFXBytes limita o .pfx: um A1 real tem poucos KB; 64 KB folga muito e
// impede usar o formulário para empurrar arquivo grande para o pass.
const maxPFXBytes = 64 << 10

// fillSettings completa a tela com o estado do certificado e renderiza.
func (s *Server) fillSettings(w http.ResponseWriter, r *http.Request, status int, view settingsView) {
	certificate, err := s.repo.CertificateStatus(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	view.Certificate, view.CertificateEnabled = certificate, s.certificate != nil
	if s.certificate != nil {
		view.CertificateStorage = s.certificate.Storage
	}
	if view.Section == "" {
		view.Section = "issuer"
	}
	locked := s.needsCertificate(r.Context())
	for _, tab := range settingsSections {
		tab.URL, tab.Active = settingsURL(tab.Key), tab.Key == view.Section
		tab.Disabled = locked && tab.Key != "certificate"
		view.Sections = append(view.Sections, tab)
	}
	if view.Section == "security" {
		if view.Passkeys, err = s.repo.Passkeys(r.Context()); err != nil {
			fail(w, err)
			return
		}
		if host, port, found := strings.Cut(r.Host, ":"); found && host != "localhost" {
			view.PasskeyLocalURL = "http://localhost:" + port + "/settings/security"
		}
	}
	s.render(w, r, status, "settings", "Configurações", view)
}

// certificateReplace recebe o .pfx e a senha, confere e grava. Em
// qualquer recusa nada é gravado, e a mensagem nunca repete a senha.
func (s *Server) certificateReplace(w http.ResponseWriter, r *http.Request) {
	if s.certificate == nil {
		http.NotFound(w, r)
		return
	}
	issuer, err := s.repo.Issuer(r.Context())
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		fail(w, err)
		return
	}
	view := settingsView{Issuer: issuer, Section: "certificate"}
	refuse := func(message string) {
		view.CertificateError = message
		s.fillSettings(w, r, http.StatusUnprocessableEntity, view)
	}
	file, _, err := r.FormFile("pfx")
	if err != nil {
		refuse("escolha o arquivo do certificado A1 (.pfx)")
		return
	}
	defer file.Close()
	pfx, _ := io.ReadAll(io.LimitReader(file, maxPFXBytes+1))
	password := r.PostFormValue("password")
	switch {
	case len(pfx) > maxPFXBytes:
		refuse("o arquivo é grande demais para um certificado A1")
		return
	case password == "":
		refuse("informe a senha do certificado")
		return
	}
	first := s.needsCertificate(r.Context())
	info, err := s.certificate.Replace(r.Context(), pfx, password, issuer.CNPJ)
	if err != nil {
		refuse(err.Error())
		return
	}
	if err := s.saveCertificateInfo(r.Context(), info); err != nil {
		fail(w, err)
		return
	}
	// Enviar ou substituir o certificado dispara a importação do portal
	// (pedido do dono, 30/10/2026) — idempotente, então reimportar não
	// duplica nada. O primeiro envio vale como a autorização de LEITURA da
	// base nacional; depois, quem revogou a consulta em Certificado continua
	// revogado: substituir o certificado não reautoriza sozinho.
	if s.portal != nil {
		if first {
			if err := s.authorizePortal(r.Context()); err != nil {
				fail(w, err)
				return
			}
		}
		if allowed, err := s.portalAllowed(r.Context()); err == nil && allowed {
			http.Redirect(w, r, "/portal", http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, "/settings/certificate?flash="+url.QueryEscape("Certificado de "+info.HolderName+" gravado"), http.StatusSeeOther)
}

// authorizePortal registra agora a autorização de leitura do portal.
func (s *Server) authorizePortal(ctx context.Context) error {
	status, err := s.repo.CertificateStatus(ctx)
	if err != nil {
		return err
	}
	status.PortalAuthorizedAt = s.now()
	return s.repo.SaveCertificateStatus(ctx, status)
}

// saveCertificateInfo grava os dados públicos mantendo a autorização do portal.
func (s *Server) saveCertificateInfo(ctx context.Context, info domain.CertificateInfo) error {
	status, err := s.repo.CertificateStatus(ctx)
	if err != nil {
		return err
	}
	status.Info, status.CheckedAt = info, s.now()
	return s.repo.SaveCertificateStatus(ctx, status)
}

// portalAuthorization autoriza ou revoga o uso do certificado para LER a base
// nacional (importar do portal e sincronizar).
func (s *Server) portalAuthorization(w http.ResponseWriter, r *http.Request) {
	status, err := s.repo.CertificateStatus(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	message := "Consulta ao Portal Nacional revogada"
	status.PortalAuthorizedAt = domain.CertificateStatus{}.PortalAuthorizedAt
	if strings.TrimSpace(r.PostFormValue("action")) == "authorize" {
		status.PortalAuthorizedAt, message = s.now(), "Consulta ao Portal Nacional autorizada"
	}
	if err := s.repo.SaveCertificateStatus(r.Context(), status); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/settings/certificate?flash="+url.QueryEscape(message), http.StatusSeeOther)
}

// portalAllowed diz se a leitura da base nacional está autorizada.
func (s *Server) portalAllowed(ctx context.Context) (bool, error) {
	status, err := s.repo.CertificateStatus(ctx)
	return status.PortalAuthorized(), err
}
