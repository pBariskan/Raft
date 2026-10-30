// Loads the numbers the site shows. They are produced by real runs (scripts/site-data.sh) and
// stored in web/data; nothing here is typed in by hand.

const base = new URL("../data/", import.meta.url);

export const MISTAKES = [
  ["commit-old-terms", "Count replicas of an old-term entry as a commit (and skip the no-op)", "Figure 8"],
  ["vote-ignores-log", "Vote without comparing logs", "the vote restriction"],
  ["accept-stale-leader", "Accept entries from a leader of an older term", "rejecting lower terms"],
  ["forget-vote", "Don't save the vote to disk", "persistence"],
  ["local-reads", "Leader answers GET from its own memory", "reads through the log"],
  ["no-dedupe", "Apply a retried request again", "client sessions"],
];

const fmt = n => Number(n).toLocaleString("en-US");

let loading = null;
export function loadData() {
  if (!loading) {
    loading = (async () => {
      const get = async name => {
        try {
          const r = await fetch(new URL(name, base));
          return r.ok ? await r.json() : null;
        } catch { return null; }
      };
      const [none, bench, ...ms] = await Promise.all([
        get("chaos/none.json"), get("bench.json"), ...MISTAKES.map(([m]) => get(`chaos/${m}.json`)),
      ]);
      const mistakes = {};
      MISTAKES.forEach(([m], i) => { mistakes[m] = ms[i]; });
      return { none, bench, mistakes };
    })();
  }
  return loading;
}

export function mistakesTable(d, opts = {}) {
  const rows = MISTAKES.map(([m, what, rule]) => {
    const s = d.mistakes[m];
    if (!s) return "";
    const props = Object.keys(s.failures_by_property || {}).join(", ") || "—";
    const first = s.failures && s.failures[0];
    const watch = opts.links && first
      ? ` <a class="mono small" href="workplace.html#replay=${first.seed}&mistake=${m}">watch seed ${first.seed}</a>` : "";
    return `<tr><td><code>${m}</code><br><span class="small muted">${what}</span></td>
      <td class="num">${fmt(s.failed_seeds)} / ${fmt(s.seeds)}</td><td>${props}${watch}</td></tr>`;
  }).join("");
  return `<div class="table-scroll"><table class="data compact"><thead><tr><th>mistake</th><th class="num">seeds caught</th><th>caught by</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

export function benchTable(d) {
  const b = d.bench;
  if (!b) return "";
  const rows = b.throughput.filter(t => t.fsync).map(t =>
    `<tr><td>${t.nodes}</td><td class="num">${t.clients}</td><td class="num">${fmt(Math.round(t.ops_per_sec))}</td><td class="num">${t.p50_ms}</td><td class="num">${t.p99_ms}</td></tr>`).join("");
  return `<div class="table-scroll"><table class="data compact"><thead><tr><th>servers</th><th class="num">clients</th><th class="num">writes/s</th><th class="num">p50 ms</th><th class="num">p99 ms</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

export function templateValues(d) {
  const v = {};
  if (d.none) {
    v["chaos.seeds"] = fmt(d.none.seeds);
    v["chaos.requests"] = fmt(d.none.requests);
    v["chaos.crashes"] = fmt(d.none.crashes);
    v["chaos.partitions"] = fmt(d.none.partitions);
    v["chaos.violations"] = fmt(d.none.failed_seeds ?? (d.none.failures || []).length);
  }
  for (const [m] of MISTAKES) if (d.mistakes[m]) v["mistake." + m] = fmt(d.mistakes[m].failed_seeds);
  if (d.bench) {
    const f3 = d.bench.failover.find(f => f.nodes === 3);
    if (f3) v["bench.failover3"] = fmt(Math.round(f3.median_ms));
    v["bench.machine"] = d.bench.machine.note ? `a ${d.bench.machine.note.split(";")[0]}` : `a ${d.bench.machine.cpus}-CPU machine`;
    v["bench-table"] = benchTable(d);
  }
  v["mistakes-table"] = mistakesTable(d);
  return v;
}

export function fill(html, values) {
  return html.replace(/\{\{([\w.-]+)\}\}/g, (_, k) => values[k] ?? "—");
}
