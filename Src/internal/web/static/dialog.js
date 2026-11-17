document.addEventListener("click", (event) => {
  const open = event.target.closest("[data-open]");
  if (open) {
    const box = document.getElementById(open.getAttribute("data-open"));
    if (box && box.showModal) {
      box.showModal();
    }
  }
  const close = event.target.closest("[data-close]");
  if (close) {
    const box = close.closest("dialog");
    if (box) {
      box.close();
    }
  }
});
