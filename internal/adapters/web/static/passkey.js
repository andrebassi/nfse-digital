// Código de página (roda no navegador): a única ponte possível com o WebAuthn,
// que só existe como API JavaScript (navigator.credentials). Tudo o que decide
// — desafio, conferência da assinatura, sessão — fica no servidor em Go.
"use strict";

// fromBase64URL converte o base64url do servidor nos bytes que o WebAuthn pede.
function fromBase64URL(text) {
  const base64 = text.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(text.length / 4) * 4, "=");
  return Uint8Array.from(atob(base64), (char) => char.charCodeAt(0)).buffer;
}

// toBase64URL converte os bytes do aparelho no base64url que o servidor lê.
function toBase64URL(buffer) {
  let binary = "";
  for (const byte of new Uint8Array(buffer)) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// post manda JSON com o token CSRF no cabeçalho e devolve a resposta lida.
async function post(url, csrf, body) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: body === undefined ? "{}" : JSON.stringify(body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || "o servidor recusou (" + response.status + ")");
  return data;
}

// credentialJSON serializa a resposta do aparelho no formato do WebAuthn.
function credentialJSON(credential) {
  const response = {};
  for (const field of ["clientDataJSON", "attestationObject", "authenticatorData", "signature", "userHandle"]) {
    if (credential.response[field]) response[field] = toBase64URL(credential.response[field]);
  }
  if (credential.response.getTransports) response.transports = credential.response.getTransports();
  return { id: credential.id, rawId: toBase64URL(credential.rawId), type: credential.type, response };
}

// register cadastra uma passkey nova para a conta logada.
async function register(csrf) {
  const options = (await post("/settings/security/passkeys/options", csrf)).publicKey;
  options.challenge = fromBase64URL(options.challenge);
  options.user.id = fromBase64URL(options.user.id);
  for (const item of options.excludeCredentials || []) item.id = fromBase64URL(item.id);
  const credential = await navigator.credentials.create({ publicKey: options });
  return post("/settings/security/passkeys", csrf, credentialJSON(credential));
}

// login entra com uma passkey já cadastrada neste aparelho.
async function login(csrf) {
  const options = (await post("/login/passkey/options", csrf)).publicKey;
  options.challenge = fromBase64URL(options.challenge);
  for (const item of options.allowCredentials || []) item.id = fromBase64URL(item.id);
  const credential = await navigator.credentials.get({ publicKey: options });
  return post("/login/passkey", csrf, credentialJSON(credential));
}

// Liga os botões com data-passkey="register" ou "login"; o erro aparece no
// elemento data-passkey-error da mesma página.
for (const button of document.querySelectorAll("[data-passkey]")) {
  button.disabled = !window.PublicKeyCredential;
  button.addEventListener("click", async () => {
    const error = document.querySelector("[data-passkey-error]");
    if (error) error.hidden = true;
    button.disabled = true;
    try {
      const action = button.dataset.passkey === "register" ? register : login;
      const result = await action(button.dataset.csrf);
      window.location.assign(result.redirect);
    } catch (problem) {
      if (error) {
        error.textContent = problem.name === "NotAllowedError" ? "Cancelado ou sem resposta do aparelho." : problem.message;
        error.hidden = false;
      }
      button.disabled = false;
    }
  });
}
