// Código de página (roda no navegador, não no Node): importação do Portal
// Nacional. Abre a janela em Modelos, ou começa sozinho em /portal (depois do
// certificado), acende as etapas enquanto o servidor consulta e troca o
// cartão pelo resultado que o servidor renderizou (fragmento portal_result).
// Sem JavaScript, o mesmo formulário faz o POST e a página volta pronta.
(() => {
  // stepDelay é o ritmo das etapas na tela, em ms: só visual — quem diz que
  // terminou é a resposta do servidor, que traz o cartão final.
  const stepDelay = 1200;

  // run dispara a importação dentro de um contêiner com o cartão "consultando".
  // Devolve uma promessa que resolve quando o resultado está na tela.
  function run(container) {
    const form = container.querySelector("form[data-portal-import]");
    const card = container.querySelector("[data-portal-running]");
    if (!form || !card) {
      return Promise.resolve(false);
    }
    form.hidden = true;
    const steps = [...card.querySelectorAll("[data-step]")];
    let current = 0;
    steps[0]?.classList.add("import-step-active");
    // Avança até a penúltima: a última fica girando até a resposta chegar.
    const timer = setInterval(() => {
      if (current < steps.length - 1) {
        steps[current].classList.replace("import-step-active", "import-step-done");
        current += 1;
        steps[current].classList.add("import-step-active");
      }
    }, stepDelay);
    return fetch(form.action, {
      method: "POST",
      credentials: "same-origin",
      headers: { "X-CSRF-Token": form.elements.csrf.value, "X-Fragment": "portal_result" },
    })
      .then((response) => response.text())
      .then((html) => {
        clearInterval(timer);
        // HTML do próprio servidor, já escapado pelo html/template.
        card.outerHTML = html;
        return true;
      })
      .catch(() => {
        // Rede caiu: devolve o botão para tentar de novo.
        clearInterval(timer);
        steps.forEach((step) => step.classList.remove("import-step-active", "import-step-done"));
        form.hidden = false;
        return false;
      });
  }

  // Página /portal sem resultado: começa sozinha.
  const autostart = document.querySelector("[data-portal-autostart]");
  if (autostart) {
    run(autostart);
  }

  // Modelos: o botão abre a janela e começa; fechar depois do resultado
  // recarrega a lista, que agora tem os modelos importados.
  const dialog = document.querySelector("[data-portal-dialog]");
  const opener = document.querySelector("[data-portal-open]");
  if (dialog && opener) {
    const pristine = dialog.querySelector("[data-portal-body]").innerHTML;
    let finished = false;
    opener.addEventListener("click", (event) => {
      event.preventDefault();
      dialog.querySelector("[data-portal-body]").innerHTML = pristine;
      dialog.showModal();
      run(dialog).then((done) => {
        finished = done;
      });
    });
    dialog.addEventListener("close", () => {
      if (finished) {
        window.location.reload();
      }
    });
    dialog.querySelector("[data-portal-close]").addEventListener("click", () => dialog.close());
  }
})();
