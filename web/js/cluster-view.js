// Draws a cluster snapshot as SVG: servers, messages on the wire, clients, partitions.
// update(snapshot) once per simulated tick; frame(fraction) on every animation frame to move the
// messages smoothly between ticks.

const NS = "http://www.w3.org/2000/svg";
const BOX_W = 118, BOX_H = 60;

function el(name, attrs = {}, parent) {
  const e = document.createElementNS(NS, name);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (parent) parent.appendChild(e);
  return e;
}

export class ClusterView {
  constructor(container, opts = {}) {
    this.opts = { interactive: false, clients: true, ...opts };
    this.container = container;
    this.pickSize();
    const { width: W, height: H } = this.opts;
    this.svg = el("svg", {
      viewBox: `0 0 ${W} ${H}`,
      class: "cluster" + (this.opts.interactive ? " interactive" : ""),
      role: "img",
      "aria-label": "Live diagram of the cluster",
    });
    const defs = el("defs", {}, this.svg);
    const pat = el("pattern", { id: "hatch", width: 6, height: 6, patternUnits: "userSpaceOnUse", patternTransform: "rotate(45)" }, defs);
    el("rect", { width: 6, height: 6, fill: "var(--paper)" }, pat);
    el("line", { x1: 0, y1: 0, x2: 0, y2: 6, stroke: "var(--rule)", "stroke-width": 2 }, pat);

    this.gFaults = el("g", {}, this.svg);
    this.gClients = el("g", {}, this.svg);
    this.gNodes = el("g", {}, this.svg);
    this.gFlights = el("g", {}, this.svg);
    this.gPuffs = el("g", {}, this.svg);
    this.tickLabel = el("text", { x: 12, y: 20, class: "tick-label" }, this.svg);

    this.nodes = new Map();   // id -> {g, inner, ...}
    this.clients = new Map(); // id -> {g, ...}
    this.flights = new Map(); // id -> element
    this.puffs = [];
    this.pos = new Map();     // "n3" / "c1" -> {x, y}
    this.snap = null;
    this.marked = new Set();
    this.selected = 0;
    container.appendChild(this.svg);
    // Phones get a taller, narrower drawing so the servers stay readable.
    this.onResize = () => {
      const w = this.opts.width;
      this.pickSize();
      if (w !== this.opts.width) {
        this.svg.setAttribute("viewBox", `0 0 ${this.opts.width} ${this.opts.height}`);
        if (this.snap) this.update({ ...this.snap, events: [] });
        this.frame(0);
      }
    };
    window.addEventListener("resize", this.onResize);
  }

  pickSize() {
    const narrow = (this.container.clientWidth || window.innerWidth) < 560;
    this.opts.width = this.opts.fixedWidth || (narrow ? 420 : 640);
    this.opts.height = this.opts.fixedHeight || (narrow ? 540 : 440);
  }

  destroy() {
    window.removeEventListener("resize", this.onResize);
    this.svg.remove();
  }

  setMarked(ids) { this.marked = new Set(ids); this.paintNodes(); }
  setSelected(id) { this.selected = id; this.paintNodes(); }

