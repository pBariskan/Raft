import { Sim, loadEngine } from "./engine.js";
import { ClusterView } from "./cluster-view.js";
import { Runner } from "./runner.js";
import { Script, leaderOf, followersOf } from "./script.js";
import { renderLogs, renderStores, Feed, h, setupTheme } from "./panels.js";

setupTheme();

const $ = id => document.getElementById(id);
const SPEEDS = [1, 2, 4, 6, 10, 20, 40, 80];
const PROPERTIES = [
  ["Election Safety", "at most one leader per term"],
  ["Leader Completeness", "a new leader has every committed entry"],
  ["State Machine Safety", "no two servers apply different commands at one index"],
  ["Log Matching", "same index and term → same history before it"],
  ["Internal consistency", "no server is asked to undo a committed entry"],
];

const state = {
  sim: null, view: null, runner: null, script: null,
  selected: 0, marked: new Set(), myClient: 0,
  params: readHash(), reported: false,
};

function readHash() {
  const p = new URLSearchParams(location.hash.slice(1));
  const num = (k, d) => (p.has(k) && Number(p.get(k)) > 0 ? Math.floor(Number(p.get(k))) : d);
  return {
    seed: num("seed", 0) || num("replay", 0) || 1 + Math.floor(Math.random() * 99999),
    nodes: [3, 5, 7].includes(num("nodes", 5)) ? num("nodes", 5) : 5,
    mistake: p.get("mistake") || "none",
    replay: p.has("replay"),
  };
}

function writeHash() {
  const p = state.params;
  const q = new URLSearchParams();
  if (p.replay) q.set("replay", p.seed); else { q.set("seed", p.seed); if (p.nodes !== 5) q.set("nodes", p.nodes); }
  if (p.mistake !== "none") q.set("mistake", p.mistake);
  history.replaceState(null, "", "#" + q.toString());
}

function start(params = state.params, script = null) {
  state.params = { ...params };
  const p = state.params;
  if (state.runner) state.runner.pause();
  if (state.sim) state.sim.destroy();
  if (state.view) state.view.destroy();
  $("figure").replaceChildren();
  state.sim = new Sim(state.engine, {
    seed: p.seed, nodes: p.nodes, replay: p.replay,
    mistakes: p.mistake === "none" ? [] : [p.mistake],
  });
  state.view = new ClusterView($("figure"), { interactive: true, onNodeClick: selectNode });
  state.feed.clear();
  state.selected = 0;
  state.marked.clear();
  state.myClient = 0;
  state.reported = false;
  state.script = script ? new Script(script, { onNote: note }) : null;
  note(null);
  $("lin-out").textContent = "";
  $("traffic").checked = false;
  $("loss").value = 0; $("loss-out").textContent = "0%";
  $("delay").value = 3; $("delay-out").textContent = "1–3";
  $("seed").value = p.seed;
  $("nodes").value = p.nodes;
  $("mistake").value = p.mistake;
  $("replay-info").hidden = !p.replay;
  buildPicks();
  writeHash();
  state.runner = new Runner(state.sim, state.view, {
    speed: SPEEDS[$("speed").value],
    onTick,
    beforeTick: state.script ? r => state.script.tick(r) : null,
    pauseOffscreen: true,
  });
  state.runner.play();
  syncPlay();
}

function onTick(snap) {
  state.feed.add(snap.events, snap);
  renderLogs($("logs"), snap, window.innerWidth < 600 ? 9 : 18);
  renderStores($("stores"), snap);
  renderStatus(snap);
  renderChecks(snap);
  renderNode(snap);
  if (snap.replay) renderReplay(snap);
  if (snap.violation && !state.reported) {
    state.reported = true;
    state.runner.pause();
    syncPlay();
    note(`A safety check failed at tick ${snap.tick}: ${snap.broken}. ` +
      (snap.mistakes.length ? `That is what “${snap.mistakes[0]}” breaks. ` : "") +
      "The run is paused here; step on, or start over.", true);
  }
}

function renderStatus(snap) {
  const ld = snap.leader;
  const up = snap.nodes.filter(n => n.up).length;
  const term = Math.max(...snap.nodes.map(n => n.term));
  const split = new Set(snap.nodes.map(n => n.group)).size > 1;
  $("status").textContent = [
    `tick ${snap.tick}`,
    ld ? `leader S${ld} (term ${snap.nodes.find(n => n.id === ld).term})` : `no leader (highest term ${term})`,
    `${up}/${snap.nodes.length} up`,
    split ? "network split" : null,
    snap.drop_rate ? `${Math.round(snap.drop_rate * 100)}% loss` : null,
    snap.max_delay > 3 ? `delay ${snap.min_delay}–${snap.max_delay}` : null,
  ].filter(Boolean).join("  ·  ");
}

