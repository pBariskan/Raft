import { Sim } from "./engine.js";
import { ClusterView } from "./cluster-view.js";
import { Runner } from "./runner.js";
import { setupTheme } from "./panels.js";
import { loadData, templateValues } from "./data.js";

setupTheme();
const $ = id => document.getElementById(id);

async function hero() {
  const box = $("hero");
  let sim;
  try {
    sim = await Sim.create({ seed: 1 + Math.floor(Math.random() * 99999), nodes: 5 });
  } catch (e) {
    box.firstChild.textContent = "The engine could not load in this browser.";
    return;
  }
  box.replaceChildren();
  let runner;
  const view = new ClusterView(box, {
    interactive: true,
    onNodeClick: id => {
      const n = runner.snap.nodes.find(x => x.id === id);
      runner.run({ op: n.up ? "crash" : "restart", node: id });
    },
  });
  runner = new Runner(sim, view, {
    speed: 6,
    pauseOffscreen: true,
    onTick: s => {
      const up = s.nodes.filter(n => n.up).length;
      $("hero-status").textContent = s.leader
        ? `Leader: S${s.leader}. ${up} of 5 up.`
        : up < 3 ? `${up} of 5 up: no majority, so nothing can commit.` : `Electing a leader… ${up} of 5 up.`;
    },
  });
  sim.do({ op: "traffic", on: true });
  runner.play();
}

async function facts() {
  const d = await loadData();
  const v = templateValues(d);
  const items = [];
  if (d.none) items.push([v["chaos.seeds"], `seeded chaos runs (crashes, splits, lost messages): ${v["chaos.violations"]} safety violations`]);
  const caught = Object.values(d.mistakes).filter(s => s && s.failed_seeds > 0).length;
  items.push([`${caught} of 6`, "classic Raft mistakes, switched on deliberately, caught by the checkers"]);
  if (d.bench) {
    const t = d.bench.throughput.find(x => x.nodes === 3 && x.fsync && x.clients === 32);
    if (t) items.push([Math.round(t.ops_per_sec).toLocaleString("en-US"), "writes per second on 3 real servers with fsync, 32 clients"]);
    items.push([v["bench.failover3"] + " ms", "median time writes stall when the leader is killed"]);
  }
  $("facts").replaceChildren(...items.map(([big, what]) => {
    const d = document.createElement("div");
    d.className = "fact";
    d.innerHTML = `<span class="big">${big}</span><span class="what">${what}</span>`;
    return d;
  }));
}

hero();
facts();
