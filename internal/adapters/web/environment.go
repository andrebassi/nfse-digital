package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// shortMonths são os meses abreviados do relógio do cabeçalho.
var shortMonths = []string{"JAN.", "FEV.", "MAR.", "ABR.", "MAIO", "JUN.", "JUL.", "AGO.", "SET.", "OUT.", "NOV.", "DEZ."}

// headerClock escreve "30 DE OUT. · 01:30" no fuso de Brasília.
func headerClock(t time.Time) string {
	return fmt.Sprintf("%d DE %s · %s", t.Day(), shortMonths[t.Month()-1], t.Format("15:04"))
}

// environmentView alimenta a confirmação da troca para produção.
type environmentView struct {
	Issuer domain.Issuer
	Error  string
}

// environmentHomologation volta o emitente para homologação. Não pede
// confirmação: sair de produção nunca gera nota com valor fiscal.
func (s *Server) environmentHomologation(w http.ResponseWriter, r *http.Request) {
	issuer, err := s.repo.Issuer(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	issuer.Environment = domain.EnvHomologation
	if err := s.repo.SaveIssuer(r.Context(), issuer); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, safeBack(r.PostFormValue("back")), http.StatusSeeOther)
}

// environmentProductionForm pede o CNPJ redigitado antes de ligar produção
// (story 4.1): o seletor do cabeçalho é um clique, e um clique errado não
// pode virar nota com valor fiscal.
func (s *Server) environmentProductionForm(w http.ResponseWriter, r *http.Request) {
	issuer, err := s.repo.Issuer(r.Context())
	if errors.Is(err, ports.ErrNotFound) {
		http.Redirect(w, r, "/settings?flash="+url.QueryEscape("Configure o emitente antes de passar para produção"), http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.render(w, r, http.StatusOK, "environment", "Passar para produção", environmentView{Issuer: issuer})
}

// environmentProduction confere o CNPJ e liga produção.
func (s *Server) environmentProduction(w http.ResponseWriter, r *http.Request) {
	issuer, err := s.repo.Issuer(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if !domain.ConfirmsCNPJ(issuer.CNPJ, strings.TrimSpace(r.PostFormValue("confirm_cnpj"))) {
		s.render(w, r, http.StatusUnprocessableEntity, "environment", "Passar para produção",
			environmentView{Issuer: issuer, Error: "o CNPJ digitado não confere com o do emitente; o ambiente continua em homologação"})
		return
	}
	issuer.Environment = domain.EnvProduction
	if err := s.repo.SaveIssuer(r.Context(), issuer); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/invoices?flash="+url.QueryEscape("Ambiente em produção: as próximas notas têm valor fiscal"), http.StatusSeeOther)
}