function renderChecks(snap) {
  $("checks").replaceChildren(...PROPERTIES.map(([name, what]) => {
    const broken = snap.broken === name;
    return h("li", { title: what },
      h("span", {}, name),
      h("span", { class: broken ? "state-bad" : "state-ok" }, broken ? "BROKEN" : "holding"));
  }));
  if (snap.broken === "Liveness" || snap.broken === "Linearizability") {
    $("checks").append(h("li", {}, h("span", {}, snap.broken), h("span", { class: "state-bad" }, "BROKEN")));
  }
}

function selectNode(id) {
  state.selected = state.selected === id ? 0 : id;
  state.view.setSelected(state.selected);
  renderNode(state.runner.snap);
  if (state.selected && window.innerWidth < 980) $("node-panel").scrollIntoView({ behavior: "smooth", block: "nearest" });
}

function renderNode(snap) {
  const id = state.selected;
  $("node-hint").hidden = !!id;
  if (!id) { $("node-info").replaceChildren(); return; }
  const n = snap.nodes.find(x => x.id === id);
  const dl = h("dl", { class: "kv" },
    h("dt", {}, "server"), h("dd", {}, `S${id} (${n.up ? n.state : "down"})`),
    h("dt", {}, "term"), h("dd", {}, n.term),
    h("dt", {}, "voted for"), h("dd", {}, n.vote ? "S" + n.vote : "nobody yet"),
    h("dt", {}, "knows leader"), h("dd", {}, n.lead ? "S" + n.lead : "none"),
    h("dt", {}, "log"), h("dd", {}, `${n.last_index} entries, ${n.commit} committed`),
    h("dt", {}, "applied"), h("dd", {}, n.applied),
    h("dt", {}, "timer"), h("dd", {}, n.state === "leader" ? "sends heartbeats" : `${n.election_elapsed}/${n.election_timeout} ticks`),
  );
  const actions = h("div", { class: "btn-row" },
    n.up
      ? h("button", { class: "btn small danger", onclick: () => act({ op: "crash", node: id }) }, `Crash S${id}`)
      : h("button", { class: "btn small", onclick: () => act({ op: "restart", node: id }) }, `Restart S${id}`),
    h("button", { class: "btn small", onclick: () => act({ op: "partition", groups: [id] }) }, `Isolate S${id}`),
  );
  $("node-info").replaceChildren(dl, actions);
}

function act(cmd) {
  try { state.runner.run(cmd); }
  catch (e) { note(e.message, true); }
}

function buildPicks() {
  const picks = $("split-picks");
  picks.replaceChildren();
  for (let i = 1; i <= state.params.nodes; i++) {
    const b = h("button", { class: "btn small pick", "aria-pressed": "false", type: "button" }, "S" + i);
    b.addEventListener("click", () => {
      if (state.marked.has(i)) state.marked.delete(i); else state.marked.add(i);
      b.setAttribute("aria-pressed", state.marked.has(i));
      state.view.setMarked([...state.marked]);
    });
    picks.append(b);
  }
}

function note(text, alarm = false) {
  const el = $("note");
  el.hidden = !text;
  el.textContent = text || "";
  el.style.borderLeftColor = alarm ? "var(--signal)" : "var(--wire)";
}

function syncPlay() {
  $("play").textContent = state.runner.playing ? "Pause" : "Play";
}

function renderReplay(snap) {
  const r = snap.replay;
  const info = $("replay-info");
  info.hidden = false;
  if (r.result) {
    const res = r.result;
    info.textContent = res.violation
      ? `Replay of seed ${snap.seed} (${r.profile}): ${res.violation} broken at tick ${res.violation_tick}.`
      : `Replay of seed ${snap.seed} (${r.profile}) finished: recovered, history ${res.linearizable}.`;
  } else if (r.remaining > 0 && !snap.violation) {
    info.textContent = `Replaying chaos seed ${snap.seed} (${r.profile}): ${r.remaining} ticks of faults left, then healing.`;
  } else {
    info.replaceChildren(`Chaos phase over (seed ${snap.seed}, ${r.profile}). `,
      h("button", { class: "btn small", onclick: finishReplay }, "Finish: heal and check"));
  }
}

