document.addEventListener("DOMContentLoaded", function () {
  document.querySelectorAll("form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (event) {
      if (!window.confirm(form.dataset.confirm)) {
        event.preventDefault();
      }
    });
  });

  document.querySelectorAll("[data-reload]").forEach(function (button) {
    button.addEventListener("click", function () {
      window.location.reload();
    });
  });

  const autoRefresh = document.querySelector("[data-refresh]");
  if (autoRefresh) {
    const seconds = parseInt(autoRefresh.getAttribute("data-refresh"), 10);
    if (seconds > 0) {
      setTimeout(function () {
        window.location.reload();
      }, seconds * 1000);
    }
  }

  document.querySelectorAll("[data-copy]").forEach(function (button) {
    button.addEventListener("click", async function () {
      const value = button.getAttribute("data-copy");
      try {
        await navigator.clipboard.writeText(value);
        button.textContent = "Copiado";
        setTimeout(function () {
          button.textContent = "Copiar";
        }, 1500);
      } catch (error) {
        window.prompt("Copia el valor:", value);
      }
    });
  });
});
