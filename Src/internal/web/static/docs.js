(function () {
  var csrfInput = document.getElementById("doc-csrf");
  var menu = document.getElementById("doc-menu");
  var dialog = document.getElementById("doc-dialog");
  var dialogForm = document.getElementById("doc-dialog-form");
  if (menu) document.body.appendChild(menu);
  if (dialog) document.body.appendChild(dialog);
  var target = null;
  var action = "";

  function csrf() {
    return csrfInput ? csrfInput.value : "";
  }

  function post(url, fields) {
    var form = document.createElement("form");
    form.method = "post";
    form.action = url;
    fields.csrf = csrf();
    Object.keys(fields).forEach(function (key) {
      var input = document.createElement("input");
      input.type = "hidden";
      input.name = key;
      input.value = fields[key] == null ? "" : String(fields[key]);
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
  }

  function hideMenu() {
    if (menu) menu.hidden = true;
  }

  function showMenu(x, y, row) {
    if (!menu) return;
    target = row;
    var kind = row ? row.getAttribute("data-kind") : "";
    menu.querySelectorAll("button").forEach(function (button) {
      var act = button.getAttribute("data-act");
      var show = act === "new-doc" || act === "new-folder";
      if (act === "rename" || act === "move" || act === "color" || act === "icon" || act === "delete") show = !!kind;
      if (act === "copy") show = kind === "page";
      button.hidden = !show;
    });
    menu.hidden = false;
    var width = menu.offsetWidth;
    var height = menu.offsetHeight;
    menu.style.left = Math.max(8, Math.min(x, window.innerWidth - width - 8)) + "px";
    menu.style.top = Math.max(8, Math.min(y, window.innerHeight - height - 8)) + "px";
  }

  function field(id, on) {
    var node = document.getElementById(id);
    if (node) node.hidden = !on;
  }

  function setRadio(value) {
    var picked = dialogForm.querySelector('input[name="tone"][value="' + value + '"]');
    if (picked) picked.checked = true;
  }

  function openDialog(kind, row) {
    if (!dialog) return;
    target = row;
    action = kind;
    var name = document.getElementById("doc-dialog-name");
    var parent = document.getElementById("doc-dialog-parent");
    var mark = document.getElementById("doc-dialog-mark");
    var hint = document.getElementById("doc-dialog-hint");
    var title = document.getElementById("doc-dialog-title");
    name.value = "";
    hint.textContent = "";
    field("doc-field-name", false);
    field("doc-field-parent", false);
    field("doc-field-tone", false);
    field("doc-field-mark", false);
    var folder = "0";
    if (row && row.getAttribute("data-kind") === "folder") folder = row.getAttribute("data-id");
    else if (row && row.getAttribute("data-folder")) folder = row.getAttribute("data-folder");
    else if (document.querySelector(".doc-side")) folder = document.querySelector(".doc-side").getAttribute("data-folder") || "0";
    if (kind === "new-doc" || kind === "new-folder") {
      title.textContent = kind === "new-doc" ? "新建文档" : "新建文件夹";
      field("doc-field-name", true);
      field("doc-field-parent", true);
      parent.value = folder;
      if (kind === "new-folder") {
        field("doc-field-tone", true);
        field("doc-field-mark", true);
        setRadio("ink");
        mark.value = "folder";
      }
      name.required = true;
    } else if (kind === "rename") {
      title.textContent = "重命名";
      field("doc-field-name", true);
      name.value = row.getAttribute("data-name") || "";
      name.required = true;
    } else if (kind === "move") {
      title.textContent = "移动";
      field("doc-field-parent", true);
      parent.value = row.getAttribute("data-kind") === "folder" ? (row.getAttribute("data-parent") || "0") : (row.getAttribute("data-folder") || "0");
      name.required = false;
    } else if (kind === "color") {
      title.textContent = "修改颜色";
      field("doc-field-tone", true);
      setRadio(row.getAttribute("data-tone") || "ink");
      name.required = false;
    } else if (kind === "icon") {
      title.textContent = "修改图标";
      field("doc-field-mark", true);
      mark.value = row.getAttribute("data-mark") || (row.getAttribute("data-kind") === "page" ? "note" : "folder");
      name.required = false;
    } else if (kind === "delete") {
      title.textContent = "删除";
      hint.textContent = row.getAttribute("data-kind") === "folder" ? "只删除空文件夹." : "从目录里拿掉, 之后还能恢复.";
      name.required = false;
    }
    var folderName = kind === "new-folder" || (row && row.getAttribute("data-kind") === "folder" && kind === "rename");
    name.maxLength = folderName ? 40 : 80;
    if (dialog.showModal) dialog.showModal();
    else dialog.hidden = false;
    if (!name.hidden && name.offsetParent !== null) name.focus();
  }

  function submitDialog(event) {
    event.preventDefault();
    var row = target;
    var name = document.getElementById("doc-dialog-name").value.trim();
    var parent = document.getElementById("doc-dialog-parent").value;
    var tone = "ink";
    var picked = dialogForm.querySelector('input[name="tone"]:checked');
    if (picked) tone = picked.value;
    var mark = document.getElementById("doc-dialog-mark").value;
    if (action === "new-doc") {
      post("/docs", { title: name, folder: parent, body: "", visibility: "all", tags: "", version: "0", next: "edit" });
      return;
    }
    if (action === "new-folder") {
      post("/docs/folders", { name: name, parent: parent, tone: tone, mark: mark });
      return;
    }
    if (!row) return;
    var id = row.getAttribute("data-id");
    var kind = row.getAttribute("data-kind");
    if (action === "rename") {
      if (kind === "folder") post("/docs/folders/" + id, { name: name, parent: row.getAttribute("data-parent") || "0", tone: row.getAttribute("data-tone") || "ink", mark: row.getAttribute("data-mark") || "folder" });
      else post("/docs/p/" + id + "/rename", { name: name });
    } else if (action === "move") {
      if (kind === "folder") post("/docs/folders/" + id, { name: row.getAttribute("data-name"), parent: parent, tone: row.getAttribute("data-tone") || "ink", mark: row.getAttribute("data-mark") || "folder" });
      else post("/docs/p/" + id + "/move", { folder: parent });
    } else if (action === "color") {
      if (kind === "folder") post("/docs/folders/" + id, { name: row.getAttribute("data-name"), parent: row.getAttribute("data-parent") || "0", tone: tone, mark: row.getAttribute("data-mark") || "folder" });
      else post("/docs/p/" + id + "/mark", { tone: tone, mark: row.getAttribute("data-mark") || "note" });
    } else if (action === "icon") {
      if (kind === "folder") post("/docs/folders/" + id, { name: row.getAttribute("data-name"), parent: row.getAttribute("data-parent") || "0", tone: row.getAttribute("data-tone") || "ink", mark: mark });
      else post("/docs/p/" + id + "/mark", { tone: row.getAttribute("data-tone") || "ink", mark: mark });
    } else if (action === "delete") {
      if (kind === "folder") post("/docs/folders/" + id + "/delete", {});
      else post("/docs/p/" + id + "/void", { reason: "删除" });
    }
  }

  function closedMap() {
    var map = {};
    try {
      JSON.parse(localStorage.getItem("mclub-docs-closed") || "[]").forEach(function (id) { map[String(id)] = true; });
    } catch (err) { /* 忽略损坏的本地记录 */ }
    return map;
  }

  function remember(id, open) {
    var map = closedMap();
    if (open) delete map[String(id)];
    else map[String(id)] = true;
    localStorage.setItem("mclub-docs-closed", JSON.stringify(Object.keys(map)));
  }

  function setOpen(node, open) {
    node.classList.toggle("open", open);
    var chevron = node.querySelector(":scope > .row .chevron");
    if (chevron) chevron.setAttribute("aria-expanded", open ? "true" : "false");
  }

  var tree = document.getElementById("doc-tree");
  if (tree) {
    var closed = closedMap();
    tree.querySelectorAll(".node").forEach(function (node) {
      var row = node.querySelector(":scope > .row");
      var id = row ? row.getAttribute("data-id") : "";
      var holdsCurrent = !!node.querySelector('[aria-current="page"]');
      setOpen(node, holdsCurrent || !closed[id]);
    });
    tree.addEventListener("click", function (event) {
      var chevron = event.target.closest(".chevron");
      var row = event.target.closest('.row[data-kind="folder"]');
      if (!chevron && !row) return;
      if (event.target.closest("a")) return;
      event.preventDefault();
      var node = (chevron || row).closest(".node");
      var open = !node.classList.contains("open");
      setOpen(node, open);
      var idRow = node.querySelector(":scope > .row");
      if (idRow) remember(idRow.getAttribute("data-id"), open);
    });
    tree.addEventListener("contextmenu", function (event) {
      var row = event.target.closest("[data-kind]");
      event.preventDefault();
      showMenu(event.clientX, event.clientY, row);
    });
  }

  var side = document.querySelector(".doc-side");
  if (side) {
    side.addEventListener("contextmenu", function (event) {
      if (event.target.closest("#doc-tree")) return;
      event.preventDefault();
      showMenu(event.clientX, event.clientY, null);
    });
  }
  document.addEventListener("click", hideMenu);
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") hideMenu();
  });
  if (menu) {
    menu.addEventListener("click", function (event) {
      var button = event.target.closest("button");
      if (!button) return;
      hideMenu();
      var act = button.getAttribute("data-act");
      if (act === "copy" && target) {
        post("/docs/p/" + target.getAttribute("data-id") + "/copy", {});
        return;
      }
      openDialog(act, target);
    });
  }
  var newDoc = document.getElementById("tree-new-doc");
  var newFolder = document.getElementById("tree-new-folder");
  if (newDoc) newDoc.addEventListener("click", function () { openDialog("new-doc", null); });
  if (newFolder) newFolder.addEventListener("click", function () { openDialog("new-folder", null); });
  if (dialogForm) dialogForm.addEventListener("submit", submitDialog);
  var cancel = document.getElementById("doc-dialog-cancel");
  if (cancel) cancel.addEventListener("click", function () { if (dialog.close) dialog.close(); });
  if (location.search.indexOf("new=page") >= 0) {
    openDialog("new-doc", null);
    history.replaceState(null, "", location.pathname);
  }

  var source = document.getElementById("doc-source");
  var preview = document.getElementById("doc-preview");
  if (source && preview && window.Vditor && typeof Vditor.preview === "function") {
    Vditor.preview(preview, source.value, {
      cdn: "/static/vditor",
      mode: "light",
      theme: { current: "light" },
      markdown: { mark: true, sanitize: true },
      math: mathOptions(),
      hljs: { style: "github", lineNumber: false }
    });
  }

  var body = document.getElementById("doc-body");
  var host = document.getElementById("doc-editor");
  if (!body || !host || !window.Vditor || typeof Vditor !== "function") return;
  var form = body.closest("form");
  var ready = false;
  var editor;

  function uploadFiles(files) {
    var token = csrf();
    var failed = "";
    var chain = Promise.resolve();
    Array.prototype.forEach.call(files, function (file) {
      chain = chain.then(function () {
        if (failed) return;
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
            if (payload && payload.markdown && editor) editor.insertValue(payload.markdown + "\n");
          });
        });
      });
    });
    return chain.then(function () { return failed || null; }, function () { return "上传失败"; });
  }

  function mount() {
    ready = false;
    if (editor && editor.destroy) editor.destroy();
    host.innerHTML = "";
    editor = new Vditor(host, {
      cdn: "/static/vditor",
      mode: "wysiwyg",
      lang: "zh_CN",
      theme: "classic",
      icon: "ant",
      height: "640px",
      value: body.value,
      placeholder: "直接写. 公式用 $F=ma$ 或 $$ .",
      cache: { enable: false },
      toolbar: [],
      markdown: { mark: true, sanitize: true },
      after: function () {
        ready = true;
        paintHTML(host);
      },
      input: function () {
        paintHTML(host);
      },
      preview: {
        markdown: { mark: true, sanitize: true },
        math: mathOptions(),
        hljs: { style: "github", lineNumber: false },
        theme: { current: "light" }
      },
      upload: {
        url: "/docs/upload",
        accept: "image/*",
        multiple: true,
        max: 20 * 1024 * 1024,
        handler: function (files) { return uploadFiles(files); }
      }
    });
  }

  function tagText(node) {
    return (node.textContent || "").replace(/\u200b/g, "").trim().toLowerCase();
  }

  function paintHTML(root) {
    var codes = Array.prototype.slice.call(root.querySelectorAll("code[data-type='html-inline']"));
    var i = 0;
    while (i < codes.length) {
      var open = tagText(codes[i]);
      var kind = "";
      var style = "";
      if (open === "<u>") kind = "u";
      else if (open === "<mark>") kind = "mark";
      else if (open.indexOf("<span") === 0) {
        kind = "span";
        var found = open.match(/style="([^"]*)"/);
        if (found) style = found[1];
      }
      if (!kind) {
        i += 1;
        continue;
      }
      var close = "</" + kind + ">";
      var j = i + 1;
      while (j < codes.length && tagText(codes[j]) !== close) j += 1;
      if (j >= codes.length) {
        i += 1;
        continue;
      }
      var el = document.createElement(kind);
      if (style) el.setAttribute("style", style);
      var node = codes[i].nextSibling;
      while (node && node !== codes[j]) {
        var next = node.nextSibling;
        el.appendChild(node);
        node = next;
      }
      codes[i].parentNode.insertBefore(el, codes[i]);
      codes[i].parentNode.removeChild(codes[i]);
      codes[j].parentNode.removeChild(codes[j]);
      i = j + 1;
    }
  }

  function focusEditor() {
    var nodes = document.querySelectorAll("#doc-editor [contenteditable='true']");
    if (nodes.length) nodes[nodes.length - 1].focus();
  }

  function put(markdown) {
    if (!ready) return;
    focusEditor();
    if (editor.getSelection()) document.execCommand("delete", false);
    editor.insertValue(markdown, true);
    paintHTML(host);
  }

  function wrap(left, right, fallback) {
    if (!ready) return;
    var sel = editor.getSelection() || fallback;
    focusEditor();
    if (editor.getSelection()) document.execCommand("delete", false);
    editor.insertValue(left + sel + right, true);
    paintHTML(host);
  }

  var block = document.getElementById("doc-block");
  if (block) {
    block.addEventListener("change", function () {
      if (!ready || block.value === "") return;
      var text = (editor.getSelection() || "标题").replace(/^#{1,6}\s*/, "");
      if (block.value === "0") put(text);
      else put(new Array(Number(block.value) + 1).join("#") + " " + text);
      block.value = "";
    });
  }
  document.querySelectorAll("[data-cmd]").forEach(function (button) {
    button.addEventListener("click", function () {
      var cmd = button.getAttribute("data-cmd");
      if (cmd === "bold") wrap("**", "**", "文字");
      else if (cmd === "mark") wrap("==", "==", "重点");
      else if (cmd === "under") wrap("<u>", "</u>", "下划线");
      else if (cmd === "math") {
        if (!ready) return;
        var sel = editor.getSelection();
        if (sel) put("$" + sel + "$");
        else put("\n$$\nF = ma\n$$\n");
      } else if (cmd === "table") {
        put("\n| 列 1 | 列 2 |\n| --- | --- |\n|  |  |\n");
      } else if (cmd === "image") {
        var picker = document.getElementById("doc-image");
        if (picker) picker.click();
      }
    });
  });
  var picker = document.getElementById("doc-image");
  if (picker) {
    picker.addEventListener("change", function () {
      if (picker.files && picker.files.length) uploadFiles(picker.files);
      picker.value = "";
    });
  }
  form.addEventListener("submit", function (event) {
    if (!ready) {
      event.preventDefault();
      return;
    }
    body.value = editor.getValue();
  });
  mount();

  function mathOptions() {
    return {
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
  }
})();