function finishReplay() {
  const r = state.runner.run({ op: "finish" });
  const res = r.chaos;
  note(res.violation
    ? `${res.violation}: ${res.detail}`
    : `All faults removed: every client got its answer, a final write reached every server, and the history is ${res.linearizable}.`, !!res.violation);
}

// ---------- scenarios ----------

const quorum = n => Math.floor(n / 2) + 1;

const SCENARIOS = {
  "crash-leader": () => [
    { wait: "leader", note: "Waiting for a leader…" },
    { wait: 20, do: (sim, v, s) => { v.old = leaderOf(s); sim.do({ op: "crash", node: v.old }); },
      note: (v, s) => `S${leaderOf(s)}, the leader, crashes. The others stop hearing heartbeats; watch their election timers fill up. The first to run out becomes a candidate.` },
    { wait: s => s.leader !== 0, note: (v, s) => `S${s.leader} wins a majority of votes and leads term ${s.nodes.find(n => n.id === s.leader).term}. The cluster carries on without S${v.old}.` },
    { wait: 50, do: (sim, v) => sim.do({ op: "restart", node: v.old }),
      note: v => `S${v.old} restarts from its disk. It sees a higher term in the leader's messages, so it becomes a follower and catches up.` },
  ],
  "minority-leader": n => [
    { wait: "leader", do: sim => sim.do({ op: "traffic", on: true }), note: "Waiting for a leader; background clients are sending requests…" },
    { wait: 30, do: (sim, v, s) => {
        v.old = leaderOf(s);
        v.side = [v.old, ...followersOf(s, v.old).slice(0, n - quorum(n) - 1)];
        sim.do({ op: "partition", groups: v.side });
      },
      note: v => `The network splits: ${v.side.map(x => "S" + x).join(" and ")} on one side, a majority on the other. S${v.old} still thinks it leads, but it can't reach a majority, so nothing it appends gets committed (hatched in the logs).` },
    { wait: (s, v) => s.leader !== 0 && s.leader !== v.old,
      note: (v, s) => `The majority side elects S${s.leader} with a higher term and keeps committing. Two servers call themselves leader, in different terms; only one can commit.` },
    { wait: 60, do: sim => sim.do({ op: "heal" }),
      note: v => `Healed. S${v.old} sees the higher term, steps down, and its uncommitted entries are replaced by the new leader's log.` },
  ],
  "old-leader": () => [
    { wait: "leader", note: "Waiting for a leader…" },
    { wait: 20, do: (sim, v, s) => { v.old = leaderOf(s); sim.do({ op: "partition", groups: [v.old] }); sim.do({ op: "traffic", on: true }); },
      note: v => `S${v.old}, the leader, is cut off alone. Clients that reach it get no answer: it can't commit anything by itself.` },
    { wait: (s, v) => s.leader !== 0 && s.leader !== v.old, note: (v, s) => `The rest elect S${s.leader}. Requests flow again through the new leader.` },
    { wait: 80, do: sim => sim.do({ op: "heal" }),
      note: v => `S${v.old} comes back still believing it leads an old term. Its first message meets a higher term: it steps down at once and becomes a follower.` },
  ],
  "majority-down": n => [
    { wait: "leader", do: sim => sim.do({ op: "traffic", on: true }), note: "Waiting for a leader; background clients are sending requests…" },
    { wait: 30, do: (sim, v, s) => { v.down = followersOf(s).slice(0, n - quorum(n)); v.down.forEach(id => sim.do({ op: "crash", node: id })); },
      note: v => `${v.down.map(x => "S" + x).join(" and ")} crash. ${quorum(n)} of ${n} are still up: a majority, so requests still commit.` },
    { wait: 60, do: (sim, v, s) => { v.last = followersOf(s)[0]; sim.do({ op: "crash", node: v.last }); },
      note: v => `S${v.last} crashes too. Now fewer than ${quorum(n)} servers are up: nothing can be committed. Clients wait; nobody gets a wrong answer.` },
    { wait: 80, do: (sim, v) => sim.do({ op: "restart", node: v.last }),
      note: v => `S${v.last} restarts from its disk. A majority is back, and the waiting requests complete.` },
  ],
  lossy: () => [
    { wait: "leader", do: sim => { sim.do({ op: "drop", rate: 0.3 }); sim.do({ op: "traffic", on: true }); },
      note: "30% of messages now disappear (×). Raft doesn't need reliable delivery: heartbeats repeat unacknowledged entries, and clients retry with the same request number so nothing is applied twice." },
  ],
  slow: () => [
    { wait: "leader", do: sim => sim.do({ op: "delay", min: 4, max: 12 }), note: "Messages now take 4–12 ticks, about as long as an election timeout." },
    { wait: 20, do: (sim, v, s) => sim.do({ op: "crash", node: leaderOf(s) }),
      note: "The leader crashes. With slow messages, timers run out before votes arrive: several candidates split the vote and elections repeat until random timeouts separate them." },
  ],
};