  layout(snap) {
    const { width: W, height: H } = this.opts;
    const hasClients = this.opts.clients && snap.clients.length > 0;
    const bottom = hasClients ? H - 78 : H - 10;
    const top = 34;
    const groups = [...new Set(snap.nodes.map(n => n.group))].sort((a, b) => a - b);
    this.faultXs = [];
    if (groups.length <= 1) {
      const cx = W / 2, cy = (top + bottom) / 2;
      const R = Math.min(W / 2 - 80, (bottom - top) / 2 - 34);
      snap.nodes.forEach((n, i) => {
        const N = snap.nodes.length;
        if (N === 1) return this.pos.set("n" + n.id, { x: cx, y: cy });
        const a = -Math.PI / 2 + (i * 2 * Math.PI) / N;
        this.pos.set("n" + n.id, { x: cx + R * Math.cos(a), y: cy + R * Math.sin(a) * 0.92 });
      });
    } else {
      // Islands: side by side on a wide drawing, stacked on a tall one. Each island lays its
      // servers out in a small grid.
      const G = groups.length;
      const tall = H > W;
      groups.forEach((g, gi) => {
        const members = snap.nodes.filter(n => n.group === g);
        const rx = tall ? 0 : (W / G) * gi, rw = tall ? W : W / G;
        const ry = tall ? top + ((bottom - top) / G) * gi : top, rh = tall ? (bottom - top) / G : bottom - top;
        const k = members.length;
        const cols = Math.max(1, Math.min(k, Math.floor(rw / (BOX_W + 18))));
        const rows = Math.ceil(k / cols);
        const cellH = Math.min(rh / rows, BOX_H + 40);
        const y0 = ry + rh / 2 - (cellH * rows) / 2;
        members.forEach((n, i) => {
          const row = Math.floor(i / cols);
          const inRow = row === rows - 1 ? k - cols * (rows - 1) : cols;
          const cellW = Math.min(rw / inRow, BOX_W + 50);
          const x0 = rx + rw / 2 - (cellW * inRow) / 2;
          this.pos.set("n" + n.id, { x: x0 + cellW * ((i % cols) + 0.5), y: y0 + cellH * (row + 0.5) });
        });
        if (gi > 0) this.faultXs.push(tall ? { y: ry } : { x: rx });
      });
    }
    if (hasClients) {
      const n = snap.clients.length;
      const step = Math.min(118, (W - 40) / n);
      snap.clients.forEach((c, i) => {
        this.pos.set("c" + c.id, { x: 20 + step * (i + 0.5), y: H - 30 });
      });
    }
  }

  update(snap) {
    this.snap = snap;
    this.layout(snap);
    this.tickLabel.textContent = "tick " + snap.tick;
    this.drawFaults();
    this.drawNodes(snap);
    this.drawClients(snap);
    this.syncFlights(snap);
    for (const ev of snap.events || []) {
      if ((ev.kind === "lost" || ev.kind === "dropped") && ev.flight) this.addPuff(ev);
    }
  }

  drawFaults() {
    this.gFaults.replaceChildren();
    const { height: H } = this.opts;
    for (const f of this.faultXs) {
      const { width: W } = this.opts;
      if (f.x != null) {
        el("line", { x1: f.x, y1: 30, x2: f.x, y2: H - 90, class: "fault" }, this.gFaults);
        el("text", { x: f.x, y: 24, class: "fault-label", "text-anchor": "middle" }, this.gFaults).textContent = "NO LINK";
      } else {
        el("line", { x1: 16, y1: f.y, x2: W - 16, y2: f.y, class: "fault" }, this.gFaults);
        el("text", { x: W - 16, y: f.y - 5, class: "fault-label", "text-anchor": "end" }, this.gFaults).textContent = "NO LINK";
      }
    }
  }

  drawNodes(snap) {
    const seen = new Set();
    for (const n of snap.nodes) {
      seen.add(n.id);
      let v = this.nodes.get(n.id);
      if (!v) v = this.makeNode(n.id);
      const p = this.pos.get("n" + n.id);
      v.g.style.transform = `translate(${p.x}px, ${p.y}px)`;
      v.data = n;
    }
    for (const [id, v] of this.nodes) if (!seen.has(id)) { v.g.remove(); this.nodes.delete(id); }
    this.paintNodes();
  }

