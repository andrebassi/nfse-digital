// Código de página (roda no navegador, não no Node): botões "Copiar" da aba
// API REST. Cada botão aponta, em data-copy, o id do bloco que copia. Os
// botões nascem escondidos e só aparecem aqui: sem JavaScript (ou sem a API de
// clipboard), sobra o clique que seleciona o bloco inteiro.
(() => {
  if (!navigator.clipboard) {
    return;
  }
  // feedbackDelay é quanto tempo, em ms, o botão mostra "Copiado" antes de voltar.
  const feedbackDelay = 1500;
  for (const button of document.querySelectorAll("button[data-copy]")) {
    const source = document.getElementById(button.dataset.copy);
    if (!source) {
      continue;
    }
    button.hidden = false;
    const label = button.textContent;
    button.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(source.textContent.trim());
        button.textContent = "Copiado";
      } catch {
        button.textContent = "Não copiou: selecione e copie";
      }
      setTimeout(() => {
        button.textContent = label;
      }, feedbackDelay);
    });
  }
})();
