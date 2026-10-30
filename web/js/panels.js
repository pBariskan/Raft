// Smaller views shared by the Workplace and the Course: logs, key-value stores, the event feed.

export function h(tag, attrs = {}, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else e.setAttribute(k, v === true ? "" : v);
  }
  for (const k of kids.flat()) if (k != null && k !== false) e.append(k instanceof Node ? k : String(k));
  return e;
}

// Logs: one row per server, columns aligned by log index, tinted by term. Committed entries are
// solid, uncommitted ones hatched. Shows at most `cols` indexes, ending at the longest log.
export function renderLogs(container, snap, cols = 18) {
  const last = Math.max(0, ...snap.nodes.map(n => n.last_index));
  const first = Math.max(1, last - cols + 1);
  const n = last - first + 1;
  const grid = h("div", { class: "log-grid", role: "table", "aria-label": "Each server's log" });
  grid.style.gridTemplateColumns = `auto ${first > 1 ? "14px " : ""}repeat(${Math.max(n, 1)}, 30px)`;
  grid.append(h("div", { class: "hd", role: "columnheader" }, "index"));
  if (first > 1) grid.append(h("div", { class: "hd" }, ""));
  for (let i = first; i <= last; i++) grid.append(h("div", { class: "hd", role: "columnheader" }, i));
  if (n === 0) grid.append(h("div", { class: "hd" }, "empty"));
  for (const node of snap.nodes) {
    const byIndex = new Map(node.log.map(e => [e.index, e]));
    const lead = snap.leader === node.id ? " (leader)" : "";
    grid.append(h("div", { class: "who" + (node.up ? "" : " down"), role: "rowheader" }, `S${node.id}`, h("span", { class: "muted small" }, node.up ? lead : "down")));
    if (first > 1) grid.append(h("div", { class: "ellipsis" }, "…"));
    if (n === 0) grid.append(h("div", { role: "cell" }));
    for (let i = first; i <= last; i++) {
      const e = byIndex.get(i);
      if (!e) {
        grid.append(h("div", { class: "cell empty", role: "cell" }));
        continue;
      }
      const committed = i <= node.commit;
      const cls = `cell term-${e.term % 8}` + (committed ? "" : " unc") + (e.command === "no-op" ? " noop" : " data");
      grid.append(h("div", {
        class: cls, role: "cell",
        title: `S${node.id} #${e.index}, term ${e.term}: ${e.command}${committed ? " (committed)" : " (not committed yet)"}`,
      }, e.term));
    }
  }
  container.replaceChildren(grid);
}

// Key-value stores side by side: what each server's state machine holds.
export function renderStores(container, snap) {
  const keys = [...new Set(snap.nodes.flatMap(n => Object.keys(n.store)))].sort();
  if (keys.length === 0) {
    container.replaceChildren(h("p", { class: "muted small" }, "Every store is empty. Send a PUT."));
    return;
  }
  const table = h("table", { class: "data" },
    h("thead", {}, h("tr", {}, h("th", {}, "key"), ...snap.nodes.map(n => h("th", {}, `S${n.id}` + (n.up ? "" : " ×"))))),
    h("tbody", {}, ...keys.map(k => h("tr", {}, h("td", {}, k), ...snap.nodes.map(n => {
      const v = n.store[k];
      return h("td", { class: n.up ? "" : "muted", title: v ?? "" }, v == null ? "—" : v.length > 12 ? v.slice(0, 11) + "…" : v);
    })))));
  container.replaceChildren(h("div", { class: "table-scroll" }, table));
}

// Turns an engine event into a feed line, or null for events not worth a line.
export function describeEvent(ev, snap, opts = {}) {
  const S = id => "S" + id;
  switch (ev.kind) {
    case "role":
      if (ev.text === "leader") return { cls: "ev-role", html: `<b>${S(ev.node)} is leader</b> of term ${ev.term}` };
      if (ev.text === "candidate") return { cls: "ev-role", html: `${S(ev.node)} timed out → candidate for term ${ev.term}` };
      return opts.verbose ? { cls: "ev-role", html: `${S(ev.node)} is a follower in term ${ev.term}` } : null;
    case "commit":
      if (!opts.verbose && snap && snap.nodes.find(n => n.id === ev.node)?.state !== "leader") return null;
      return { cls: "ev-commit", html: `${S(ev.node)} commits up to #${ev.index}` };
    case "crash": return { cls: "ev-fault", html: `<b>${S(ev.node)} crashed</b>` };
    case "restart": return { cls: "ev-fault", html: `${S(ev.node)} restarts from its disk` };
    case "partition": return { cls: "ev-fault", html: `network split: ${esc(ev.text)}` };
    case "heal": return { cls: "ev-fault", html: "network healed" };
    case "network": return { cls: "ev-fault", html: esc(ev.text) };
    case "call": return { cls: "ev-call", html: `C${ev.client} sends ${esc(ev.text)}` };
    case "retry": return { cls: "ev-call", html: `C${ev.client} heard nothing, tries ${S(ev.node)}` };
    case "redirect": return { cls: "ev-call", html: `C${ev.client} told to try ${S(ev.node)}` };
    case "return": return { cls: "ev-return", html: `C${ev.client} ← ${esc(ev.text)} (from ${S(ev.node)})` };
    case "duplicate": return { cls: "ev-commit", html: `${S(ev.node)} skips duplicate ${esc(ev.text)}` };
    case "violation": return { cls: "ev-violation", html: `SAFETY CHECK FAILED: ${esc(ev.text)}` };
    case "lost":
    case "dropped":
      if (!opts.verbose) return null;
      return { cls: "ev-" + ev.kind, html: `${ev.kind === "lost" ? "lost" : "dropped"}: ${esc(ev.flight.kind)} ${ev.flight.from ? S(ev.flight.from) : "C" + ev.flight.client}→${ev.flight.to ? S(ev.flight.to) : "C" + ev.flight.client}` };
  }
  return null;
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
}

export class Feed {
  constructor(container, opts = {}) {
    this.el = container;
    this.opts = { max: 250, verbose: false, ...opts };
  }
  clear() { this.el.replaceChildren(); }
  add(events, snap) {
    const atBottom = this.el.scrollTop + this.el.clientHeight >= this.el.scrollHeight - 8;
    for (const ev of events) {
      const d = describeEvent(ev, snap, this.opts);
      if (!d) continue;
      const line = document.createElement("div");
      line.className = d.cls;
      line.innerHTML = `<span class="t">${ev.tick}</span><span>${d.html}</span>`;
      this.el.append(line);
    }
    while (this.el.childElementCount > this.opts.max) this.el.firstElementChild.remove();
    if (atBottom) this.el.scrollTop = this.el.scrollHeight;
  }
}

// Theme toggle shared by every page (stored per browser; the page works without storage).
export function setupTheme() {
  const btn = document.querySelector(".theme-toggle");
  const get = () => { try { return localStorage.getItem("theme"); } catch { return null; } };
  const apply = t => {
    if (t) document.documentElement.dataset.theme = t; else delete document.documentElement.dataset.theme;
    if (btn) btn.textContent = (t || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")) === "dark" ? "light" : "dark";
  };
  apply(get());
  btn?.addEventListener("click", () => {
    const cur = document.documentElement.dataset.theme || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    const next = cur === "dark" ? "light" : "dark";
    try { localStorage.setItem("theme", next); } catch {}
    apply(next);
  });
}
