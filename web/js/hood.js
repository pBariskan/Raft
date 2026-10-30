import { setupTheme } from "./panels.js";
import { loadData, mistakesTable, benchTable } from "./data.js";

setupTheme();
const $ = id => document.getElementById(id);
const fmt = n => Number(n).toLocaleString("en-US");
const NS = "http://www.w3.org/2000/svg";

function svgEl(name, attrs = {}, parent) {
  const e = document.createElementNS(NS, name);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (parent) parent.appendChild(e);
  return e;
}

function stats(el, items) {
  el.innerHTML = items.map(([b, s]) => `<div><b>${b}</b><span>${s}</span></div>`).join("");
}

// A small hover tooltip shared by both charts.
function tooltip(container) {
  const tip = document.createElement("div");
  tip.className = "tip";
  container.appendChild(tip);
  return {
    show(x, y, html) { tip.innerHTML = html; tip.style.left = x + "px"; tip.style.top = y + "px"; tip.style.display = "block"; },
    hide() { tip.style.display = "none"; },
  };
}

function niceMax(v) {
  const p = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 1.5, 2, 2.5, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

// Throughput: clients on x (as categories), writes/s on y, one line per cluster size.
function throughputChart(el, bench) {
  const rows = bench.throughput.filter(t => t.fsync);
  const xs = [...new Set(rows.map(t => t.clients))].sort((a, b) => a - b);
  const sizes = [...new Set(rows.map(t => t.nodes))].sort();
  const W = 460, H = 280, L = 54, R = 70, T = 14, B = 34;
  const ymax = niceMax(Math.max(...rows.map(t => t.ops_per_sec)));
  const x = i => L + (i * (W - L - R)) / (xs.length - 1);
  const y = v => H - B - (v / ymax) * (H - T - B);
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": "Writes per second by number of clients, for 3 and 5 servers" });
  const grid = svgEl("g", { class: "grid" }, svg);
  for (let i = 0; i <= 4; i++) {
    const v = (ymax * i) / 4;
    svgEl("line", { x1: L, x2: W - R + 10, y1: y(v), y2: y(v) }, grid);
    svgEl("text", { x: L - 8, y: y(v) + 4, "text-anchor": "end" }, svg).textContent = fmt(v);
  }
  xs.forEach((c, i) => { svgEl("text", { x: x(i), y: H - B + 18, "text-anchor": "middle" }, svg).textContent = c; });
  svgEl("text", { x: (L + W - R) / 2, y: H - 2, "text-anchor": "middle" }, svg).textContent = "concurrent clients";
  const tip = tooltip(el);
  sizes.forEach((n, si) => {
    const cls = si === 0 ? "s1" : "s2";
    const pts = xs.map((c, i) => [x(i), y(rows.find(t => t.nodes === n && t.clients === c).ops_per_sec)]);
    svgEl("path", { class: `line ${cls}`, d: "M" + pts.map(p => p.join(",")).join("L") }, svg);
    const last = pts[pts.length - 1];
    svgEl("text", { x: last[0] + 10, y: last[1] + 4, class: "dlabel" }, svg).textContent = `${n} servers`;
    xs.forEach((c, i) => {
      const t = rows.find(r => r.nodes === n && r.clients === c);
      svgEl("circle", { class: `pt ${cls}`, cx: pts[i][0], cy: pts[i][1], r: 4.5 }, svg);
      const hit = svgEl("circle", { class: "hit", cx: pts[i][0], cy: pts[i][1], r: 14, tabindex: 0 }, svg);
      const show = () => {
        const box = svg.getBoundingClientRect(), k = box.width / W;
        tip.show(pts[i][0] * k, pts[i][1] * k,
          `${n} servers, ${c} client${c > 1 ? "s" : ""}<br>${fmt(Math.round(t.ops_per_sec))} writes/s<br>p50 ${t.p50_ms} ms · p99 ${t.p99_ms} ms`);
      };
      hit.addEventListener("pointerenter", show);
      hit.addEventListener("focus", show);
      hit.addEventListener("pointerleave", tip.hide);
      hit.addEventListener("blur", tip.hide);
    });
  });
  el.prepend(svg);
}

// Failover: one dot per trial on a milliseconds axis, a row per cluster size, median marked.
function failoverChart(el, bench) {
  const W = 460, H = 170, L = 84, R = 20, T = 20, B = 40;
  const all = bench.failover.flatMap(f => f.stall_ms);
  const xmax = niceMax(Math.max(...all) * 1.05);
  const x = v => L + (v / xmax) * (W - L - R);
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": "Write stall after the leader is killed, per trial" });
  const grid = svgEl("g", { class: "grid" }, svg);
  for (let i = 0; i <= 4; i++) {
    const v = (xmax * i) / 4;
    svgEl("line", { x1: x(v), x2: x(v), y1: T - 6, y2: H - B }, grid);
    svgEl("text", { x: x(v), y: H - B + 16, "text-anchor": "middle" }, svg).textContent = fmt(v);
  }
  svgEl("text", { x: (L + W - R) / 2, y: H - 4, "text-anchor": "middle" }, svg).textContent = "milliseconds until writes resume";
  const tip = tooltip(el);
  const rowH = (H - T - B) / bench.failover.length;
  bench.failover.forEach((f, fi) => {
    const cls = fi === 0 ? "s1" : "s2";
    const cy = T + rowH * (fi + 0.5);
    svgEl("text", { x: 6, y: cy + 4 }, svg).textContent = `${f.nodes} servers`;
    svgEl("line", { class: "med", x1: x(f.median_ms), x2: x(f.median_ms), y1: cy - 13, y2: cy + 13 }, svg);
    f.stall_ms.forEach((v, i) => {
      const jitter = ((i % 3) - 1) * 5;
      svgEl("circle", { class: `pt ${cls}`, cx: x(v), cy: cy + jitter, r: 4.5, "fill-opacity": 0.85 }, svg);
      const hit = svgEl("circle", { class: "hit", cx: x(v), cy: cy + jitter, r: 10, tabindex: 0 }, svg);
      const show = () => {
        const k = svg.getBoundingClientRect().width / W;
        tip.show(x(v) * k, (cy + jitter) * k, `${f.nodes} servers, trial ${i + 1}: ${v} ms<br>median ${f.median_ms} ms`);
      };
      hit.addEventListener("pointerenter", show);
      hit.addEventListener("focus", show);
      hit.addEventListener("pointerleave", tip.hide);
      hit.addEventListener("blur", tip.hide);
    });
  });
  el.prepend(svg);
}

(async () => {
  const d = await loadData();
  if (d.none) {
    const n = d.none;
    stats($("chaos-stats"), [
      [fmt(n.seeds), "seeded runs"],
      [fmt(n.ticks), "ticks simulated"],
      [fmt(n.crashes), "server crashes"],
      [fmt(n.partitions), "network splits"],
      [fmt(n.leaders), "leaders elected"],
      [fmt(n.requests), "client requests, all answered"],
      [fmt(n.failed_seeds ?? 0), "safety or linearizability violations"],
    ]);
    $("chaos-meta").textContent = `Seeds ${n.first_seed}–${n.first_seed + n.seeds - 1}, ${n.seconds.toFixed(0)} s on one machine, ${n.go_version}. Replay any seed: go run ./cmd/chaos -from N -seeds 1 -v`;
  }
  $("mistake-table").innerHTML = mistakesTable(d, { links: true });
  const b = d.bench;
  if (b) {
    $("bench-machine").textContent = `Measured ${b.generated_at.slice(0, 10)} on a ${b.machine.cpus}-CPU ${b.machine.os}/${b.machine.arch} ${b.machine.note || "machine"}; numbers depend on the disk and will differ on yours.`;
    throughputChart($("tput"), b);
    failoverChart($("failover"), b);
    const on1 = b.throughput.find(t => t.nodes === 3 && t.clients === 1 && t.fsync);
    const off1 = b.throughput.find(t => t.nodes === 3 && t.clients === 1 && !t.fsync);
    const on32 = b.throughput.find(t => t.nodes === 3 && t.clients === 32 && t.fsync);
    const off32 = b.throughput.find(t => t.nodes === 3 && t.clients === 32 && !t.fsync);
    if (on1 && off1 && on32 && off32) {
      stats($("fsync-stats"), [
        [`${on1.p50_ms} ms`, "median write, 1 client, fsync on"],
        [`${off1.p50_ms} ms`, "the same without fsync (unsafe)"],
        [fmt(Math.round(on32.ops_per_sec)), "writes/s, 32 clients, fsync on"],
        [fmt(Math.round(off32.ops_per_sec)), "the same without fsync (unsafe)"],
      ]);
    }
    $("bench-table").innerHTML = benchTable(d);
  }
})();
