package web

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"nfse/internal/domain"
)

// softAuthenticator faz o papel do Touch ID no teste: guarda uma chave P-256
// e responde aos desafios com a mesma estrutura de um aparelho real. Assim o
// servidor confere assinatura, origem e contador de verdade.
type softAuthenticator struct {
	key        *ecdsa.PrivateKey
	credential []byte
	userHandle []byte
	counter    uint32
}

// b64 é o base64url sem preenchimento do WebAuthn.
var b64 = base64.RawURLEncoding

// newSoftAuthenticator cria a chave e um ID de credencial aleatório.
func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: key, credential: id}
}

// clientData monta o clientDataJSON com tipo, desafio e a origem do painel.
func clientData(kind, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": "http://localhost:8735", "crossOrigin": false})
	return data
}

// authData monta o authenticatorData: hash do RP ID, flags e contador; com
// attested, acrescenta a chave pública em COSE (registro).
func (a *softAuthenticator) authData(attested bool) []byte {
	rpHash := sha256.Sum256([]byte("localhost"))
	flags := byte(0x01 | 0x04) // usuário presente e verificado
	if attested {
		flags |= 0x40
	}
	a.counter++
	data := append(rpHash[:], flags)
	data = binary.BigEndian.AppendUint32(data, a.counter)
	if attested {
		data = append(data, make([]byte, 16)...) // AAGUID zerado
		data = binary.BigEndian.AppendUint16(data, uint16(len(a.credential)))
		data = append(data, a.credential...)
		x, y := a.key.PublicKey.X.FillBytes(make([]byte, 32)), a.key.PublicKey.Y.FillBytes(make([]byte, 32))
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		data = append(data, cose...)
	}
	return data
}

// register responde ao desafio de cadastro (atestado "none").
func (a *softAuthenticator) register(challenge string, userHandle []byte) map[string]any {
	a.userHandle = userHandle
	attestation, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(true)})
	return map[string]any{
		"id": b64.EncodeToString(a.credential), "rawId": b64.EncodeToString(a.credential), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(clientData("webauthn.create", challenge)), "attestationObject": b64.EncodeToString(attestation)},
	}
}

// login assina authenticatorData + hash do clientDataJSON, como o aparelho faz.
func (a *softAuthenticator) login(challenge string) map[string]any {
	client := clientData("webauthn.get", challenge)
	data := a.authData(false)
	hash := sha256.Sum256(client)
	digest := sha256.Sum256(append(append([]byte{}, data...), hash[:]...))
	signature, _ := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	return map[string]any{
		"id": b64.EncodeToString(a.credential), "rawId": b64.EncodeToString(a.credential), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON": b64.EncodeToString(client), "authenticatorData": b64.EncodeToString(data),
			"signature": b64.EncodeToString(signature), "userHandle": b64.EncodeToString(a.userHandle),
		},
	}
}

// jsonPost manda JSON com o CSRF no cabeçalho, pelo localhost, com cookies.
func (f fixture) jsonPost(target string, body any, session bool, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(raw))
	request.Host = "localhost:8735"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", f.server.csrf)
	if session {
		request.AddCookie(f.session)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// optionsOf lê o desafio e o cookie da cerimônia de uma resposta de opções.
func optionsOf(t *testing.T, response *httptest.ResponseRecorder) (map[string]any, *http.Cookie) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("opções: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		PublicKey map[string]any `json:"publicKey"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &payload)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == ceremonyCookie {
			return payload.PublicKey, cookie
		}
	}
	t.Fatal("sem cookie da cerimônia")
	return nil, nil
}

