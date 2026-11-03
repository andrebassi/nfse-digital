package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// mcpMaxBody limita o corpo de uma chamada MCP: pedidos JSON-RPC são pequenos,
// e o limite impede um token vazado de encher a memória do painel.
const mcpMaxBody = 2 << 20

// UseMCP liga o servidor MCP por HTTP em /mcp, autenticado por token.
func (s *Server) UseMCP(handler http.Handler) {
	s.mcpHandler = handler
}

// mcpGate protege /mcp: Host local (contra DNS rebinding, como o resto do
// painel) e token de API no cabeçalho Authorization. Sessão e CSRF não se
// aplicam — quem chama é o agente, não o navegador.
func (s *Server) mcpGate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !s.hosts[r.Host] {
		http.Error(w, "host não permitido", http.StatusForbidden)
		return
	}
	token, status, message := s.bearer(r)
	if status != 0 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="nfse-digital"`)
		http.Error(w, message, status)
		return
	}
	// Sessão MCP aberta por uma conta só serve à mesma conta: sem isto, o
	// token de outra conta reaproveitaria a sessão (e os dados) da primeira.
	if session := r.Header.Get("Mcp-Session-Id"); session != "" {
		if owner, known := s.mcpSessions.Load(session); known && owner.(int64) != token.AccountID {
			http.Error(w, "sessão MCP de outra conta", http.StatusForbidden)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpMaxBody)
	recorder := &sessionRecorder{ResponseWriter: w}
	s.mcpHandler.ServeHTTP(recorder, r.WithContext(ports.WithAccount(r.Context(), token.AccountID)))
	if session := recorder.Header().Get("Mcp-Session-Id"); session != "" {
		s.mcpSessions.LoadOrStore(session, token.AccountID)
	}
}

// bearer confere o token de API do cabeçalho Authorization, regra única do
// MCP e da API REST. status zero é token válido; senão, 401 (ausente,
// inválido ou revogado, sem dizer qual) ou 500 (o banco falhou ao conferir).
func (s *Server) bearer(r *http.Request) (domain.APIToken, int, string) {
	plain, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token, err := s.service.VerifyToken(r.Context(), strings.TrimSpace(plain))
	switch {
	case found && err != nil && !errors.Is(err, app.ErrInvalidToken):
		return token, http.StatusInternalServerError, "erro ao conferir o token"
	case !found || err != nil:
		return token, http.StatusUnauthorized, app.ErrInvalidToken.Error()
	}
	return token, 0, ""
}

// sessionRecorder deixa o portão ler o Mcp-Session-Id que o servidor MCP
// devolve no initialize, para amarrar a sessão à conta do token.
type sessionRecorder struct {
	http.ResponseWriter
}

// Flush repassa o flush: o MCP por HTTP responde em streaming.
func (s *sessionRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// tokenGenerate gera um token e mostra a tela com ele preenchido nos passos.
// Responde a página direto (sem redirect) para o token não ir parar na URL
// nem no histórico do navegador.
func (s *Server) tokenGenerate(w http.ResponseWriter, r *http.Request) {
	plain, _, err := s.service.IssueToken(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	page := formPage(r)
	page.Token = plain
	s.renderIntegrations(w, r, page)
}

// tokenRevoke apaga o token: ele para de abrir o MCP na hora.
func (s *Server) tokenRevoke(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.repo.DeleteAPIToken(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/integrations/tokens?flash=Token+revogado", http.StatusSeeOther)
}
