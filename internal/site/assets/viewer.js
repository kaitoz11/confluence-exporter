/* Confluence Space Exporter — offline space viewer.
 * Plain JavaScript, no libraries, works from local files (file://). */
(function () {
  "use strict";
  var page = window.PAGE || { root: "", path: "" };
  var space = window.SPACE || { title: "", tree: [] };
  var $ = function (id) { return document.getElementById(id); };
  var el = function (tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  };
  var unescapePath = function (p) { return p.split("/").map(decodeURIComponent).join("/"); };
  var chevron = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';

  // ---- Page tree -------------------------------------------------------
  var tree = $("tree");
  var activeRow = null;

  function build(items, depth) {
    var ul = el("ul");
    if (depth === 0) ul.setAttribute("role", "tree");
    items.forEach(function (it) {
      var li = el("li");
      var row = el("div", "row");
      var twisty = el("button", "twisty" + (it.c ? "" : " leaf"));
      twisty.type = "button";
      twisty.innerHTML = chevron;
      twisty.setAttribute("aria-label", "Show or hide sub-pages of " + it.t);
      twisty.tabIndex = it.c ? 0 : -1;
      row.appendChild(twisty);
      var label;
      if (it.h) {
        label = el("a", null, it.t);
        label.href = page.root + it.h;
        if (unescapePath(it.h) === page.path) {
          row.classList.add("active");
          label.setAttribute("aria-current", "page");
          activeRow = row;
        }
      } else {
        label = el("span", "label", it.t);
      }
      label.title = it.t;
      row.appendChild(label);
      li.appendChild(row);
      if (it.c) {
        li.appendChild(build(it.c, depth + 1));
        twisty.addEventListener("click", function () { li.classList.toggle("open"); });
        if (!it.h) label.addEventListener("click", function () { li.classList.toggle("open"); });
      }
      ul.appendChild(li);
    });
    return ul;
  }

  if (tree) {
    tree.appendChild(build(space.tree, 0));
    // Open the path to the current page (or the first level on the overview).
    if (activeRow) {
      for (var n = activeRow.parentNode; n && n !== tree; n = n.parentNode) {
        if (n.tagName === "LI") n.classList.add("open");
      }
      activeRow.scrollIntoView({ block: "center" });
    } else {
      Array.prototype.forEach.call(tree.querySelectorAll(":scope > ul > li"), function (li) { li.classList.add("open"); });
    }
  }

  // ---- Search ------------------------------------------------------------
  var input = $("search");
  var results = $("results");
  var selected = -1;

  // Accent-insensitive matching: "huong dan" finds "Hướng dẫn".
  function fold(s) {
    return s.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d");
  }

  function loadIndex(done) {
    if (window.SEARCH) return done();
    var s = document.createElement("script");
    s.src = page.root + "_viewer/search.js";
    s.onload = done;
    s.onerror = function () { window.SEARCH = []; done(); };
    document.head.appendChild(s);
  }

  function highlight(text, words) {
    var frag = document.createDocumentFragment();
    var folded = fold(text);
    var pos = 0;
    while (pos < text.length) {
      var best = -1, len = 0;
      words.forEach(function (w) {
        var i = folded.indexOf(w, pos);
        if (i >= 0 && (best < 0 || i < best)) { best = i; len = w.length; }
      });
      if (best < 0) break;
      frag.appendChild(document.createTextNode(text.slice(pos, best)));
      frag.appendChild(el("mark", null, text.slice(best, best + len)));
      pos = best + len;
    }
    frag.appendChild(document.createTextNode(text.slice(pos)));
    return frag;
  }

  function snippet(text, folded, words) {
    var i = folded.indexOf(words[0]);
    if (i < 0) return text.slice(0, 140);
    var start = Math.max(0, i - 50);
    return (start > 0 ? "…" : "") + text.slice(start, start + 160) + "…";
  }

  function runSearch() {
    var q = fold(input.value.trim());
    if (!q) { results.hidden = true; tree.hidden = false; return; }
    loadIndex(function () {
      var words = q.split(/\s+/).filter(Boolean);
      var hits = [];
      window.SEARCH.forEach(function (p) {
        var t = fold(p.t), x = fold(p.x);
        var score = 0;
        for (var i = 0; i < words.length; i++) {
          var inTitle = t.indexOf(words[i]) >= 0;
          if (!inTitle && x.indexOf(words[i]) < 0) return;
          score += inTitle ? 10 : 1;
        }
        if (t.indexOf(q) === 0) score += 20;
        hits.push({ p: p, score: score, x: x });
      });
      hits.sort(function (a, b) { return b.score - a.score; });
      results.innerHTML = "";
      if (!hits.length) results.appendChild(el("div", "empty", "No pages match “" + input.value.trim() + "”."));
      hits.slice(0, 60).forEach(function (h) {
        var a = el("a");
        a.href = page.root + h.p.h;
        var strong = el("strong");
        strong.appendChild(highlight(h.p.t, words));
        var small = el("small");
        small.appendChild(highlight(snippet(h.p.x, h.x, words), words));
        a.appendChild(strong);
        a.appendChild(small);
        results.appendChild(a);
      });
      selected = hits.length ? 0 : -1;
      mark();
      tree.hidden = true;
      results.hidden = false;
    });
  }

  function mark() {
    var links = results.querySelectorAll("a");
    Array.prototype.forEach.call(links, function (a, i) { a.classList.toggle("sel", i === selected); });
    if (links[selected]) links[selected].scrollIntoView({ block: "nearest" });
  }

  if (input) {
    input.addEventListener("input", runSearch);
    input.addEventListener("focus", function () { loadIndex(function () {}); });
    input.addEventListener("keydown", function (e) {
      var links = results.querySelectorAll("a");
      if (e.key === "ArrowDown") { selected = Math.min(selected + 1, links.length - 1); mark(); e.preventDefault(); }
      else if (e.key === "ArrowUp") { selected = Math.max(selected - 1, 0); mark(); e.preventDefault(); }
      else if (e.key === "Enter" && links[selected]) { location.href = links[selected].href; }
      else if (e.key === "Escape") { input.value = ""; runSearch(); input.blur(); }
    });
    // index.html?q=word opens with that search, so a search can be shared.
    var q = new URLSearchParams(location.search).get("q");
    if (q) { input.value = q; runSearch(); }
  }

  // ---- Keyboard shortcuts --------------------------------------------------
  document.addEventListener("keydown", function (e) {
    var typing = /INPUT|TEXTAREA/.test(document.activeElement.tagName);
    if (e.key === "/" && !typing) { e.preventDefault(); openNav(true); input.focus(); }
    if (e.key === "Escape") { closeLightbox(); openNav(false); }
    if (!typing && !e.metaKey && !e.ctrlKey && (e.key === "ArrowLeft" || e.key === "ArrowRight") && e.altKey) {
      var a = document.querySelector(e.key === "ArrowLeft" ? ".pager .prev" : ".pager .next");
      if (a) location.href = a.href;
    }
  });

  // ---- Mobile navigation ---------------------------------------------------
  function openNav(open) { document.body.classList.toggle("nav-open", open); }
  var menu = $("menu"), scrim = $("scrim");
  if (menu) menu.addEventListener("click", function () { openNav(!document.body.classList.contains("nav-open")); });
  if (scrim) scrim.addEventListener("click", function () { openNav(false); });

  // ---- "On this page" outline --------------------------------------------
  var content = document.querySelector(".content");
  var outline = $("outline");
  var heads = content ? content.querySelectorAll("h2[id], h3[id]") : [];
  if (outline && heads.length >= 2) {
    outline.appendChild(el("h4", null, "On this page"));
    var links = [];
    Array.prototype.forEach.call(heads, function (h) {
      var a = el("a", h.tagName === "H3" ? "sub" : "", h.textContent);
      a.href = "#" + h.id;
      outline.appendChild(a);
      links.push(a);
    });
    var spy = function () {
      var current = 0;
      Array.prototype.forEach.call(heads, function (h, i) { if (h.getBoundingClientRect().top < 120) current = i; });
      links.forEach(function (a, i) { a.classList.toggle("on", i === current); });
    };
    document.addEventListener("scroll", spy, { passive: true });
    spy();
  }

  // ---- Attachments: show the file type on each card -----------------------
  if (content) {
    Array.prototype.forEach.call(content.querySelectorAll("ul.attachments li"), function (li) {
      var a = li.querySelector("a");
      var m = a && /\.([a-z0-9]{1,5})$/i.exec(decodeURIComponent(a.getAttribute("href")));
      li.setAttribute("data-ext", m ? m[1] : "file");
    });
  }

  // ---- Image viewer ----------------------------------------------------------
  var box = $("lightbox");
  function closeLightbox() { if (box) box.hidden = true; }
  if (box && content) {
    content.addEventListener("click", function (e) {
      if (e.target.tagName !== "IMG" || e.target.closest("a")) return;
      box.querySelector("img").src = e.target.src;
      box.querySelector("img").alt = e.target.alt;
      box.hidden = false;
    });
    box.addEventListener("click", closeLightbox);
  }
})();
