package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// ceremonyCookie liga o navegador ao desafio WebAuthn em andamento. O desafio
// fica no servidor; o cookie só carrega um identificador aleatório.
const ceremonyCookie = "webauthn_ceremony"

// ceremonyTTL é quanto um desafio vale: dá tempo do Touch ID e impede reusar
// um desafio esquecido numa aba aberta.
const ceremonyTTL = 5 * time.Minute

// passkeyRP é a parte do WebAuthn ligada ao painel. RPID é "localhost": o
// padrão não aceita endereço IP, então passkey só funciona em
// http://localhost:<porta> (o navegador trata localhost como contexto seguro).
type passkeyRP struct {
	webauthn *webauthn.WebAuthn
	mu       sync.Mutex
	pending  map[string]pendingCeremony
}

// pendingCeremony é um desafio emitido e ainda não respondido.
type pendingCeremony struct {
	session webauthn.SessionData
	expires time.Time
}

// newPasskeyRP configura o WebAuthn para o domínio e a origem do painel:
// localhost:<porta> na máquina, o host público em HTTPS no Fly. Passkey
// cadastrada num domínio não serve no outro — é o WebAuthn amarrando a
// credencial ao site, contra phishing.
func newPasskeyRP(rpID, origin string) (*passkeyRP, error) {
	instance, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "nfse-digital",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, err
	}
	return &passkeyRP{webauthn: instance, pending: map[string]pendingCeremony{}}, nil
}

// owner é o usuário WebAuthn: a conta única do painel com as passkeys dela.
type owner struct {
	account     domain.Account
	credentials []webauthn.Credential
}

// WebAuthnID é o identificador opaco do dono: hash do e-mail, para não pôr o
// e-mail em claro dentro do aparelho e ser estável entre cadastros.
func (o owner) WebAuthnID() []byte {
	sum := sha256.Sum256([]byte("nfse-digital:" + o.account.Email))
	return sum[:]
}

// WebAuthnName é o e-mail, que o aparelho mostra na lista de passkeys.
func (o owner) WebAuthnName() string { return o.account.Email }

// WebAuthnDisplayName é o nome da conta.
func (o owner) WebAuthnDisplayName() string { return o.account.Name }

// WebAuthnCredentials são as passkeys já cadastradas.
func (o owner) WebAuthnCredentials() []webauthn.Credential { return o.credentials }

// loadOwner monta o dono com as passkeys gravadas, guardando o vínculo entre
// a credencial e a linha do banco para atualizar o contador depois.
func (s *Server) loadOwner(ctx context.Context) (owner, map[string]domain.Passkey, error) {
	account, err := s.repo.Account(ctx)
	if err != nil {
		return owner{}, nil, err
	}
	stored, err := s.repo.Passkeys(ctx)
	if err != nil {
		return owner{}, nil, err
	}
	user := owner{account: account}
	byID := map[string]domain.Passkey{}
	for _, passkey := range stored {
		var credential webauthn.Credential
		if json.Unmarshal(passkey.Data, &credential) != nil {
			continue
		}
		user.credentials = append(user.credentials, credential)
		byID[string(credential.ID)] = passkey
	}
	return user, byID, nil
}