// TestPasskeyRegisterAndLogin: cadastra uma passkey com o autenticador de
// software, entra com ela sem senha, o contador sobe, o desafio não serve
// duas vezes, e remover apaga tudo.
func TestPasskeyRegisterAndLogin(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	device := newSoftAuthenticator(t)

	options, ceremony := optionsOf(t, f.jsonPost("/settings/security/passkeys/options", nil, true))
	user := options["user"].(map[string]any)
	handle, _ := b64.DecodeString(user["id"].(string))
	if user["name"] != "dona@exemplo.example" || options["authenticatorSelection"].(map[string]any)["residentKey"] != "required" {
		t.Fatalf("opções de cadastro: %+v", options)
	}
	registered := f.jsonPost("/settings/security/passkeys", device.register(options["challenge"].(string), handle), true, ceremony)
	if registered.Code != http.StatusOK || !strings.Contains(registered.Body.String(), "/settings/security") {
		t.Fatalf("cadastro: %d %s", registered.Code, registered.Body.String())
	}
	if again := f.jsonPost("/settings/security/passkeys", device.register(options["challenge"].(string), handle), true, ceremony); again.Code != http.StatusBadRequest {
		t.Fatalf("desafio reutilizado: %d", again.Code)
	}
	stored, _ := f.repo.Passkeys(ctx)
	if len(stored) != 1 || !bytes.Equal(stored[0].CredentialID, device.credential) {
		t.Fatalf("passkey gravada: %+v", stored)
	}
	page := f.get("/settings/security").Body.String()
	if !strings.Contains(page, "1 passkey(s) cadastrada(s)") || !strings.Contains(page, "Adicionar outra passkey") || !strings.Contains(page, "nunca usada") {
		t.Fatal("aba Segurança não mostra a passkey")
	}

	// Segundo cadastro: a passkey existente vai na lista de exclusão.
	second, _ := optionsOf(t, f.jsonPost("/settings/security/passkeys/options", nil, true))
	if excluded, _ := second["excludeCredentials"].([]any); len(excluded) != 1 {
		t.Fatalf("exclusão: %+v", second["excludeCredentials"])
	}

	// Login sem sessão nenhuma: só a passkey.
	loginOptions, loginCeremony := optionsOf(t, f.jsonPost("/login/passkey/options", nil, false))
	logged := f.jsonPost("/login/passkey", device.login(loginOptions["challenge"].(string)), false, loginCeremony)
	if logged.Code != http.StatusOK || !strings.Contains(logged.Body.String(), "/invoices") {
		t.Fatalf("login: %d %s", logged.Code, logged.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range logged.Result().Cookies() {
		if cookie.Name == sessionCookie {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("login sem cookie de sessão")
	}
	if after, _ := f.repo.Passkeys(ctx); after[0].LastUsedAt.IsZero() {
		t.Fatal("último uso não registrado")
	}

	// Assinatura de outra chave (aparelho clonado ou falso) não entra.
	impostor := newSoftAuthenticator(t)
	impostor.credential, impostor.userHandle = device.credential, handle
	badOptions, badCeremony := optionsOf(t, f.jsonPost("/login/passkey/options", nil, false))
	if response := f.jsonPost("/login/passkey", impostor.login(badOptions["challenge"].(string)), false, badCeremony); response.Code != http.StatusUnauthorized {
		t.Fatalf("assinatura falsa: %d", response.Code)
	}

	if response := f.post("/settings/security/passkeys/remove", url.Values{}); response.Code != http.StatusSeeOther {
		t.Fatalf("remover: %d", response.Code)
	}
	if after, _ := f.repo.Passkeys(ctx); len(after) != 0 {
		t.Fatal("passkeys não foram removidas")
	}
}

// TestPasskeyRefusals: sem desafio, corpo inválido, conta de outro dono e
// sem conta nenhuma são recusados; a aba avisa quando aberta por IP.
func TestPasskeyRefusals(t *testing.T) {
	f := newFixture(t, nil, nil)
	if response := f.jsonPost("/settings/security/passkeys", map[string]any{}, true); response.Code != http.StatusBadRequest {
		t.Fatalf("cadastro sem desafio: %d", response.Code)
	}
	if response := f.jsonPost("/login/passkey", map[string]any{}, false); response.Code != http.StatusBadRequest {
		t.Fatalf("login sem desafio: %d", response.Code)
	}
	_, ceremony := optionsOf(t, f.jsonPost("/settings/security/passkeys/options", nil, true))
	if response := f.jsonPost("/settings/security/passkeys", map[string]any{"id": "x"}, true, ceremony); response.Code != http.StatusBadRequest {
		t.Fatalf("corpo inválido: %d", response.Code)
	}
	loginOptions, loginCeremony := optionsOf(t, f.jsonPost("/login/passkey/options", nil, false))
	stranger := newSoftAuthenticator(t)
	stranger.userHandle = []byte("outra-conta")
	if response := f.jsonPost("/login/passkey", stranger.login(loginOptions["challenge"].(string)), false, loginCeremony); response.Code != http.StatusUnauthorized {
		t.Fatalf("passkey de outra conta: %d", response.Code)
	}
	if body := f.get("/settings/security").Body.String(); !strings.Contains(body, "http://localhost:8735/settings/security") || !strings.Contains(body, "Nenhuma passkey ainda") {
		t.Fatal("aba aberta por IP deveria apontar para localhost")
	}
	// Credencial corrompida no banco é ignorada, não derruba a tela.
	_ = f.repo.SavePasskey(context.Background(), &domain.Passkey{CredentialID: []byte("x"), Data: []byte("{")})
	if response := f.jsonPost("/settings/security/passkeys/options", nil, true); response.Code != http.StatusOK {
		t.Fatalf("credencial corrompida: %d", response.Code)
	}
}
