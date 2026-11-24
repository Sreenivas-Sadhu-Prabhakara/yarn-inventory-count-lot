/* Yarn store ledger UI. Talks only to this binary's own /api endpoints on the
   same origin. No third-party code, no outbound requests, no inline handlers. */
(function () {
  "use strict";

  /* ---- tiny DOM helpers ------------------------------------------------- */

  function el(sel, root) { return (root || document).querySelector(sel); }
  function els(sel, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(sel));
  }

  /* The shared stylesheet gives every .note one look, so the tone of a message
     is carried in its words rather than only in a colour. That is also the
     accessible way round: a screen reader on the role="status" region hears the
     same distinction a sighted user sees. */
  var tonePrefix = { bad: "Problem — ", warn: "Check — " };

  function note(node, msg, tone) {
    if (!node) return;
    node.textContent = msg ? (tonePrefix[tone] || "") + msg : "";
    node.hidden = !msg;
    node.setAttribute("data-tone", tone || "info");
  }

  function clearNode(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
  }

  /* cell builds one table cell. Text is always set through textContent, never
     as markup, so a shade or supplier name can never be read as HTML. */
  function cell(value, opts) {
    var o = opts || {};
    var td = document.createElement(o.head ? "th" : "td");
    if (o.num) td.className = "num";
    td.appendChild(document.createTextNode(value === null || value === undefined ? "" : String(value)));
    if (o.hint) {
      var s = document.createElement("span");
      s.className = "hint";
      s.textContent = " " + o.hint;
      td.appendChild(s);
    }
    return td;
  }

  function rowOf(cells) {
    var tr = document.createElement("tr");
    for (var i = 0; i < cells.length; i++) tr.appendChild(cells[i]);
    return tr;
  }

  /* toggleEmpty shows a table's "nothing here yet" note only when it is empty. */
  function toggleEmpty(emptyNote, count) {
    if (emptyNote) emptyNote.hidden = count > 0;
  }

  function kg(q) { return (q && q.kg ? q.kg : "0.000") + " kg"; }

  function today() {
    var d = new Date();
    function p(n) { return (n < 10 ? "0" : "") + n; }
    return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate());
  }

  /* ---- API -------------------------------------------------------------- */

  function api(path, opts) {
    return fetch(path, opts).then(function (res) {
      return res.text().then(function (raw) {
        var body = null;
        if (raw) { try { body = JSON.parse(raw); } catch (e) { body = null; } }
        if (!res.ok) {
          var msg = (body && body.error) || ("the request failed (" + res.status + ")");
          var err = new Error(msg);
          err.body = body;
          throw err;
        }
        return body;
      });
    });
  }

  function postJSON(path, payload) {
    return api(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
  }

  function formObject(form) {
    var out = {};
    els("input, select", form).forEach(function (input) {
      if (input.name) out[input.name] = input.value;
    });
    return out;
  }

  /* ---- theme ------------------------------------------------------------ */

  function initTheme() {
    var root = document.documentElement;
    function stored() { try { return localStorage.getItem("omsme-theme"); } catch (e) { return null; } }
    function current() {
      var t = root.getAttribute("data-theme");
      if (t) return t;
      return (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
    }
    var btn = el("[data-themer]");
    if (!btn) return;
    function paint() {
      var dark = current() === "dark";
      btn.textContent = dark ? "☀ Light" : "☾ Dark";
      btn.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
    }
    paint();
    btn.addEventListener("click", function () {
      var next = current() === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("omsme-theme", next); } catch (e) { /* private window */ }
      paint();
    });
    if (window.matchMedia) {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
        if (!stored()) paint();
      });
    }
  }

  /* ---- tabs ------------------------------------------------------------- */

  function initTabs() {
    var nav = el("[data-tabs]");
    if (!nav) return;
    var buttons = els("button[data-tab]", nav);
    buttons.forEach(function (btn) {
      btn.addEventListener("click", function () {
        var want = btn.getAttribute("data-tab");
        buttons.forEach(function (b) {
          b.setAttribute("aria-pressed", String(b === btn));
        });
        els("[data-panel]").forEach(function (panel) {
          panel.hidden = panel.getAttribute("data-panel") !== want;
        });
      });
    });
  }

  /* ---- state rendering -------------------------------------------------- */

  var state = null;

  function renderGroups() {
    var body = el("[data-groups]");
    clearNode(body);
    state.groups.forEach(function (gr) {
      body.appendChild(rowOf([
        cell(gr.count),
        cell(gr.shade),
        cell(String(gr.openLots), { num: true }),
        cell(kg(gr.balance), { num: true })
      ]));
    });
    toggleEmpty(el("[data-groups-empty]"), state.groups.length);
  }

  function renderLots() {
    var body = el("[data-lots]");
    clearNode(body);
    state.lots.forEach(function (lot) {
      var age = lot.ageDays === null || lot.ageDays === undefined ? "—" : lot.ageDays + " d";
      body.appendChild(rowOf([
        cell(lot.key.count + " " + lot.key.shade),
        cell(lot.key.dyeLot, { hint: lot.aged ? "aged" : "" }),
        cell(lot.firstReceived || "—"),
        cell(age, { num: true }),
        cell(kg(lot.balance), { num: true })
      ]));
    });
    toggleEmpty(el("[data-lots-empty]"), state.lots.length);
  }

  var registerLimit = 200;

  function renderRegister() {
    var body = el("[data-register]");
    clearNode(body);
    state.register.slice(0, registerLimit).forEach(function (t) {
      body.appendChild(rowOf([
        cell(t.date),
        cell(t.ref || "—"),
        cell(t.key.count + " " + t.key.shade),
        cell(t.key.dyeLot),
        cell(kg(t.qty), { num: true })
      ]));
    });
    if (state.register.length > registerLimit) {
      body.appendChild(rowOf([
        cell("Showing the latest " + registerLimit + " of " + state.register.length +
          " issues. Export the register for the full history.")
      ]));
    }
    toggleEmpty(el("[data-register-empty]"), state.register.length);
  }

  function renderAlerts() {
    var reorder = el("[data-reorder]");
    clearNode(reorder);
    state.alerts.reorder.forEach(function (a) {
      reorder.appendChild(rowOf([
        cell(a.count + " " + a.shade, { hint: a.openLots > 1 ? a.openLots + " open lots" : "" }),
        cell(kg(a.onHand), { num: true }),
        cell(kg(a.level), { num: true }),
        cell(kg(a.suggestedOrder), { num: true, hint: a.targetDerived ? "to 2x level" : "" })
      ]));
    });
    toggleEmpty(el("[data-reorder-empty]"), state.alerts.reorder.length);

    var aged = el("[data-aged]");
    clearNode(aged);
    state.alerts.agedLots.forEach(function (a) {
      aged.appendChild(rowOf([
        cell(a.key.count + " " + a.key.shade),
        cell(a.key.dyeLot),
        cell(a.firstReceived),
        cell(a.ageDays + " d", { num: true }),
        cell(kg(a.balance), { num: true })
      ]));
    });
    toggleEmpty(el("[data-aged-empty]"), state.alerts.agedLots.length);
  }

  function renderRules() {
    var body = el("[data-rules]");
    clearNode(body);
    state.rules.forEach(function (r) {
      var btn = document.createElement("button");
      btn.className = "btn ghost";
      btn.type = "button";
      btn.textContent = "Remove";
      btn.addEventListener("click", function () {
        postJSON("/api/rules", { count: r.count, shade: r.shade, delete: true })
          .then(reload)
          .catch(function (err) { note(el("[data-status='rule']"), err.message, "bad"); });
      });
      var td = document.createElement("td");
      td.className = "num";
      td.appendChild(btn);

      body.appendChild(rowOf([
        cell(r.count),
        cell(r.shade),
        cell(kg(r.onHand), { num: true }),
        cell(kg(r.level), { num: true }),
        cell(kg(r.orderUpTo), { num: true }),
        td
      ]));
    });
    toggleEmpty(el("[data-rules-empty]"), state.rules.length);
  }

  function renderPickLists() {
    [["counts", state.counts], ["shades", state.shades], ["suppliers", state.suppliers]]
      .forEach(function (pair) {
        var list = document.getElementById(pair[0]);
        if (!list) return;
        clearNode(list);
        pair[1].forEach(function (v) {
          var opt = document.createElement("option");
          opt.value = v;
          list.appendChild(opt);
        });
      });
  }

  function renderState() {
    el("[data-total]").textContent = kg(state.totalBalance);
    var openLots = state.lots.filter(function (l) { return l.balance.g > 0; }).length;
    el("[data-total-sub]").textContent = state.lots.length === 0
      ? "No stock recorded yet."
      : openLots + " open dye-lot" + (openLots === 1 ? "" : "s") +
        " across " + state.groups.length + " count and shade combination" + (state.groups.length === 1 ? "" : "s") +
        " · " + state.movements + " movement" + (state.movements === 1 ? "" : "s") + " on record";

    renderGroups();
    renderLots();
    renderRegister();
    renderAlerts();
    renderRules();
    renderPickLists();

    var agedInput = el("#s-aged");
    if (agedInput) agedInput.value = String(state.agedAfterDays);
    var file = el("[data-datafile]");
    if (file) file.textContent = state.dataFile;
  }

  function reload() {
    return api("/api/state").then(function (s) {
      state = s;
      renderState();
      note(el("[data-global-status]"), "", "info");
    }).catch(function (err) {
      note(el("[data-global-status]"), "Could not read the ledger: " + err.message, "bad");
    });
  }

  /* ---- receive and adjust ----------------------------------------------- */

  function bindPost(formName, path, onDone) {
    var form = el("[data-form='" + formName + "']");
    if (!form) return;
    var status = el("[data-status='" + formName + "']");
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      note(status, "Working…", "info");
      postJSON(path, formObject(form))
        .then(function (res) {
          onDone(res, form, status);
          return reload();
        })
        .catch(function (err) { note(status, err.message, "bad"); });
    });
  }

  function initReceive() {
    bindPost("receive", "/api/receive", function (res, form, status) {
      note(status, "Booked " + kg(res.qty) + " of " + res.key.count + " " + res.key.shade +
        ", dye-lot " + res.key.dyeLot + ", dated " + res.date + ".", "ok");
      el("#r-lot").value = "";
      el("#r-kg").value = "";
      el("#r-ref").value = "";
    });
    bindPost("adjust", "/api/adjust", function (res, form, status) {
      note(status, "Adjusted dye-lot " + res.key.dyeLot + " by " + kg(res.qty) + " on " + res.date + ".", "ok");
      el("#a-kg").value = "";
    });
  }

  /* ---- issue ------------------------------------------------------------ */

  function initIssue() {
    var form = el("[data-form='issue']");
    var status = el("[data-status='issue']");
    var commit = el("[data-commit]");
    var body = el("[data-alloc]");
    var total = el("[data-alloc-total]");
    var sub = el("[data-alloc-sub]");
    var warn = el("[data-alloc-warn]");
    if (!form) return;

    var planned = null;

    function resetPlan(message) {
      planned = null;
      commit.disabled = true;
      clearNode(body);
      total.textContent = "0.000 kg";
      sub.textContent = message || "Plan an allocation to see which lots the yarn comes off.";
      note(warn, "", "info");
    }

    function draw(res) {
      clearNode(body);
      res.allocations.forEach(function (a) {
        body.appendChild(rowOf([
          cell(a.key.dyeLot, { hint: a.exhausts ? "emptied" : "" }),
          cell(a.firstReceived || "—"),
          cell(kg(a.qty), { num: true }),
          cell(kg(a.remaining), { num: true })
        ]));
      });
      total.textContent = kg(res.total);
      sub.textContent = res.lots === 1
        ? "All from one dye-lot, so no shade join in this run."
        : "Spread across " + res.lots + " dye-lots. The cloth will show a shade join at each change-over.";
      if (res.lots > 1) {
        note(warn, "This run crosses " + (res.lots - 1) + " dye-lot change" +
          (res.lots === 2 ? "" : "s") + ". Re-sample the shade at each join before continuing.", "warn");
      } else {
        note(warn, "", "info");
      }
    }

    els("input", form).forEach(function (input) {
      input.addEventListener("input", function () { resetPlan(); });
    });

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var payload = formObject(form);
      payload.commit = false;
      note(status, "Planning…", "info");
      api("/api/issue", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      }).then(function (res) {
        draw(res);
        planned = payload;
        commit.disabled = false;
        note(status, "Nothing written yet. Check the allocation, then confirm.", "info");
      }).catch(function (err) {
        resetPlan("Could not allocate this issue.");
        var extra = "";
        if (err.body && err.body.shortfall) {
          extra = " Short by " + err.body.shortfall.kg + " kg (only " +
            err.body.available.kg + " kg on hand).";
        }
        note(status, err.message + extra, "bad");
      });
    });

    commit.addEventListener("click", function () {
      if (!planned) return;
      var payload = {};
      Object.keys(planned).forEach(function (k) { payload[k] = planned[k]; });
      payload.commit = true;
      commit.disabled = true;
      note(status, "Posting…", "info");
      postJSON("/api/issue", payload).then(function (res) {
        draw(res);
        note(status, "Issued " + kg(res.total) + " across " + res.lots +
          " dye-lot" + (res.lots === 1 ? "" : "s") + ". The register now carries every line.", "ok");
        planned = null;
        el("#i-kg").value = "";
        return reload();
      }).catch(function (err) {
        note(status, err.message, "bad");
      });
    });

    resetPlan();
  }

  /* ---- count conversion ------------------------------------------------- */

  var systemHints = {
    Ne: "English cotton count, 840-yard hanks per pound",
    Nm: "metric count, metres per gram",
    tex: "grams per 1000 metres",
    denier: "grams per 9000 metres"
  };

  function initConvert() {
    var form = el("[data-form='convert']");
    if (!form) return;
    var status = el("[data-status='convert']");
    var seg = el("[data-systems]");
    var buttons = els("button[data-system]", seg);
    var valueField = el("[data-value-field]");
    var countField = el("[data-count-field]");
    var hint = el("[data-value-hint]");
    var plyNote = el("[data-ply-note]");
    var system = "Ne";

    function pick(next) {
      system = next;
      buttons.forEach(function (b) {
        b.setAttribute("aria-pressed", String(b.getAttribute("data-system") === next));
      });
      var ply = next === "count";
      valueField.hidden = ply;
      countField.hidden = !ply;
      if (!ply) hint.textContent = systemHints[next] || "";
    }

    buttons.forEach(function (b) {
      b.addEventListener("click", function () { pick(b.getAttribute("data-system")); });
    });

    function show(res) {
      el("[data-out-ne]").textContent = res.ne;
      el("[data-out-nm]").textContent = res.nm;
      el("[data-out-tex]").textContent = res.tex;
      el("[data-out-denier]").textContent = res.denier;
      if (res.system === "count") {
        var msg = res.folded
          ? "Read as " + res.plies + " plies of " + res.singles + "s. Each singles thread is " +
            res.singlesTex + " tex, so the resultant is " + res.tex + " tex and Ne " + res.ne + "."
          : "Read as a singles " + res.singles + "s.";
        note(plyNote, msg, "info");
      } else {
        note(plyNote, "", "info");
      }
    }

    function blank() {
      ["ne", "nm", "tex", "denier"].forEach(function (k) {
        el("[data-out-" + k + "]").textContent = "—";
      });
      note(plyNote, "", "info");
    }

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var url;
      if (system === "count") {
        url = "/api/convert?count=" + encodeURIComponent(el("#c-count").value);
      } else {
        url = "/api/convert?system=" + encodeURIComponent(system) +
          "&value=" + encodeURIComponent(el("#c-value").value);
      }
      api(url).then(function (res) {
        show(res);
        note(status, "", "info");
      }).catch(function (err) {
        blank();
        note(status, err.message, "bad");
      });
    });

    pick("Ne");
  }

  /* ---- alerts, rules, settings ------------------------------------------ */

  function initRulesAndSettings() {
    bindPost("rule", "/api/rules", function (res, form, status) {
      note(status, "Reorder level saved.", "ok");
      el("#k-level").value = "";
      el("#k-target").value = "";
    });

    var form = el("[data-form='settings']");
    if (!form) return;
    var status = el("[data-status='settings']");
    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var days = parseInt(el("#s-aged").value, 10);
      if (!(days >= 1 && days <= 3650)) {
        note(status, "The threshold must be a whole number of days between 1 and 3650.", "bad");
        return;
      }
      postJSON("/api/settings", { agedAfterDays: days }).then(function () {
        note(status, "A lot will now be flagged once it is more than " + days + " days old.", "ok");
        return reload();
      }).catch(function (err) { note(status, err.message, "bad"); });
    });
  }

  /* ---- consumption variance --------------------------------------------- */

  function initConsumption() {
    var form = el("[data-form='consumption']");
    if (!form) return;
    var status = el("[data-status='consumption']");
    var table = el("[data-variance-table]");

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      postJSON("/api/consumption", formObject(form)).then(function (v) {
        el("[data-v-expected]").textContent = kg(v.expected);
        el("[data-v-actual]").textContent = kg(v.actual);
        el("[data-v-variance]").textContent = kg(v.variance);
        el("[data-v-over]").textContent = v.overStandardDefined
          ? v.overStandardPct + " %" : "not meaningful (no standard)";
        el("[data-v-wastage]").textContent = v.wastageDefined
          ? v.wastagePct + " %" : "not meaningful (nothing issued)";
        table.hidden = false;
        note(status, v.over
          ? "That run drew more yarn than its standard over " + v.from + " to " + v.to + "."
          : "That run was at or under its standard over " + v.from + " to " + v.to + ".",
          v.over ? "warn" : "ok");
      }).catch(function (err) {
        table.hidden = true;
        note(status, err.message, "bad");
      });
    });
  }

  /* ---- CSV import ------------------------------------------------------- */

  function initImport() {
    var file = el("#d-file");
    var btn = el("[data-import]");
    if (!file || !btn) return;
    var status = el("[data-status='import']");
    var table = el("[data-import-table]");
    var rows = el("[data-import-rows]");

    file.addEventListener("change", function () {
      btn.disabled = !file.files || file.files.length === 0;
      note(status, "", "info");
      table.hidden = true;
    });

    btn.addEventListener("click", function () {
      if (!file.files || !file.files.length) return;
      btn.disabled = true;
      note(status, "Reading the file…", "info");
      var reader = new FileReader();
      reader.onerror = function () {
        btn.disabled = false;
        note(status, "Could not read that file.", "bad");
      };
      reader.onload = function () {
        api("/api/import/receipts", {
          method: "POST",
          headers: { "Content-Type": "text/csv" },
          body: String(reader.result)
        }).then(function (rep) {
          var bad = (rep.unreadable || []).concat(rep.skipped || []);
          clearNode(rows);
          bad.forEach(function (b) {
            rows.appendChild(rowOf([cell(String(b.line), { num: true }), cell(b.message)]));
          });
          table.hidden = bad.length === 0;
          var msg = "Imported " + rep.imported + " of " + rep.rows + " row" + (rep.rows === 1 ? "" : "s") + ".";
          if (bad.length) msg += " " + bad.length + " row" + (bad.length === 1 ? " was" : "s were") + " skipped, listed below.";
          note(status, msg, bad.length ? "warn" : "ok");
          file.value = "";
          btn.disabled = true;
          return reload();
        }).catch(function (err) {
          btn.disabled = false;
          table.hidden = true;
          note(status, err.message, "bad");
        });
      };
      reader.readAsText(file.files[0]);
    });
  }

  /* ---- start ------------------------------------------------------------ */

  function initDates() {
    var t = today();
    ["#r-date", "#a-date", "#i-date"].forEach(function (sel) {
      var node = el(sel);
      if (node && !node.value) node.value = t;
    });
    var from = el("#v-from");
    var to = el("#v-to");
    if (to && !to.value) to.value = t;
    if (from && !from.value) from.value = t.slice(0, 8) + "01";
  }

  initTheme();
  initTabs();
  initDates();
  initReceive();
  initIssue();
  initConvert();
  initRulesAndSettings();
  initConsumption();
  initImport();
  reload();
})();