// rememberCeremony guarda o desafio e entrega o cookie que aponta para ele.
func (s *Server) rememberCeremony(w http.ResponseWriter, session *webauthn.SessionData) {
	id := s.passkeys.remember(session, s.now())
	s.setCookie(w, &http.Cookie{Name: ceremonyCookie, Value: id, Path: "/", MaxAge: int(ceremonyTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// remember guarda o desafio e devolve o identificador aleatório dele.
func (p *passkeyRP) remember(session *webauthn.SessionData, now time.Time) string {
	id := rand.Text()
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, ceremony := range p.pending {
		if now.After(ceremony.expires) {
			delete(p.pending, key)
		}
	}
	p.pending[id] = pendingCeremony{session: *session, expires: now.Add(ceremonyTTL)}
	return id
}

// take devolve e apaga o desafio do cookie: cada desafio vale uma vez só.
func (p *passkeyRP) take(r *http.Request, now time.Time) (webauthn.SessionData, bool) {
	cookie, err := r.Cookie(ceremonyCookie)
	if err != nil {
		return webauthn.SessionData{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ceremony, ok := p.pending[cookie.Value]
	delete(p.pending, cookie.Value)
	if !ok || now.After(ceremony.expires) {
		return webauthn.SessionData{}, false
	}
	return ceremony.session, true
}

// writeJSON responde JSON com o status dado.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// passkeyError responde o erro em JSON para o passkey.js mostrar na tela.
func passkeyError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// passkeyRegisterOptions começa o cadastro: devolve o desafio para o
// navegador pedir o Touch ID. Passkey já cadastrada entra na exclusão, para o
// aparelho não criar duas para a mesma conta.
func (s *Server) passkeyRegisterOptions(w http.ResponseWriter, r *http.Request) {
	user, _, err := s.loadOwner(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(user.credentials))
	for _, credential := range user.credentials {
		exclude = append(exclude, credential.Descriptor())
	}
	creation, session, err := s.passkeys.webauthn.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired), webauthn.WithExclusions(exclude))
	if err != nil {
		passkeyError(w, http.StatusBadRequest, "não consegui começar o cadastro: "+err.Error())
		return
	}
	s.rememberCeremony(w, session)
	writeJSON(w, http.StatusOK, creation)
}

// passkeyRegister confere a resposta do aparelho e grava a passkey.
func (s *Server) passkeyRegister(w http.ResponseWriter, r *http.Request) {
	session, ok := s.passkeys.take(r, s.now())
	if !ok {
		passkeyError(w, http.StatusBadRequest, "o desafio expirou: tente de novo")
		return
	}
	user, _, err := s.loadOwner(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	credential, err := s.passkeys.webauthn.FinishRegistration(user, session, r)
	if err != nil {
		passkeyError(w, http.StatusBadRequest, "o aparelho não confirmou a passkey: "+err.Error())
		return
	}
	data, _ := json.Marshal(credential)
	passkey := domain.Passkey{CredentialID: credential.ID, Data: data, CreatedAt: s.now()}
	if err := s.repo.SavePasskey(r.Context(), &passkey); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": "/settings/security?flash=Passkey+cadastrada"})
}

// passkeyLoginOptions começa o login sem e-mail: o aparelho oferece as
// passkeys que tem para localhost.
func (s *Server) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	assertion, session, err := s.passkeys.webauthn.BeginDiscoverableLogin()
	if err != nil {
		passkeyError(w, http.StatusBadRequest, "não consegui começar o login: "+err.Error())
		return
	}
	s.rememberCeremony(w, session)
	writeJSON(w, http.StatusOK, assertion)
}

// passkeyLogin confere a assinatura do aparelho, atualiza o contador da
// passkey (o que detecta clone) e abre a sessão.
func (s *Server) passkeyLogin(w http.ResponseWriter, r *http.Request) {
	session, ok := s.passkeys.take(r, s.now())
	if !ok {
		passkeyError(w, http.StatusBadRequest, "o desafio expirou: tente de novo")
		return
	}
	// O aparelho devolve o identificador da conta (userHandle): acha a conta
	// dona dele entre todas e confere a assinatura contra as passkeys dela.
	var found owner
	var byID map[string]domain.Passkey
	credential, err := s.passkeys.webauthn.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		accounts, err := s.repo.Accounts(r.Context())
		if err != nil {
			return nil, err
		}
		for _, account := range accounts {
			if bytes.Equal(owner{account: account}.WebAuthnID(), userHandle) {
				found, byID, err = s.loadOwner(ports.WithAccount(r.Context(), account.ID))
				return found, err
			}
		}
		return nil, errors.New("passkey de conta inexistente")
	}, session, r)
	if err != nil {
		passkeyError(w, http.StatusUnauthorized, "passkey não reconhecida")
		return
	}
	ctx := ports.WithAccount(r.Context(), found.account.ID)
	if stored, ok := byID[string(credential.ID)]; ok {
		stored.Data, _ = json.Marshal(credential)
		stored.LastUsedAt = s.now()
		_ = s.repo.SavePasskey(ctx, &stored)
	}
	s.startSession(w, found.account.ID)
	writeJSON(w, http.StatusOK, map[string]string{"redirect": "/invoices"})
}

// passkeysRemove apaga todas as passkeys; entrar volta a ser só por senha.
func (s *Server) passkeysRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.DeletePasskeys(r.Context()); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/settings/security?flash=Passkeys+removidas", http.StatusSeeOther)
}
