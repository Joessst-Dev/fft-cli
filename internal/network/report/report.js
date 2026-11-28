"use strict";
// fft network report. Everything this draws was worked out by fft from the
// snapshot; nothing here decides what reaches what. Tenant-supplied text only
// ever reaches the page through textContent, never as markup.
(function () {
  var view = JSON.parse(document.getElementById("view").textContent);
  var app = document.getElementById("app");

  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        if (k === "text") node.textContent = attrs[k];
        else if (k === "class") node.className = attrs[k];
        else node.setAttribute(k, attrs[k]);
      });
    }
    (children || []).forEach(function (c) {
      if (c === null || c === undefined) return;
      node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
    });
    return node;
  }

  var fmt = new Intl.NumberFormat("en");
  function num(n) { return fmt.format(n); }
  function pct(have, size) {
    if (size === 0) return "–";
    var p = (100 * have) / size;
    // Never round a gap away: 99.6% of a group is not everyone.
    if (have < size && p > 99) return ">99%";
    if (have > 0 && p < 1) return "<1%";
    return Math.round(p) + "%";
  }
  // The ramp encodes the share of the group a cell misses.
  function bucket(missing, size) {
    if (size === 0 || missing === 0) return "";
    var f = missing / size;
    if (f <= 0.05) return "g1";
    if (f <= 0.10) return "g2";
    if (f <= 0.25) return "g3";
    if (f <= 0.50) return "g4";
    if (f <= 0.75) return "g5";
    return "g6";
  }
  var kindNames = { hub: "Hubs", supplier: "Suppliers", any: "Any source" };
  var kindNoun = { hub: "a hub", supplier: "a supplier", any: "some source" };

  // --- Header -----------------------------------------------------------------

  var date = view.snapshotAt ? view.snapshotAt.slice(0, 10) : "unknown";
  app.appendChild(el("header", null, [
    el("h1", { text: "Routing coverage — " + (view.project || "snapshot") }),
    el("div", { class: "sub", text: "Snapshot taken " + date + ", drawn by fft " + (view.fftVersion || "dev") +
      ". Every number comes from the same analysis as 'fft network check'." }),
    el("div", { class: "tiles" }, [
      tile(view.counts.facilities, "facilities", num(view.counts.online) + " online"),
      tile(view.counts.connections, "connections"),
      tile(view.counts.hubs, "hubs"),
      tile(view.counts.stores, "stores"),
      tile(view.counts.suppliers, "suppliers"),
      tile(totalFindings(), "findings", view.suppressed ? num(view.suppressed) + " suppressed" : null)
    ])
  ]));

  function tile(value, label, extra) {
    return el("div", { class: "tile" }, [
      el("div", { class: "value", text: num(value) }),
      el("div", { class: "label", text: label + (extra ? " · " + extra : "") })
    ]);
  }
  function totalFindings() {
    return Object.keys(view.findings).reduce(function (s, k) { return s + view.findings[k]; }, 0);
  }

  var decided = el("section", { class: "card" }, [el("h2", { text: "What the check assumed" })]);
  var facts = el("dl", { class: "facts" });
  function fact(term, value) { facts.appendChild(el("dt", { text: term })); facts.appendChild(el("dd", null, [value])); }
  if (view.decided.hubCut) {
    fact("Hubs", view.counts.hubs + " facilities whose connections reach " + num(view.decided.hubCut) +
      " or more online facilities, " + view.decided.hubJump.toFixed(1) + "× the next one down");
  } else {
    fact("Hubs", view.counts.hubs ? view.counts.hubs + " facilities, as the config file pins them" :
      "none: no facility's connections reach far more facilities than the rest");
  }
  if (view.decided.hubs.length) {
    var details = el("details", null, [el("summary", { text: "Show the hubs" }),
      el("p", { class: "note", text: view.decided.hubs.join(", ") +
        (view.decided.hubs.length < view.counts.hubs ? " …" : "") })]);
    fact("", details);
  }
  fact("Peer groups", "stores grouped by " + view.decided.peerBy + ": " + view.peers.length + " groups");
  fact("Order profiles", view.profiles.length + " kinds of order" +
    (view.profiles.some(function (p) { return p.orders > 0; }) ? ", taken from routed orders" :
      ", one per tag value a connection asks for (the snapshot has no routed orders)"));
  fact("Config file", view.decided.fromFile ? "fft-network.yaml decided part of this" : "none; everything was inferred");
  decided.appendChild(facts);
  app.appendChild(decided);

  var rules = Object.keys(view.findings);
  if (rules.length) {
    var list = el("ul", { class: "rules" });
    rules.forEach(function (r) {
      list.appendChild(el("li", null, [el("span", { class: "count", text: num(view.findings[r]) }), el("code", { text: r })]));
    });
    app.appendChild(el("section", { class: "card" }, [
      el("h2", { text: "Findings by rule" }), list,
      el("p", { class: "note", text: "'fft network check' lists each one with its evidence and next step." })
    ]));
  }

  // --- Coverage heatmap -------------------------------------------------------

  if (!view.peers.length || !view.profiles.length) {
    app.appendChild(el("section", { class: "card" }, [el("h2", { text: "Coverage" }),
      el("p", { class: "note", text: "The network has no stores or no kinds of order to compare, so there is no coverage to draw." })]));
    return;
  }

  var state = { kind: view.coverage[0].kind, cell: null };
  var heat = el("section", { class: "card" });
  app.appendChild(heat);
  var detail = el("section", { class: "card", id: "detail" });
  app.appendChild(detail);

  function matrix() {
    for (var i = 0; i < view.coverage.length; i++) if (view.coverage[i].kind === state.kind) return view.coverage[i];
    return view.coverage[0];
  }

  function drawHeat() {
    heat.textContent = "";
    var m = matrix();
    heat.appendChild(el("h2", { text: "Coverage by " + view.decided.peerBy + " and kind of order" }));
    heat.appendChild(el("p", { class: "note", text: "Each cell: the share of the group's stores " + kindNoun[m.kind] +
      " reaches, whatever the order's items and recipient. The darker the cell, the more stores it misses. " +
      "A flag counts the missing stores the check reported." }));

    var seg = el("div", { class: "segmented", role: "group", "aria-label": "Kind of source" });
    view.coverage.forEach(function (c) {
      var b = el("button", { type: "button", "aria-pressed": String(c.kind === m.kind), text: kindNames[c.kind] || c.kind });
      b.addEventListener("click", function () { state.kind = c.kind; state.cell = null; drawHeat(); drawDetail(); });
      seg.appendChild(b);
    });
    heat.appendChild(el("div", { class: "controls" }, [seg]));

    var thead = el("tr", null, [el("th", { scope: "col", text: view.decided.peerBy })]);
    view.profiles.forEach(function (p) {
      thead.appendChild(el("th", { scope: "col" }, [p.name,
        el("span", { class: "orders", text: p.orders ? num(p.orders) + " orders" : "declared" })]));
    });
    var tbody = el("tbody");
    view.peers.forEach(function (peer, gi) {
      var row = el("tr", null, [el("th", { scope: "row" }, [peer.key, el("span", { class: "size", text: num(peer.size) })])]);
      view.profiles.forEach(function (p, pi) {
        var c = m.cells[gi][pi];
        var missing = peer.size - c.have;
        var selected = state.cell && state.cell[0] === gi && state.cell[1] === pi;
        var label = peer.key + ", " + p.name + ": " + kindNoun[m.kind] + " reaches " + num(c.have) + " of " +
          num(peer.size) + " stores" + (missing ? "; " + num(missing) + " missing" : "") +
          (c.conditional ? ", " + num(c.conditional) + " of them only for some items or recipients" : "") +
          (c.flagged ? "; " + num(c.flagged) + " reported by the check" : "");
        var b = el("button", { type: "button", class: bucket(missing, peer.size), title: label, "aria-label": label,
          "aria-pressed": String(!!selected) }, [
          el("span", { class: "share", text: pct(c.have, peer.size) }),
          c.flagged ? el("span", { class: "flag", text: "⚑ " + num(c.flagged) }) : null,
          el("span", { class: "of", text: num(c.have) + "/" + num(peer.size) })
        ]);
        b.addEventListener("click", function () { state.cell = [gi, pi]; drawHeat(); drawDetail();
          if (detail.scrollIntoView) detail.scrollIntoView({ block: "nearest" }); });
        row.appendChild(el("td", null, [b]));
      });
      tbody.appendChild(row);
    });
    heat.appendChild(el("div", { class: "scroll" }, [el("table", { class: "heat" }, [el("thead", null, [thead]), tbody])]));
    heat.appendChild(legend());
  }

  function legend() {
    var steps = [["", "none"], ["g1", "≤5%"], ["g2", "≤10%"], ["g3", "≤25%"], ["g4", "≤50%"], ["g5", "≤75%"], ["g6", "more"]];
    var l = el("div", { class: "legend" }, [el("span", { text: "Stores missed:" })]);
    steps.forEach(function (s) {
      l.appendChild(el("span", { class: "step" }, [el("span", { class: "swatch " + s[0] }), el("span", { text: s[1] })]));
    });
    l.appendChild(el("span", { class: "gap" }, [el("span", { class: "flag", text: "⚑ n" }), " reported by the check"]));
    return l;
  }

  function drawDetail() {
    detail.textContent = "";
    if (!state.cell) {
      detail.appendChild(el("p", { class: "note", text: "Choose a cell to list the stores it misses." }));
      return;
    }
    var m = matrix();
    var peer = view.peers[state.cell[0]], profile = view.profiles[state.cell[1]];
    var c = m.cells[state.cell[0]][state.cell[1]];
    var missing = peer.size - c.have;
    detail.appendChild(el("h2", { text: peer.key + " · " + profile.name }));
    detail.appendChild(el("p", { class: "note", text: missing ?
      num(missing) + " of " + num(peer.size) + " stores no " + (m.kind === "any" ? "source" : m.kind) +
        " reaches for this kind of order" + (c.conditional ? "; " + num(c.conditional) +
        " of them are reached only for some items or recipients" : "") + "." :
      "Every store in the group is reached." }));
    if (!missing) return;

    var rows = el("tbody");
    (c.missing || []).forEach(function (label) {
      var f = view.facilities[label] || {};
      var rulesCell = el("td", { class: "rules-cell" });
      (f.rules || []).forEach(function (r) { rulesCell.appendChild(el("code", { text: r })); rulesCell.appendChild(document.createTextNode(" ")); });
      rows.appendChild(el("tr", null, [el("td", null, [el("code", { text: label })]), el("td", { text: f.name || "" }), rulesCell,
        el("td", null, [el("code", { text: "fft network reach --to " + label + profileFlags(profile) })])]));
    });
    detail.appendChild(el("table", { class: "list" }, [
      el("thead", null, [el("tr", null, [el("th", { text: "Store" }), el("th", { text: "Name" }), el("th", { text: "Findings" }),
        el("th", { text: "Why it is not reached" })])]),
      rows
    ]));
    if (c.more) {
      detail.appendChild(el("p", { class: "note", text: "… and " + num(c.more) + " more. 'fft network check -o json' lists them all." }));
    }
  }

  function profileFlags(profile) {
    return profile.tags.map(function (t) { return " --tag " + t.id + "=" + t.value; }).join("");
  }

  drawHeat();
  drawDetail();
})();
