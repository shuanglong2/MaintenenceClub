(function () {
  var cdn = "/static/vditor";
  var math = {
    engine: "KaTeX",
    inlineDigit: false,
    macros: {
      "\\dd": "\\mathrm{d}",
      "\\dv": "\\frac{\\mathrm{d}#1}{\\mathrm{d}#2}",
      "\\pdv": "\\frac{\\partial #1}{\\partial #2}",
      "\\vb": "\\boldsymbol{#1}",
      "\\vu": "\\hat{\\boldsymbol{#1}}",
      "\\abs": "\\left|#1\\right|",
      "\\norm": "\\left\\|#1\\right\\|",
      "\\grad": "\\boldsymbol{\\nabla}",
      "\\curl": "\\boldsymbol{\\nabla}\\times #1",
      "\\divg": "\\boldsymbol{\\nabla}\\cdot #1",
      "\\order": "\\mathcal{O}\\left(#1\\right)",
      "\\qty": "\\left(#1\\right)"
    }
  };
  var source = document.getElementById("doc-source");
  var preview = document.getElementById("doc-preview");
  if (source && preview && window.Vditor && typeof Vditor.preview === "function") {
    Vditor.preview(preview, source.value, {
      cdn: cdn,
      mode: "light",
      theme: { current: "light" },
      math: math,
      hljs: { style: "github", lineNumber: false }
    });
  }

  var body = document.getElementById("doc-body");
  var host = document.getElementById("doc-editor");
  if (!body || !host || !window.Vditor || typeof Vditor !== "function") {
    return;
  }
  var form = body.closest("form");
  var csrf = form.querySelector('input[name="csrf"]');
  var ready = false;
  var editor;
  editor = new Vditor(host, {
    cdn: cdn,
    mode: "sv",
    lang: "zh_CN",
    theme: "classic",
    icon: "ant",
    height: "640px",
    value: body.value,
    placeholder: "Markdown. 行内公式 $F=ma$, 独立公式用 $$ 包起来.",
    cache: { enable: false },
    after: function () { ready = true; },
    preview: {
      math: math,
      hljs: { style: "github", lineNumber: false },
      theme: { current: "light" }
    },
    toolbar: [
      "headings", "bold", "italic", "strike", "|",
      "quote", "list", "ordered-list", "check", "|",
      "code", "inline-code", "link", "table", "|",
      "upload",
      {
        name: "compile",
        tip: "编译",
        icon: '<svg viewBox="0 0 32 32"><path d="M6 8h20M6 16h12M6 24h20"/></svg>',
        click: function () { editor.setPreviewMode("both"); }
      },
      "both",
      "preview",
      "edit-mode",
      "fullscreen"
    ],
    upload: {
      url: "/docs/upload",
      accept: "image/*",
      fieldName: "file",
      multiple: true,
      max: 20 * 1024 * 1024,
      handler: function (files) {
        var token = csrf ? csrf.value : "";
        var failed = "";
        var chain = Promise.resolve();
        Array.prototype.forEach.call(files, function (file) {
          chain = chain.then(function () {
            if (failed) {
              return;
            }
            var data = new FormData();
            data.append("file", file);
            data.append("csrf", token);
            return fetch("/docs/upload", {
              method: "POST",
              body: data,
              headers: { Accept: "application/json", "X-CSRF-Token": token }
            }).then(function (res) {
              if (!res.ok) {
                failed = "上传失败";
                return;
              }
              return res.json().then(function (payload) {
                if (payload && payload.markdown) {
                  editor.insertValue(payload.markdown + "\n");
                }
              });
            });
          });
        });
        return chain.then(function () { return failed || null; }, function () { return "上传失败"; });
      }
    }
  });
  form.addEventListener("submit", function (event) {
    if (!ready) {
      event.preventDefault();
      return;
    }
    body.value = editor.getValue();
  });
})();
