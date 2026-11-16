(function () {
  var box = document.getElementById("doc-body");
  if (!box) return;
  box.addEventListener("dragover", function (ev) { ev.preventDefault(); });
  box.addEventListener("drop", function (ev) {
    ev.preventDefault();
    var file = ev.dataTransfer && ev.dataTransfer.files && ev.dataTransfer.files[0];
    if (!file) return;
    var form = box.closest("form");
    var token = form.querySelector('input[name="csrf"]');
    var data = new FormData();
    data.append("csrf", token ? token.value : "");
    data.append("file", file);
    fetch("/docs/upload", { method: "POST", body: data, headers: { Accept: "application/json" } })
      .then(function (res) { return res.json(); })
      .then(function (payload) {
        if (!payload || !payload.markdown) return;
        var start = box.selectionStart || box.value.length;
        var end = box.selectionEnd || start;
        box.value = box.value.slice(0, start) + payload.markdown + "\n" + box.value.slice(end);
      });
  });
})();