  makeNode(id) {
    const g = el("g", { class: "node-g" }, this.gNodes);
    const inner = el("g", { class: "node", tabindex: this.opts.interactive ? 0 : -1, role: this.opts.interactive ? "button" : "presentation" }, g);
    el("rect", { class: "box", x: -BOX_W / 2, y: -BOX_H / 2, width: BOX_W, height: BOX_H }, inner);
    const name = el("text", { class: "name", x: -BOX_W / 2 + 9, y: -BOX_H / 2 + 19 }, inner);
    name.textContent = "S" + id;
    const role = el("text", { class: "role", x: BOX_W / 2 - 8, y: -BOX_H / 2 + 17, "text-anchor": "end" }, inner);
    const term = el("text", { class: "term", x: -BOX_W / 2 + 9, y: 9 }, inner);
    const vote = el("text", { class: "vote", x: BOX_W / 2 - 8, y: 9, "text-anchor": "end" }, inner);
    el("rect", { class: "timer-bg", x: -BOX_W / 2 + 9, y: 17, width: BOX_W - 18, height: 4 }, inner);
    const timer = el("rect", { class: "timer", x: -BOX_W / 2 + 9, y: 17, width: 0, height: 4 }, inner);
    el("path", { class: "mark", d: `M${BOX_W / 2 - 14} ${-BOX_H / 2} h14 v14 z` }, inner);
    const title = el("title", {}, inner);
    if (this.opts.interactive) {
      const fire = () => this.opts.onNodeClick && this.opts.onNodeClick(id);
      inner.addEventListener("click", fire);
      inner.addEventListener("keydown", e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fire(); } });
    }
    const v = { g, inner, role, term, vote, timer, title, data: null };
    this.nodes.set(id, v);
    return v;
  }

  paintNodes() {
    for (const [id, v] of this.nodes) {
      const n = v.data;
      if (!n) continue;
      const state = n.up ? n.state : "down";
      v.inner.setAttribute("class", `node ${state}` + (this.selected === id ? " selected" : "") + (this.marked.has(id) ? " marked" : ""));
      v.role.textContent = state.toUpperCase();
      v.term.textContent = "term " + n.term;
      v.vote.textContent = n.up && n.state === "follower" && n.vote ? "voted S" + n.vote : "";
      const frac = n.election_timeout ? Math.min(1, n.election_elapsed / n.election_timeout) : 0;
      v.timer.setAttribute("width", ((BOX_W - 18) * frac).toFixed(1));
      v.title.textContent = n.up
        ? `S${id}: ${n.state}, term ${n.term}, log ${n.last_index}, committed ${n.commit}` + (this.opts.interactive ? ". Click for actions." : "")
        : `S${id}: down` + (this.opts.interactive ? ". Click to restart." : "");
      v.inner.setAttribute("aria-label", v.title.textContent);
    }
  }

  drawClients(snap) {
    const seen = new Set();
    if (this.opts.clients) {
      for (const c of snap.clients) {
        seen.add(c.id);
        let v = this.clients.get(c.id);
        if (!v) {
          const g = el("g", { class: "client" }, this.gClients);
          el("rect", { x: -48, y: -17, width: 96, height: 34 }, g);
          const name = el("text", { x: -42, y: -3 }, g);
          const cmd = el("text", { x: -42, y: 11, class: "cmd" }, g);
          v = { g, name, cmd };
          this.clients.set(c.id, v);
        }
        const p = this.pos.get("c" + c.id);
        v.g.setAttribute("transform", `translate(${p.x},${p.y})`);
        v.g.setAttribute("class", "client" + (c.busy ? " busy" : ""));
        v.name.textContent = `C${c.id}` + (c.busy ? " → S" + c.target : "");
        const text = c.command || "idle";
        v.cmd.textContent = text.length > 14 ? text.slice(0, 13) + "…" : text;
      }
    }
    for (const [id, v] of this.clients) if (!seen.has(id)) { v.g.remove(); this.clients.delete(id); }
  }

  endpoints(f) {
    const from = f.kind === "request" ? this.pos.get("c" + f.client) : this.pos.get("n" + f.from);
    const to = f.kind === "reply" ? this.pos.get("c" + f.client) : this.pos.get("n" + f.to);
    return [from, to];
  }

  syncFlights(snap) {
    const seen = new Set();
    for (const f of snap.flights) {
      seen.add(f.id);
      let e = this.flights.get(f.id);
      if (!e) {
        e = this.makeFlight(f);
        if (!e) continue;
        this.flights.set(f.id, e);
      }
      e.flight = f;
    }
    for (const [id, e] of this.flights) if (!seen.has(id)) { e.remove(); this.flights.delete(id); }
  }

  makeFlight(f) {
    const [a, b] = this.endpoints(f);
    if (!a || !b) return null;
    const g = el("g", {}, this.gFlights);
    switch (f.kind) {
      case "vote": el("circle", { r: 5.5, class: "vote-msg" }, g); break;
      case "vote-resp": el("circle", { r: 4.5, class: "vote-msg" + (f.reject ? " hollow" : "") }, g); break;
      case "append":
        if (f.entries > 0) {
          el("rect", { x: -5, y: -5, width: 10, height: 10, class: "app-msg" }, g);
          const t = el("text", { x: 8, y: 3, class: "msg-count" }, g);
          t.textContent = "+" + f.entries;
        } else {
          el("rect", { x: -3.5, y: -3.5, width: 7, height: 7, class: "app-msg heartbeat" }, g);
        }
        break;
      case "append-resp": el("rect", { x: -3, y: -3, width: 6, height: 6, class: "app-msg" + (f.reject ? " hollow" : "") }, g); break;
      case "request": el("path", { d: "M0 -6 L6 0 L0 6 L-6 0 Z", class: "client-msg" }, g); break;
      case "reply": el("path", { d: "M0 -5 L5 0 L0 5 L-5 0 Z", class: "client-msg" + (f.not_leader ? " hollow" : "") }, g); break;
    }
    const t = el("title", {}, g);
    t.textContent = describeFlight(f);
    g.flight = f;
    return g;
  }

  // The straight path a message takes, from the sender's box edge to the receiver's.
  path(f) {
    const [a, b] = this.endpoints(f);
    if (!a || !b) return null;
    const dx = b.x - a.x, dy = b.y - a.y;
    const len = Math.hypot(dx, dy) || 1;
    const ux = dx / len, uy = dy / len;
    const edge = (hw, hh) => Math.min(hw / Math.max(Math.abs(ux), 1e-6), hh / Math.max(Math.abs(uy), 1e-6)) + 5;
    const ea = Math.min(len / 2 - 2, f.kind === "request" ? edge(48, 17) : edge(BOX_W / 2, BOX_H / 2));
    const eb = Math.min(len / 2 - 2, f.kind === "reply" ? edge(48, 17) : edge(BOX_W / 2, BOX_H / 2));
    // Offset to the right of the direction of travel, so a request and its reply don't overlap.
    const ox = -uy * 6, oy = ux * 6;
    return { sx: a.x + ux * ea + ox, sy: a.y + uy * ea + oy, ex: b.x - ux * eb + ox, ey: b.y - uy * eb + oy };
  }

  // Where a message is at time (tick + frac).
  place(f, frac) {
    const q = this.path(f);
    if (!q || !this.snap) return null;
    const span = Math.max(1, f.deliver_at - f.sent_at);
    const p = Math.max(0, Math.min(1, (this.snap.tick + frac - f.sent_at) / span));
    return { x: q.sx + (q.ex - q.sx) * p, y: q.sy + (q.ey - q.sy) * p };
  }

  frame(frac) {
    for (const [, g] of this.flights) {
      const q = this.place(g.flight, frac);
      if (q) g.setAttribute("transform", `translate(${q.x.toFixed(1)},${q.y.toFixed(1)})`);
    }
    const now = performance.now();
    this.puffs = this.puffs.filter(p => {
      const age = (now - p.born) / 700;
      if (age >= 1) { p.g.remove(); return false; }
      p.g.setAttribute("opacity", (1 - age).toFixed(2));
      p.g.setAttribute("transform", `translate(${p.x},${p.y}) scale(${1 + age * 0.6})`);
      return true;
    });
  }

  addPuff(ev) {
    if (this.puffs.length > 40) return;
    const q = this.path(ev.flight);
    if (!q) return;
    // Lost on the way: show it leaving the sender. Dropped: show it at the receiver's door.
    const t = ev.kind === "lost" ? 0.08 : 0.94;
    const x = q.sx + (q.ex - q.sx) * t, y = q.sy + (q.ey - q.sy) * t;
    const g = el("g", { class: "puff" }, this.gPuffs);
    el("path", { d: "M-4 -4 L4 4 M4 -4 L-4 4" }, g);
    this.puffs.push({ g, x, y, born: performance.now() });
  }
}

export function describeFlight(f) {
  const who = f.kind === "request" ? `C${f.client} → S${f.to}` : f.kind === "reply" ? `S${f.from} → C${f.client}` : `S${f.from} → S${f.to}`;
  switch (f.kind) {
    case "vote": return `${who}: vote for me in term ${f.term}? (my last entry: #${f.index || 0}, term ${f.log_term || 0})`;
    case "vote-resp": return `${who}: ${f.reject ? "no" : "yes"}, term ${f.term}`;
    case "append": return f.entries ? `${who}: append ${f.entries} entr${f.entries === 1 ? "y" : "ies"} after #${f.index || 0} (term ${f.term}, commit ${f.commit || 0})` : `${who}: heartbeat, term ${f.term}, commit ${f.commit || 0}`;
    case "append-resp": return `${who}: ${f.reject ? "rejected (logs differ)" : "stored up to #" + (f.index || 0)}`;
    case "request": return `${who}: ${f.command}`;
    case "reply": return f.not_leader ? `${who}: not the leader` : `${who}: ${f.result}`;
  }
  return who;
}
