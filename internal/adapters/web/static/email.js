// Código de página (roda no navegador, não no Node): aba "Envio por e-mail"
// do tomador. Duas coisas só: (1) os botões de marcador inserem {numero} etc.
// no assunto ou na mensagem, onde estava o cursor; (2) a prévia acompanha o
// que se digita, pedindo o cartão pronto ao servidor — os marcadores são
// resolvidos lá, pela mesma regra do envio de verdade. Sem JavaScript, a
// prévia atualiza ao salvar ou testar.
(() => {
  const form = document.querySelector("form[data-email-form]");
  if (!form) {
    return;
  }
  // previewDelay é a pausa, em ms, depois da última tecla antes de pedir a
  // prévia: uma chamada por pausa, não uma por letra.
  const previewDelay = 300;

  // O último campo de texto focado recebe o marcador; o padrão é a mensagem.
  const targets = [...form.querySelectorAll("[data-placeholder-target]")];
  let target = form.elements.body;
  for (const field of targets) {
    field.addEventListener("focus", () => {
      target = field;
    });
  }
  for (const button of form.querySelectorAll("button[data-placeholder]")) {
    button.addEventListener("click", () => {
      const text = button.dataset.placeholder;
      const start = target.selectionStart ?? target.value.length;
      const end = target.selectionEnd ?? target.value.length;
      target.value = target.value.slice(0, start) + text + target.value.slice(end);
      target.focus();
      target.setSelectionRange(start + text.length, start + text.length);
      schedule();
    });
  }

  let timer = null;
  // schedule pede a prévia depois da pausa de digitação.
  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, previewDelay);
  }

  // refresh manda o formulário (sem a senha) e troca o cartão da prévia.
  async function refresh() {
    const card = document.querySelector("[data-email-preview]");
    if (!card) {
      return;
    }
    const data = new FormData(form);
    data.delete("sender_password");
    try {
      const response = await fetch(form.dataset.previewUrl, {
        method: "POST",
        body: new URLSearchParams(data),
        headers: { "X-CSRF-Token": form.elements.csrf.value },
      });
      if (response.ok) {
        card.outerHTML = await response.text();
      }
    } catch {
      // Sem rede, a prévia fica como estava; salvar continua funcionando.
    }
  }

  form.addEventListener("input", (event) => {
    if (event.target.name !== "sender_password" && event.target.name !== "test_to") {
      schedule();
    }
  });
})();
