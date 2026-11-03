package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/app"
	"nfse/internal/domain"
)

// authFixture é o painel sem conta e sem sessão, como no primeiro acesso.
type authFixture struct {
	server  *Server
	handler http.Handler
	now     *time.Time
}

// newAuthFixture monta o painel do zero, com relógio que o teste avança.
func newAuthFixture(t *testing.T) authFixture {
	t.Helper()
	now := time.Date(2026, 10, 29, 22, 52, 0, 0, brt)
	service := &app.Service{Repo: memory.NewSeeded(brt), Emitter: simulated.Emitter{}, Location: brt, Now: func() time.Time { return now }}
	server, err := New(service, testPort)
	if err != nil {
		t.Fatal(err)
	}
	return authFixture{server: server, handler: server.Handler(), now: &now}
}

// do faz a requisição com o Host certo; POST leva o token CSRF.
func (f authFixture) do(method, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var request *http.Request
	if method == http.MethodPost {
		form.Set("csrf", f.server.csrf)
		request = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		request = httptest.NewRequest(method, target, nil)
	}
	request.Host = "127.0.0.1:8735"
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// sessionFrom pega o cookie de sessão de uma resposta.
func sessionFrom(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("resposta sem cookie de sessão")
	return nil
}

// TestFirstAccessGoesToSignUp: sem conta, qualquer tela e o /login levam ao cadastro.
func TestFirstAccessGoesToSignUp(t *testing.T) {
	f := newAuthFixture(t)
	for _, target := range []string{"/invoices", "/login", "/settings"} {
		if response := f.do(http.MethodGet, target, nil); response.Header().Get("Location") != "/signup" {
			t.Errorf("%s → %s", target, response.Header().Get("Location"))
		}
	}
	if response := f.do(http.MethodGet, "/signup", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Crie sua conta") {
		t.Errorf("cadastro: %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/static/app.css", nil); response.Code != http.StatusOK {
		t.Errorf("CSS deveria ser público: %d", response.Code)
	}
}

// TestSignUpLogoutLogin percorre o fluxo inteiro e confere que o cookie
// antigo não vale depois de sair.
func TestSignUpLogoutLogin(t *testing.T) {
	f := newAuthFixture(t)
	short := f.do(http.MethodPost, "/signup", url.Values{"name": {"Dona"}, "email": {"dona@exemplo.example"}, "password": {"curta"}})
	if short.Code != http.StatusUnprocessableEntity || !strings.Contains(short.Body.String(), "12 caracteres") {
		t.Fatalf("senha curta: %d", short.Code)
	}

	created := f.do(http.MethodPost, "/signup", url.Values{"name": {"Dona Exemplo"}, "email": {"dona@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/invoices" {
		t.Fatalf("criar: %d %s", created.Code, created.Header().Get("Location"))
	}
	session := sessionFrom(t, created)
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie sem proteção: %+v", session)
	}
	if body := f.do(http.MethodGet, "/invoices", nil, session).Body.String(); !strings.Contains(body, "Dona Exemplo") || !strings.Contains(body, "dona@exemplo.example") {
		t.Fatal("barra lateral sem o dono")
	}

	// Várias contas: o cadastro continua aberto e cria outra conta (outra
	// empresa), já com sessão dela; e-mail que já tem conta vai para entrar.
	if again := f.do(http.MethodGet, "/signup", nil); again.Code != http.StatusOK {
		t.Fatalf("cadastro com conta existente: %d", again.Code)
	}
	second := f.do(http.MethodPost, "/signup", url.Values{"name": {"Outra Pessoa"}, "email": {"outra@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	if second.Code != http.StatusSeeOther || second.Header().Get("Location") != "/invoices" {
		t.Fatalf("segunda conta: %d %s", second.Code, second.Header().Get("Location"))
	}
	if body := f.do(http.MethodGet, "/invoices", nil, sessionFrom(t, second)).Body.String(); !strings.Contains(body, "outra@exemplo.example") || strings.Contains(body, "Dona Exemplo") {
		t.Fatal("sessão da segunda conta mostra a conta errada")
	}
	duplicate := f.do(http.MethodPost, "/signup", url.Values{"name": {"X"}, "email": {"DONA@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	if !strings.HasPrefix(duplicate.Header().Get("Location"), "/login?notice=") {
		t.Fatalf("e-mail repetido: %s", duplicate.Header().Get("Location"))
	}
	if body := f.do(http.MethodGet, duplicate.Header().Get("Location"), nil).Body.String(); !strings.Contains(body, "já existe uma conta com este e-mail") || !strings.Contains(body, "Entrar com passkey") {
		t.Fatal("tela de entrar sem aviso ou sem o botão de passkey")
	}

	out := f.do(http.MethodPost, "/logout", url.Values{}, session)
	if out.Header().Get("Location") != "/login" {
		t.Fatalf("sair: %s", out.Header().Get("Location"))
	}
	if response := f.do(http.MethodGet, "/invoices", nil, session); response.Header().Get("Location") != "/login" {
		t.Fatal("cookie antigo ainda vale depois de sair")
	}

	wrong := f.do(http.MethodPost, "/login", url.Values{"email": {"dona@exemplo.example"}, "password": {"errada-errada"}})
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), "e-mail ou senha incorretos") {
		t.Fatalf("senha errada: %d", wrong.Code)
	}
	right := f.do(http.MethodPost, "/login", url.Values{"email": {"Dona@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	if right.Code != http.StatusSeeOther || right.Header().Get("Location") != "/invoices" {
		t.Fatalf("entrar: %d", right.Code)
	}
	sessionFrom(t, right)
}

// TestSessionExpires: depois de 12 horas a sessão deixa de valer.
func TestSessionExpires(t *testing.T) {
	f := newAuthFixture(t)
	created := f.do(http.MethodPost, "/signup", url.Values{"name": {"Dona"}, "email": {"dona@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	session := sessionFrom(t, created)
	*f.now = f.now.Add(sessionTTL + time.Minute)
	if response := f.do(http.MethodGet, "/invoices", nil, session); response.Header().Get("Location") != "/login" {
		t.Fatalf("sessão vencida ainda vale: %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/logout", url.Values{}); response.Header().Get("Location") != "/login" {
		t.Fatal("sair sem cookie deveria só redirecionar")
	}
}

// TestLoginLockout: na sexta tentativa a tela mostra o bloqueio.
func TestLoginLockout(t *testing.T) {
	f := newAuthFixture(t)
	f.do(http.MethodPost, "/signup", url.Values{"name": {"Dona"}, "email": {"dona@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	for range 5 {
		f.do(http.MethodPost, "/login", url.Values{"email": {"dona@exemplo.example"}, "password": {"errada-errada"}})
	}
	locked := f.do(http.MethodPost, "/login", url.Values{"email": {"dona@exemplo.example"}, "password": {"senha-de-teste-longa"}})
	if locked.Code != http.StatusUnauthorized || !strings.Contains(locked.Body.String(), "aguarde 5 minutos") {
		t.Fatalf("bloqueio: %d", locked.Code)
	}
}

// brokenAccountRepo falha em toda leitura e gravação de conta.
type brokenAccountRepo struct{ *memory.Repository }

// Accounts sempre falha.
func (brokenAccountRepo) Accounts(context.Context) ([]domain.Account, error) {
	return nil, errors.New("disco")
}

// AccountByEmail sempre falha.
func (brokenAccountRepo) AccountByEmail(context.Context, string) (domain.Account, error) {
	return domain.Account{}, errors.New("disco")
}

// CreateAccount sempre falha.
func (brokenAccountRepo) CreateAccount(context.Context, *domain.Account) error {
	return errors.New("disco")
}

// TestAuthRepositoryFailures: sem conseguir listar contas, o painel cai na
// tela de entrar (nunca no cadastro por engano); entrar e criar conta com o
// disco falhando viram 500.
func TestAuthRepositoryFailures(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 52, 0, 0, brt)
	service := &app.Service{Repo: brokenAccountRepo{memory.NewSeeded(brt)}, Emitter: simulated.Emitter{}, Location: brt, Now: func() time.Time { return now }}
	server, _ := New(service, testPort)
	f := authFixture{server: server, handler: server.Handler(), now: &now}
	if response := f.do(http.MethodGet, "/invoices", nil); response.Header().Get("Location") != "/login" {
		t.Errorf("middleware: %s", response.Header().Get("Location"))
	}
	for _, target := range []string{"/login", "/signup"} {
		if response := f.do(http.MethodGet, target, nil); response.Code != http.StatusOK {
			t.Errorf("GET %s = %d", target, response.Code)
		}
	}
	for _, target := range []string{"/login", "/signup"} {
		form := url.Values{"name": {"Dona"}, "email": {"dona@exemplo.example"}, "password": {"senha-de-teste-longa"}}
		if response := f.do(http.MethodPost, target, form); response.Code != http.StatusInternalServerError {
			t.Errorf("POST %s = %d", target, response.Code)
		}
	}
}

// TestLoginLinksToSignup: a tela de entrar oferece o link para criar conta.
func TestLoginLinksToSignup(t *testing.T) {
	f := newFixture(t, nil, nil)
	body := f.get("/login").Body.String()
	if !strings.Contains(body, `Não tenho conta — <a href="/signup"`) || !strings.Contains(body, ">criar uma</a>") {
		t.Fatal("login sem o link para criar conta")
	}
}
