// Draws a cluster snapshot as a raft of logs. Each server is a wooden log, its entries are segments
// along it (aligned by log index, so the logs can be compared), and servers that can reach each
// other are lashed together with rope. The leader flies a flag with its term; a crashed server's
// log sinks; a network split cuts the raft into pieces with open water between. Messages travel on
// arcs between logs; clients are boats.
// update(snapshot) once per simulated tick; frame(fraction) on every animation frame to move the
// messages smoothly between ticks.

const NS = "http://www.w3.org/2000/svg";
const LOG_H = 30;     // thickness of a log
const PITCH = 60;     // vertical distance between logs (log + status line + gap)
const SPLIT = 44;     // open water between two parts of a split raft

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
    this.nodeCount = 0;
    this.pickSize(5);
    const { width: W, height: H } = this.opts;
    this.svg = el("svg", {
      viewBox: `0 0 ${W} ${H}`,
      class: "cluster raft" + (this.opts.interactive ? " interactive" : ""),
      role: "img",
      "aria-label": "Live diagram of the cluster: each server is a log in a raft",
    });
    const defs = el("defs", {}, this.svg);
    const pat = el("pattern", { id: "hatch", width: 5, height: 5, patternUnits: "userSpaceOnUse", patternTransform: "rotate(45)" }, defs);
    el("line", { x1: 0, y1: 0, x2: 0, y2: 5, class: "hatch-line" }, pat);
    this.water = el("rect", { class: "water", x: 0, y: 0, width: W, height: H }, this.svg);
    this.gWaves = el("g", { class: "waves" }, this.svg);
    this.gFaults = el("g", {}, this.svg);
    this.gRopes = el("g", {}, this.svg);
    this.gNodes = el("g", {}, this.svg);
    this.gClients = el("g", {}, this.svg);
    this.gTrails = el("g", {}, this.svg);
    this.gFlights = el("g", {}, this.svg);
    this.gPuffs = el("g", {}, this.svg);
    this.tickLabel = el("text", { x: 12, y: 20, class: "tick-label" }, this.svg);

    this.nodes = new Map();   // id -> {g, ...}
    this.clients = new Map(); // id -> {g, ...}
    this.flights = new Map(); // id -> element
    this.puffs = [];
    this.pos = new Map();     // "n3" / "c1" -> {x, y}
    this.snap = null;
    this.marked = new Set();
    this.selected = 0;
    container.appendChild(this.svg);
    this.drawWaves();
    // Phones get a narrower drawing so the logs stay readable.
    this.onResize = () => {
      const w = this.opts.width;
      this.pickSize(this.nodeCount || 5);
      if (w !== this.opts.width) {
        this.applySize();
        if (this.snap) this.update({ ...this.snap, events: [] });
        this.frame(0);
      }
    };
    window.addEventListener("resize", this.onResize);
  }

  pickSize(n) {
    const narrow = (this.container.clientWidth || window.innerWidth) < 560;
    this.narrow = narrow;
    this.timerW = narrow ? 36 : 48;
    const clientsBelow = narrow && this.opts.clients;
    this.opts.width = this.opts.fixedWidth || (narrow ? 420 : 680);
    this.opts.height = this.opts.fixedHeight || (40 + n * PITCH + SPLIT + (clientsBelow ? 70 : 0));
  }

  applySize() {
    const { width: W, height: H } = this.opts;
    this.svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
    this.water.setAttribute("width", W);
    this.water.setAttribute("height", H);
    this.drawWaves();
  }

  drawWaves() {
    this.gWaves.replaceChildren();
    const { width: W, height: H } = this.opts;
    // A few fixed swells; the same places every time.
    const spots = [[0.03, 0.12], [0.86, 0.08], [0.92, 0.5], [0.02, 0.62], [0.6, 0.95], [0.3, 0.98]];
    for (const [fx, fy] of spots) {
      const x = fx * W, y = fy * H;
      el("path", { d: `M${x} ${y} q9 -5 18 0 t18 0 t18 0` }, this.gWaves);
    }
  }

  destroy() {
    window.removeEventListener("resize", this.onResize);
    this.svg.remove();
  }

  setMarked(ids) { this.marked = new Set(ids); this.paintNodes(); }
  setSelected(id) { this.selected = id; this.paintNodes(); }

  layout(snap) {
    if (snap.nodes.length !== this.nodeCount) {
      this.nodeCount = snap.nodes.length;
      this.pickSize(this.nodeCount);
      this.applySize();
    }
    const { width: W } = this.opts;
    const narrow = this.narrow;
    const clientsRight = this.opts.clients && !narrow && snap.clients.length > 0;
    this.clientsRight = clientsRight;
    this.lane = narrow ? 56 : 92;                     // where the message arcs turn
    this.grainX = this.lane + 18;                     // the cut end of each log
    this.entriesX = this.grainX + 22;                 // first entry segment
    this.segW = narrow ? 22 : 26;
    const flagRoom = 56;
    const right = W - (clientsRight ? 130 : 10) - flagRoom;
    this.cols = Math.max(4, Math.floor((right - this.entriesX) / this.segW));
    const last = Math.max(0, ...snap.nodes.map(n => n.last_index));
    this.first = Math.max(1, last - this.cols + 1);
    this.last = last;
    // Every log has the same length (a raft's logs are cut to size); entries fill it from the left.
    this.logLen = this.entriesX + this.cols * this.segW + 10 - this.grainX;
    this.portX = this.grainX + this.logLen + 6; // where client messages arrive

    // Rows: grouped by partition side, each side its own raft.
    const groups = [...new Set(snap.nodes.map(n => n.group))].sort((a, b) => a - b);
    const ordered = [...snap.nodes].sort((a, b) => a.group - b.group || a.id - b.id);
    const gap = groups.length > 1 ? SPLIT / (groups.length - 1) : 0;
    let y = 40 + 16 + LOG_H / 2;
    this.faultYs = [];
    ordered.forEach((n, i) => {
      if (i > 0 && n.group !== ordered[i - 1].group) {
        this.faultYs.push(y - PITCH + LOG_H / 2 + (PITCH - LOG_H + gap) / 2 + 6);
        y += gap;
      }
      this.pos.set("n" + n.id, { x: this.grainX, y });
      y += PITCH;
    });
    this.order = ordered;
    if (this.opts.clients) {
      const cs = snap.clients;
      cs.forEach((c, i) => {
        if (clientsRight) {
          const step = Math.min(70, (this.opts.height - 80) / Math.max(1, cs.length));
          this.pos.set("c" + c.id, { x: W - 64, y: 66 + step * i });
        } else {
          const step = Math.min(124, (W - 30) / Math.max(1, cs.length));
          this.pos.set("c" + c.id, { x: 15 + step * (i + 0.5), y: this.opts.height - 26 });
        }
      });
    }
  }

  update(snap) {
    this.snap = snap;
    this.layout(snap);
    this.tickLabel.textContent = "tick " + snap.tick;
    this.drawFaults();
    this.drawNodes(snap);
    this.drawRopes();
    this.drawClients(snap);
    this.syncFlights(snap);
    for (const ev of snap.events || []) {
      if ((ev.kind === "lost" || ev.kind === "dropped") && ev.flight) this.addPuff(ev);
    }
  }

  drawFaults() {
    this.gFaults.replaceChildren();
    const { width: W } = this.opts;
    // Clients can reach either side, so on wide figures the line stops short of the boats.
    const end = this.clientsRight ? this.portX + 48 : W - 12;
    for (const y of this.faultYs) {
      let d = `M12 ${y}`;
      for (let x = 12; x < end - 12; x += 24) d += " q6 -5 12 0 t12 0";
      el("path", { d, class: "fault" }, this.gFaults);
      el("text", { x: end - 2, y: y - 7, class: "fault-label", "text-anchor": "end" }, this.gFaults).textContent = "cut off: no messages cross";
    }
  }

  // Lashings: rope between neighbouring logs on the same side of a split. Slack (dashed) when one
  // of the two servers is down.
  drawRopes() {
    this.gRopes.replaceChildren();
    const o = this.order;
    for (let i = 1; i < o.length; i++) {
      const a = o[i - 1], b = o[i];
      if (a.group !== b.group) continue;
      const ya = this.pos.get("n" + a.id).y, yb = this.pos.get("n" + b.id).y;
      const slack = !(a.up && b.up);
      const tx = this.grainX + this.logLen - this.timerW - 4;
      for (const x of this.narrow ? [tx - 30, tx - 12] : [tx - 52, tx - 28]) {
        el("line", { x1: x, y1: ya + LOG_H / 2 - 4, x2: x, y2: yb - LOG_H / 2 + 4, class: "rope" + (slack ? " slack" : "") }, this.gRopes);
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
      v.g.style.transform = `translate(0px, ${p.y}px)`;
      v.data = n;
      this.drawLog(v, n);
    }
    for (const [id, v] of this.nodes) if (!seen.has(id)) { v.g.remove(); this.nodes.delete(id); }
    this.paintNodes();
  }

  makeNode(id) {
    const g = el("g", { class: "node-g" }, this.gNodes);
    const inner = el("g", { class: "node", tabindex: this.opts.interactive ? 0 : -1, role: this.opts.interactive ? "button" : "presentation" }, g);
    const ripple = el("ellipse", { class: "ripple", cy: LOG_H / 2 + 3, ry: 4 }, inner);
    const shadow = el("rect", { class: "shadow", y: LOG_H / 2 - 5, height: 9, rx: 4.5 }, inner);
    const body = el("rect", { class: "box", y: -LOG_H / 2, height: LOG_H, rx: LOG_H / 2 }, inner);
    const segs = el("g", { class: "segs" }, inner);
    const grain = el("ellipse", { class: "grain", cy: 0, rx: 15, ry: LOG_H / 2 }, inner);
    const ring = el("ellipse", { class: "ring", cy: 0, rx: 9, ry: LOG_H / 2 - 6 }, inner);
    const name = el("text", { class: "name", y: 4.5, "text-anchor": "middle" }, inner);
    name.textContent = "S" + id;
    const status = el("text", { class: "status", y: -LOG_H / 2 - 6 }, inner);
    const timerBg = el("rect", { class: "timer-bg", y: -LOG_H / 2 - 12, width: this.timerW, height: 6, rx: 3 }, inner);
    const timer = el("rect", { class: "timer", y: -LOG_H / 2 - 12, width: 0, height: 6, rx: 3 }, inner);
    const flag = el("g", { class: "flag" }, inner);
    el("line", { class: "mast", x1: 0, y1: LOG_H / 2 - 2, x2: 0, y2: -LOG_H / 2 - 16 }, flag);
    el("path", { class: "cloth", d: `M0 ${-LOG_H / 2 - 16} l38 8 l-38 9 z` }, flag);
    const flagText = el("text", { class: "flag-text", x: 4, y: -LOG_H / 2 - 4 }, flag);
    const mark = el("path", { class: "mark" }, inner);
    const title = el("title", {}, inner);
    if (this.opts.interactive) {
      const fire = () => this.opts.onNodeClick && this.opts.onNodeClick(id);
      inner.addEventListener("click", fire);
      inner.addEventListener("keydown", e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fire(); } });
    }
    const v = { g, inner, ripple, body, shadow, segs, grain, ring, name, status, timer, timerBg, flag, flagText, mark, title, data: null };
    this.nodes.set(id, v);
    return v;
  }

  // The log body: one segment per entry in the shared window, tinted by term, hatched until committed.
  drawLog(v, n) {
    const gx = this.grainX;
    const top = Math.min(n.last_index, this.last);
    const len = this.logLen;
    v.body.setAttribute("x", gx); v.body.setAttribute("width", len);
    v.shadow.setAttribute("x", gx + 5); v.shadow.setAttribute("width", len);
    v.ripple.setAttribute("cx", gx + len / 2); v.ripple.setAttribute("rx", len / 2 + 14);
    v.grain.setAttribute("cx", gx); v.ring.setAttribute("cx", gx); v.name.setAttribute("x", gx);
    v.status.setAttribute("x", gx - 14);
    v.mark.setAttribute("d", `M${gx - 26} ${-6} l10 6 l-10 6 z`);
    const byIndex = new Map((n.log || []).map(e => [e.index, e]));
    v.segs.replaceChildren();
    for (let k = 1; k < this.cols; k++) {
      const x = this.entriesX + k * this.segW;
      el("line", { class: "slot", x1: x, x2: x, y1: -LOG_H / 2 + 7, y2: LOG_H / 2 - 7 }, v.segs);
    }
    for (let i = this.first; i <= top; i++) {
      const x = this.entriesX + (i - this.first) * this.segW;
      const e = byIndex.get(i);
      const committed = i <= n.commit;
      const box = { x: x + 1.5, y: -LOG_H / 2 + 5, width: this.segW - 3, height: LOG_H - 10, rx: 2 };
      el("rect", { ...box, class: "seg" + (e ? ` term-${e.term % 8}` : " unknown") + (committed ? "" : " unc") }, v.segs);
      if (!committed) el("rect", { ...box, class: "seg-hatch" }, v.segs);
      if (e) {
        el("text", { class: "seg-term", x: x + this.segW / 2, y: 4, "text-anchor": "middle" }, v.segs).textContent = e.term;
        if (e.command === "no-op") el("circle", { class: "seg-noop", cx: x + this.segW - 6, cy: -LOG_H / 2 + 9, r: 1.8 }, v.segs);
      }
    }
    if (this.first > 1 && top >= this.first) {
      el("text", { class: "seg-more", x: this.entriesX - 2, y: 4, "text-anchor": "end" }, v.segs).textContent = "…";
    }
    v.flag.setAttribute("transform", `translate(${gx + len + 8}, 0)`);
    v.timerBg.setAttribute("x", gx + len - this.timerW - 4);
    v.timerBg.setAttribute("width", this.timerW);
    v.timer.setAttribute("x", gx + len - this.timerW - 4);
  }

  paintNodes() {
    for (const [id, v] of this.nodes) {
      const n = v.data;
      if (!n) continue;
      const state = n.up ? n.state : "down";
      v.inner.setAttribute("class", `node ${state}` + (this.selected === id ? " selected" : "") + (this.marked.has(id) ? " marked" : ""));
      const bits = [n.up ? state : "down"];
      if (n.up) bits.push("term " + n.term);
      if (n.up && n.state === "follower" && n.vote) bits.push("voted S" + n.vote);
      v.status.textContent = bits.join(" · ");
      v.flagText.textContent = n.state === "candidate" ? "?" : "T" + n.term;
      const frac = n.election_timeout ? Math.min(1, n.election_elapsed / n.election_timeout) : 0;
      v.timer.setAttribute("width", (this.timerW * frac).toFixed(1));
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
          el("path", { class: "hull", d: "M-50 0 h100 l-13 14 h-74 z" }, g);
          const name = el("text", { x: -46, y: -6 }, g);
          const cmd = el("text", { x: -38, y: 10.5, class: "cmd" }, g);
          v = { g, name, cmd };
          this.clients.set(c.id, v);
        }
        const p = this.pos.get("c" + c.id);
        v.g.setAttribute("transform", `translate(${p.x},${p.y})`);
        v.g.setAttribute("class", "client" + (c.busy ? " busy" : ""));
        v.name.textContent = `C${c.id}` + (c.busy ? " → S" + c.target : "");
        const text = c.command || "idle";
        v.cmd.textContent = text.length > 13 ? text.slice(0, 12) + "…" : text;
      }
    }
    for (const [id, v] of this.clients) if (!seen.has(id)) { v.g.remove(); this.clients.delete(id); }
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
      if (e.trail) e.trail.setAttribute("d", this.route(f)?.d || "");
    }
    for (const [id, e] of this.flights) if (!seen.has(id)) { e.trail?.remove(); e.remove(); this.flights.delete(id); }
  }

  makeFlight(f) {
    const r = this.route(f);
    if (!r) return null;
    const g = el("g", {}, this.gFlights);
    switch (f.kind) {
      case "vote": el("circle", { r: 5.5, class: "vote-msg" }, g); break;
      case "vote-resp": el("circle", { r: 4.5, class: "vote-msg" + (f.reject ? " hollow" : "") }, g); break;
      case "append":
        if (f.entries > 0) {
          el("rect", { x: -5, y: -5, width: 10, height: 10, class: "app-msg" }, g);
          if (!this.narrow) el("text", { x: -8, y: 3.5, class: "msg-count", "text-anchor": "end" }, g).textContent = "+" + f.entries;
        } else {
          el("rect", { x: -3.5, y: -3.5, width: 7, height: 7, class: "app-msg heartbeat" }, g);
        }
        break;
      case "append-resp": el("rect", { x: -3, y: -3, width: 6, height: 6, class: "app-msg" + (f.reject ? " hollow" : "") }, g); break;
      case "request": el("path", { d: "M0 -6 L6 0 L0 6 L-6 0 Z", class: "client-msg" }, g); break;
      case "reply": el("path", { d: "M0 -5 L5 0 L0 5 L-5 0 Z", class: "client-msg" + (f.not_leader ? " hollow" : "") }, g); break;
    }
    el("title", {}, g).textContent = describeFlight(f);
    g.flight = f;
    // The arc the message travels on, faint, so its route reads at a glance.
    g.trail = el("path", { class: "trail " + f.kind, d: r.d }, this.gTrails);
    return g;
  }

  // The route a message takes: an arc in the lane left of the logs between two servers, or a
  // straight line between a boat and the far end of a log.
  route(f) {
    const fromC = f.kind === "request", toC = f.kind === "reply";
    const a = fromC ? this.pos.get("c" + f.client) : this.pos.get("n" + f.from);
    const b = toC ? this.pos.get("c" + f.client) : this.pos.get("n" + f.to);
    if (!a || !b) return null;
    if (fromC || toC) {
      const boat = fromC ? a : b, node = fromC ? b : a;
      const bx = boat.x + (this.clientsRight ? -52 : 0), by = boat.y + (this.clientsRight ? 6 : -10);
      const nx = this.portX, ny = node.y + (toC ? 5 : -5);
      const [sx, sy, ex, ey] = fromC ? [bx, by, nx, ny] : [nx, ny, bx, by];
      return { sx, sy, cx: (sx + ex) / 2, cy: (sy + ey) / 2, ex, ey, d: `M${sx} ${sy} L${ex} ${ey}` };
    }
    // Requests and responses use slightly different arcs so they don't sit on top of each other.
    const resp = f.kind.endsWith("resp");
    const x0 = this.grainX - 18;
    const dy = Math.abs(b.y - a.y);
    const cx = x0 - 10 - dy * (this.narrow ? 0.16 : 0.24) - (resp ? 0 : 10);
    const sy = a.y + (resp ? 4 : -4), ey = b.y + (resp ? 4 : -4);
    return { sx: x0, sy, cx, cy: (sy + ey) / 2, ex: x0, ey, d: `M${x0} ${sy} Q${cx} ${(sy + ey) / 2} ${x0} ${ey}` };
  }

  // Where a message is at time (tick + frac).
  place(f, frac) {
    const r = this.route(f);
    if (!r || !this.snap) return null;
    const span = Math.max(1, f.deliver_at - f.sent_at);
    const t = Math.max(0, Math.min(1, (this.snap.tick + frac - f.sent_at) / span));
    const u = 1 - t;
    return { x: u * u * r.sx + 2 * u * t * r.cx + t * t * r.ex, y: u * u * r.sy + 2 * u * t * r.cy + t * t * r.ey };
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
    const r = this.route(ev.flight);
    if (!r) return;
    // Lost on the way: a splash leaving the sender. Dropped: a splash at the receiver.
    const t = ev.kind === "lost" ? 0.12 : 0.92, u = 1 - t;
    const x = u * u * r.sx + 2 * u * t * r.cx + t * t * r.ex, y = u * u * r.sy + 2 * u * t * r.cy + t * t * r.ey;
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