// ---------- wiring ----------

async function main() {
  state.feed = new Feed($("feed"));
  try {
    state.engine = await loadEngine();
  } catch (e) {
    $("loading").textContent = "The engine could not load: " + e.message;
    return;
  }
  start();

  $("play").addEventListener("click", () => { state.runner.toggle(); syncPlay(); });
  $("step").addEventListener("click", () => { state.runner.pause(); syncPlay(); state.runner.step(1); });
  $("step10").addEventListener("click", () => { state.runner.pause(); syncPlay(); state.runner.step(10); });
  $("speed").addEventListener("input", e => {
    const s = SPEEDS[e.target.value];
    state.runner.setSpeed(s);
    $("speed-out").textContent = s + " ticks/s";
  });
  $("split").addEventListener("click", () => {
    if (state.marked.size === 0 || state.marked.size === state.params.nodes) {
      note("Pick some servers (not all) to cut off first.", true);
      return;
    }
    act({ op: "partition", groups: [...state.marked] });
    state.marked.clear();
    buildPicks();
    state.view.setMarked([]);
  });
  $("heal").addEventListener("click", () => act({ op: "heal" }));
  $("loss").addEventListener("input", e => {
    $("loss-out").textContent = e.target.value + "%";
    act({ op: "drop", rate: Number(e.target.value) / 100 });
  });
  $("delay").addEventListener("input", e => {
    $("delay-out").textContent = "1–" + e.target.value;
    act({ op: "delay", min: 1, max: Number(e.target.value) });
  });
  $("client-form").addEventListener("submit", e => {
    e.preventDefault();
    if (!state.myClient) state.myClient = state.sim.do({ op: "client" }).client;
    try {
      state.runner.run({ op: "submit", client: state.myClient, kv: $("op").value, key: $("key").value.trim() || "x", value: $("value").value });
    } catch (err) {
      note(`C${state.myClient} is still waiting for its last answer. A client sends one request at a time.`, true);
    }
  });
  $("traffic").addEventListener("change", e => act({ op: "traffic", on: e.target.checked }));
  $("lin").addEventListener("click", () => {
    $("lin-out").textContent = "checking…";
    setTimeout(() => {
      const r = state.sim.do({ op: "check" });
      $("lin-out").className = "small mono " + (r.verdict === "linearizable" ? "state-ok" : r.verdict === "unknown" ? "state-wait" : "state-bad");
      $("lin-out").textContent = r.ops ? `${r.verdict} (${r.ops} requests)` : "no requests yet";
    }, 20);
  });
  $("verbose").addEventListener("change", e => { state.feed.opts.verbose = e.target.checked; });
  $("reset").addEventListener("click", () => start({
    seed: Number($("seed").value) || 1, nodes: Number($("nodes").value), mistake: $("mistake").value, replay: false,
  }));
  $("replay").addEventListener("click", () => start({
    seed: Number($("seed").value) || 1, nodes: 5, mistake: $("mistake").value, replay: true,
  }));
  $("share").addEventListener("click", async () => {
    writeHash();
    try { await navigator.clipboard.writeText(location.href); $("share").textContent = "Copied"; }
    catch { prompt("Copy this link:", location.href); }
    setTimeout(() => { $("share").textContent = "Copy link"; }, 1500);
  });
  document.querySelectorAll("[data-scenario]").forEach(b => b.addEventListener("click", () => {
    const n = state.params.replay ? 5 : state.params.nodes;
    start({ ...state.params, replay: false, nodes: n }, SCENARIOS[b.dataset.scenario](n));
    document.querySelector(".wp-main").scrollIntoView({ behavior: "smooth", block: "start" });
  }));
  window.addEventListener("hashchange", () => {
    const p = readHash();
    if (p.seed !== state.params.seed || p.replay !== state.params.replay || p.mistake !== state.params.mistake || p.nodes !== state.params.nodes) start(p);
  });
}

main();
