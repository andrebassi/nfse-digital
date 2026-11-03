package web

import (
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// sessionCookie é o nome do cookie de sessão.
const sessionCookie = "session"

// sessionTTL é a duração de uma sessão: um dia de trabalho, para não pedir
// senha a toda hora nem deixar o painel aberto de um dia para o outro.
const sessionTTL = 12 * time.Hour

// sessionStore guarda as sessões abertas na memória do processo: reiniciar
// desloga, o que é aceitável para um painel local.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

// session é uma sessão aberta: de qual conta e até quando.
type session struct {
	account int64
	expires time.Time
}

// newSessionStore cria o armazenamento vazio.
func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]session{}}
}

// create abre uma sessão da conta com token aleatório (crypto/rand).
func (s *sessionStore) create(now time.Time, accountID int64) string {
	token := rand.Text()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session{account: accountID, expires: now.Add(sessionTTL)}
	return token
}

// removeAccount apaga todas as sessões da conta (todos os navegadores em que
// ela estava logada): depois de excluída, nenhum cookie antigo vale.
func (s *sessionStore) removeAccount(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, current := range s.sessions {
		if current.account == accountID {
			delete(s.sessions, token)
		}
	}
}

// lookup devolve a conta da sessão; sessão vencida é apagada na hora.
func (s *sessionStore) lookup(token string, now time.Time) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[token]
	if ok && now.After(current.expires) {
		delete(s.sessions, token)
		return 0, false
	}
	return current.account, ok
}

// remove apaga a sessão no servidor, para o cookie antigo não valer mais.
func (s *sessionStore) remove(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// publicPath diz quais caminhos abrem sem sessão: entrar (senha ou passkey),
// criar conta e os arquivos estáticos.
func publicPath(path string) bool {
	return path == "/login" || path == "/signup" || path == "/login/passkey" || path == "/login/passkey/options" || strings.HasPrefix(path, "/static/")
}

// requireSession põe a conta da sessão no context de toda requisição — é ela
// que separa os dados de uma empresa dos da outra — e manda para /login (ou
// /signup, se não há conta nenhuma) quem chega sem sessão a tela protegida.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accountID, ok := s.sessionAccount(r); ok {
			next.ServeHTTP(w, r.WithContext(ports.WithAccount(r.Context(), accountID)))
			return
		}
		if publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, s.entryPage(r), http.StatusSeeOther)
	})
}

// entryPage é /signup enquanto não há conta nenhuma, e /login depois.
func (s *Server) entryPage(r *http.Request) string {
	if accounts, err := s.repo.Accounts(r.Context()); err == nil && len(accounts) == 0 {
		return "/signup"
	}
	return "/login"
}

// sessionAccount lê o cookie e devolve a conta da sessão.
func (s *Server) sessionAccount(r *http.Request) (int64, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return 0, false
	}
	return s.sessions.lookup(cookie.Value, s.now())
}

// startSession abre a sessão da conta e grava o cookie protegido.
func (s *Server) startSession(w http.ResponseWriter, accountID int64) {
	s.setCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.sessions.create(s.now(), accountID), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

// authView são os dados das telas de entrar e criar conta.
type authView struct {
	Name   string
	Email  string
	Notice string
	Error  string
	Errors domain.FieldErrors
}

// clientIP é o endereço de quem tenta entrar, para o bloqueio de login. Só
// no painel público, atrás do proxy do Fly, que grava o IP real em
// Fly-Client-IP (e sobrescreve o que o cliente mandar). Local devolve "":
// ali o header seria forjável e há um usuário só, então o contador é um.
func (s *Server) clientIP(r *http.Request) string {
	if !s.httpsOnly {
		return ""
	}
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginForm mostra a tela de entrar; sem conta nenhuma, manda para o cadastro.
func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.entryPage(r) == "/signup" {
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "login", "Entrar", authView{Notice: r.URL.Query().Get("notice")})
}

// loginSubmit confere e-mail e senha e abre a sessão.
func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	ctx := ports.WithClient(r.Context(), s.clientIP(r))
	account, err := s.service.Authenticate(ctx, email, r.PostFormValue("password"))
	switch {
	case errors.Is(err, app.ErrInvalidCredentials), errors.Is(err, app.ErrLocked):
		s.render(w, r, http.StatusUnauthorized, "login", "Entrar", authView{Email: email, Error: err.Error()})
	case err != nil:
		fail(w, err)
	default:
		s.startSession(w, account.ID)
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
	}
}

// signUpForm mostra o cadastro de uma conta nova (uma empresa nova).
func (s *Server) signUpForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "signup", "Criar conta", authView{})
}

// signUpSubmit cria a conta e já entra nela.
func (s *Server) signUpSubmit(w http.ResponseWriter, r *http.Request) {
	view := authView{Name: r.PostFormValue("name"), Email: r.PostFormValue("email")}
	account, err := s.service.SignUp(r.Context(), view.Name, view.Email, r.PostFormValue("password"))
	var fields domain.FieldErrors
	switch {
	case errors.As(err, &fields):
		view.Errors = fields
		s.render(w, r, http.StatusUnprocessableEntity, "signup", "Criar conta", view)
	case errors.Is(err, app.ErrAccountExists):
		http.Redirect(w, r, "/login?notice="+url.QueryEscape(err.Error()), http.StatusSeeOther)
	case errors.Is(err, app.ErrSignUpClosed):
		view.Error = err.Error()
		s.render(w, r, http.StatusForbidden, "signup", "Criar conta", view)
	case err != nil:
		fail(w, err)
	default:
		s.startSession(w, account.ID)
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
	}
}

// logout apaga a sessão no servidor e o cookie no navegador.
// accountDelete exclui a conta da sessão e todos os dados dela, derruba as
// sessões da conta em qualquer navegador e volta para a entrada. Sem o
// e-mail confirmado, volta para Segurança com o motivo.
func (s *Server) accountDelete(w http.ResponseWriter, r *http.Request) {
	accountID := ports.AccountID(r.Context())
	err := s.service.DeleteAccount(r.Context(), r.PostFormValue("confirm_email"))
	if errors.Is(err, app.ErrDeleteNotConfirmed) {
		http.Redirect(w, r, "/settings/security?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.sessions.removeAccount(accountID)
	s.setCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, s.entryPage(r)+"?notice="+url.QueryEscape("Conta excluída, com todos os dados dela."), http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.remove(cookie.Value)
	}
	s.setCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
