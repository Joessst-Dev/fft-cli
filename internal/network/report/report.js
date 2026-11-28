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
  var nb = el("section", { class: "card", id: "neighbourhood" });
  app.appendChild(nb);

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
      rows.appendChild(el("tr", null, [el("td", null, [facilityLink(label, state.cell[1])]), el("td", { text: f.name || "" }), rulesCell,
        el("td", null, [el("code", { text: reachCommand(f, profile) })])]));
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

  // Go quoted every argument (a facility id and a tag value are tenant text, and
  // this line is one a user pastes into a shell); the page only joins them.
  function reachCommand(facility, profile) {
    return "fft network reach --to " + facility.reachArg + profile.reachFlags +
      (facility.unportable || profile.unportable ? "  (cannot be copied safely)" : "");
  }

  // --- Neighbourhood ----------------------------------------------------------

  var SVG = "http://www.w3.org/2000/svg";
  function svg(tag, attrs, children) {
    var node = document.createElementNS(SVG, tag);
    Object.keys(attrs || {}).forEach(function (k) {
      if (k === "text") node.textContent = attrs[k];
      else node.setAttribute(k, attrs[k]);
    });
    (children || []).forEach(function (c) { if (c) node.appendChild(c); });
    return node;
  }

  var resultIcon = { applies: "✓", conditional: "≈", excluded: "✗" };
  var kindPlural = { hub: "hub", supplier: "supplier", store: "store" };
  var neighbourhoods = Object.keys(view.neighbourhoods || {}).sort();

  function hashFor(label, profile) {
    return "#facility=" + encodeURIComponent(label) + "&profile=" + profile;
  }
  function facilityLink(label, profile) {
    var code = el("code", { text: label });
    if (!view.neighbourhoods[label]) return code;
    return el("a", { href: hashFor(label, profile), title: "Show the connections that could lead to " + label }, [code]);
  }
  function parseHash() {
    var params = new URLSearchParams(location.hash.slice(1));
    var label = params.get("facility");
    var profile = parseInt(params.get("profile") || "0", 10);
    if (!(profile >= 0 && profile < view.profiles.length)) profile = 0;
    return { label: label && view.neighbourhoods[label] ? label : null, profile: profile };
  }

  function drawNeighbourhood() {
    nb.textContent = "";
    var at = parseHash();
    nb.appendChild(el("h2", { text: "Connections into one facility" }));
    if (!neighbourhoods.length) {
      nb.appendChild(el("p", { class: "note", text: "No facility needed explaining. 'fft network reach --to <facility>' explains any of them." }));
      return;
    }

    var search = el("input", { type: "search", list: "nb-facilities", placeholder: "Facility", "aria-label": "Facility",
      value: at.label || "" });
    var options = el("datalist", { id: "nb-facilities" });
    neighbourhoods.forEach(function (l) { options.appendChild(el("option", { value: l })); });
    search.addEventListener("change", function () {
      if (view.neighbourhoods[search.value]) location.hash = hashFor(search.value, at.profile);
    });
    var select = el("select", { "aria-label": "Kind of order" });
    view.profiles.forEach(function (p, k) {
      var o = el("option", { value: String(k), text: p.name });
      if (k === at.profile) o.selected = true;
      select.appendChild(o);
    });
    select.addEventListener("change", function () {
      if (at.label) location.hash = hashFor(at.label, select.value);
      else at.profile = parseInt(select.value, 10);
    });
    nb.appendChild(el("div", { class: "controls" }, [search, options, select]));
    var omitted = view.omitted ? " " + num(view.omitted) + " more were left out; 'fft network reach' explains them." : "";
    if (!at.label) {
      nb.appendChild(el("p", { class: "note", text: "Choose a facility: " + num(neighbourhoods.length) +
        " are explained here, every one a finding names and the stores the coverage cells list." + omitted }));
      return;
    }

    var f = view.facilities[at.label] || {};
    var n = view.neighbourhoods[at.label];
    var profile = view.profiles[at.profile];
    var bundles = n.profiles[at.profile] || [];
    var count = { applies: 0, conditional: 0, excluded: 0 };
    bundles.forEach(function (b) { count[b.result] += b.count; });

    nb.appendChild(el("p", null, [el("strong", { text: at.label }), (f.name ? " — " + f.name : "") + ", " +
      [n.role || "no source role", f.status ? f.status.toLowerCase() : "", f.country || ""].filter(Boolean).join(", ") +
      ". Connections for an order with " + profile.name + ": " + num(count.applies) + " apply, " +
      num(count.conditional) + " apply only for some items or recipients, " + num(count.excluded) + " excluded."]));
    if (f.rules && f.rules.length) {
      var r = el("p", { class: "note" }, ["Findings: "]);
      f.rules.forEach(function (rule) { r.appendChild(el("code", { text: rule })); r.appendChild(document.createTextNode(" ")); });
      nb.appendChild(r);
    }
    if (!bundles.length) {
      nb.appendChild(el("p", { class: "note", text: "No connection from an online source could lead here: none names it, and none is a wildcard of its type." }));
      return;
    }
    nb.appendChild(drawBundles(at.label, bundles));
    nb.appendChild(bundleTable(bundles));
    nb.appendChild(el("p", { class: "note" }, ["Each connection, with its id: ",
      el("code", { text: reachCommand(f, profile) })]));
  }

  function bundleText(b) {
    return resultIcon[b.result] + " " + num(b.count) + " " + kindPlural[b.kind] +
      (b.count === 1 ? " connection " : " connections ") + b.result;
  }

  function drawBundles(label, bundles) {
    var rowH = 38, top = 10, width = 980, textX = 0, dotX = 560, nodeX = 860;
    var height = Math.max(top * 2 + bundles.length * rowH, 80);
    var mid = height / 2;
    var root = svg("svg", { viewBox: "0 0 " + width + " " + height, width: "100%", role: "img",
      "aria-label": "Connections into " + label + ", in bundles; the table below lists them" });
    var maxCount = bundles.reduce(function (m, b) { return Math.max(m, b.count); }, 1);
    bundles.forEach(function (b, k) {
      var y = top + k * rowH + rowH / 2;
      var w = 1.5 + 4.5 * Math.log(1 + b.count) / Math.log(1 + maxCount);
      var tip = bundleText(b) + "\n" + b.reason + "\nFrom: " + b.sources.join(", ") +
        (b.sourceCount > b.sources.length ? " and " + num(b.sourceCount - b.sources.length) + " more" : "");
      root.appendChild(svg("g", { class: "bundle r-" + b.result }, [
        svg("title", { text: tip }),
        svg("path", { class: "link", d: "M" + dotX + "," + y + " C" + (dotX + 150) + "," + y + " " +
          (nodeX - 150) + "," + mid + " " + (nodeX - 8) + "," + mid, "stroke-width": w.toFixed(2), fill: "none" }),
        svg("circle", { class: "dot", cx: dotX, cy: y, r: 4 }),
        svg("text", { class: "head", x: textX, y: y - 3, text: bundleText(b) }),
        svg("text", { class: "reason", x: textX, y: y + 12, text: shortText(b.reason, 84) })
      ]));
    });
    root.appendChild(svg("circle", { class: "target", cx: nodeX, cy: mid, r: 8 }));
    root.appendChild(svg("text", { class: "head", x: nodeX + 14, y: mid + 4, text: label }));
    return el("div", { class: "nb-graph" }, [root]);
  }

  function shortText(s, n) { return s.length <= n ? s : s.slice(0, n - 1) + "…"; }

  function bundleTable(bundles) {
    var body = el("tbody");
    bundles.forEach(function (b) {
      var transit = b.transit ? b.transit.minTransitDays + "–" + b.transit.maxTransitDays + " d" : "–";
      var more = b.sourceCount > b.sources.length ? " and " + num(b.sourceCount - b.sources.length) + " more" : "";
      body.appendChild(el("tr", null, [
        el("td", { text: resultIcon[b.result] + " " + b.result }),
        el("td", { text: b.kind }),
        el("td", { class: "num", text: num(b.count) }),
        el("td", { text: b.reason }),
        el("td", { text: b.sources.join(", ") + more }),
        el("td", { text: transit })
      ]));
    });
    return el("table", { class: "list" }, [
      el("thead", null, [el("tr", null, ["Result", "Source kind", "Connections", "Reason", "Sources", "Transit"].map(function (h) {
        return el("th", { text: h });
      }))]), body]);
  }

  window.addEventListener("hashchange", function () {
    drawNeighbourhood();
    if (parseHash().label && nb.scrollIntoView) nb.scrollIntoView({ block: "start" });
  });

  drawHeat();
  drawDetail();
  drawNeighbourhood();
})();
