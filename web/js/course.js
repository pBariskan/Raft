import { Sim, loadEngine } from "./engine.js";
import { ClusterView } from "./cluster-view.js";
import { Runner } from "./runner.js";
import { Script } from "./script.js";
import { renderLogs, renderStores, Feed, h, setupTheme } from "./panels.js";
import { MODULES } from "./lessons.js";
import { loadData, templateValues, fill } from "./data.js";

setupTheme();

const $ = id => document.getElementById(id);
const KEY = "raft-course-v1";

// ----- progress (kept in this browser; the course works without storage) -----

function loadProgress() {
  try {
    const p = JSON.parse(localStorage.getItem(KEY) || "{}");
    return { passed: new Set(p.passed || []), unlockAll: !!p.unlockAll, at: p.at || null };
  } catch {
    return { passed: new Set(), unlockAll: false, at: null };
  }
}

function saveProgress() {
  try {
    localStorage.setItem(KEY, JSON.stringify({ passed: [...progress.passed], unlockAll: progress.unlockAll, at: current }));
  } catch {}
}

const progress = loadProgress();
let current = { m: 0, s: 0 };
let values = {};
let scene = null;

const isOpen = i => progress.unlockAll || i === 0 || MODULES.slice(0, i).every(m => progress.passed.has(m.id));

// ----- routing: #3.2 = module 3, step 2 (1-based); #3.check = its questions -----

function parseHash() {
  const m = location.hash.match(/^#(\d+)(?:\.(\d+|check))?$/);
  if (!m) return null;
  const mi = Number(m[1]) - 1;
  if (mi < 0 || mi >= MODULES.length) return null;
  const steps = MODULES[mi].steps.length;
  const s = m[2] === "check" ? steps : m[2] ? Math.min(steps, Math.max(0, Number(m[2]) - 1)) : 0;
  return { m: mi, s };
}

function go(m, s) {
  if (!isOpen(m)) return;
  const steps = MODULES[m].steps.length;
  location.hash = `#${m + 1}.${s >= steps ? "check" : s + 1}`;
}

window.addEventListener("hashchange", route);

function route() {
  const target = parseHash() || progress.at || { m: firstOpenUnpassed(), s: 0 };
  if (!isOpen(target.m)) target.m = firstOpenUnpassed(), target.s = 0;
  current = target;
  saveProgress();
  renderToc();
  renderLesson();
}

function firstOpenUnpassed() {
  const i = MODULES.findIndex(m => !progress.passed.has(m.id));
  return i < 0 ? MODULES.length - 1 : i;
}

// ----- table of contents -----

function renderToc() {
  $("toc-progress").textContent = `${progress.passed.size}/${MODULES.length} passed`;
  $("toc-list").replaceChildren(...MODULES.map((mod, i) => {
    const n = h("span", { class: "n" }, String(i + 1).padStart(2, "0"));
    const st = h("span", { class: "st", "aria-label": progress.passed.has(mod.id) ? "passed" : "" }, progress.passed.has(mod.id) ? "✓" : isOpen(i) ? "" : "·");
    if (!isOpen(i)) return h("li", {}, h("span", { class: "locked", title: "Pass the previous module's questions to open this one" }, n, h("span", {}, mod.title), st));
    return h("li", {}, h("a", { href: `#${i + 1}.1`, "aria-current": i === current.m ? "true" : "false" }, n, h("span", {}, mod.title), st));
  }));
  $("unlock-all").checked = progress.unlockAll;
  if (window.innerWidth <= 900) $("toc-details").open = false;
}

// ----- a lesson step -----

function stopScene() {
  if (scene) {
    scene.runner?.pause();
    scene.view?.destroy();
    scene.sim?.destroy();
    scene = null;
  }
}

function renderLesson() {
  stopScene();
  const mod = MODULES[current.m];
  const steps = mod.steps.length;
  const onCheck = current.s >= steps;
  const el = $("lesson");
  const bar = h("div", { class: "steps", role: "navigation", "aria-label": "Steps" },
    ...mod.steps.map((st, i) => h("button", {
      class: i < current.s ? "done" : i === current.s ? "here" : "", title: st.title, "aria-label": `Step ${i + 1}: ${st.title}`,
      onclick: () => go(current.m, i),
    })),
    h("button", { class: "check" + (onCheck ? " here" : ""), title: "Check your understanding", "aria-label": "Questions", onclick: () => go(current.m, steps) }));
  const head = [
    h("div", { class: "crumbs" },
      h("p", { class: "label" }, `Module ${String(current.m + 1).padStart(2, "0")} of ${MODULES.length}`),
      h("p", { class: "label" }, onCheck ? "Check your understanding" : `Step ${current.s + 1} of ${steps}`)),
    h("h1", {}, mod.title),
    bar,
  ];
  el.replaceChildren(...head, ...(onCheck ? renderQuiz(mod) : renderStep(mod, mod.steps[current.s])));
  window.scrollTo({ top: 0 });
}

function renderStep(mod, step) {
  const body = h("div", { class: "prose" });
  body.innerHTML = `<h2>${step.title}</h2>` + fill(step.text, values);
  const out = [body];
  if (step.html) {
    const f = h("figure", { class: "lesson-fig" });
    const d = h("div", { class: "static-fig" });
    d.innerHTML = step.html;
    f.append(d);
    if (step.caption) f.append(h("figcaption", {}, step.caption));
    out.push(f);
  }
  if (step.scene) out.push(sceneFigure(step.scene));
  const steps = mod.steps.length;
  out.push(h("div", { class: "lesson-nav" },
    current.s > 0
      ? h("button", { class: "btn", onclick: () => go(current.m, current.s - 1) }, "← Back")
      : current.m > 0 ? h("button", { class: "btn", onclick: () => go(current.m - 1, MODULES[current.m - 1].steps.length) }, "← Previous module") : h("span"),
    h("button", { class: "btn primary", onclick: () => go(current.m, current.s + 1) },
      current.s + 1 >= steps ? "Check your understanding →" : "Next →")));
  return out;
}

// ----- engine-driven figures -----

function sceneFigure(spec) {
  const fig = h("figure", { class: "lesson-fig" });
  const box = h("div", { class: "figure" }, h("div", { class: "loading" }, "Starting the engine…"));
  const note = h("p", { class: "scene-note" }, "");
  const playBtn = h("button", { class: "btn small primary" }, "Pause");
  const restart = h("button", { class: "btn small" }, "Replay");
  const step = h("button", { class: "btn small" }, "Step");
  const tickOut = h("span", { class: "mono small muted" });
  const controls = h("div", { class: "controls" }, playBtn, step, restart, tickOut);
  const extras = [];
  const logs = spec.logs ? h("div", { class: "logs" }) : null;
  const stores = spec.stores ? h("div") : null;
  const feedEl = spec.feed ? h("div", { class: "feed" }) : null;
  if (logs) extras.push(h("div", { class: "extra" }, h("p", { class: "label" }, "logs"), logs));
  if (stores) extras.push(h("div", { class: "extra" }, h("p", { class: "label" }, "key-value stores"), stores));
  if (feedEl) extras.push(h("div", { class: "extra" }, h("p", { class: "label" }, "events"), feedEl));
  fig.append(box, note, controls, ...extras);

  const start = async () => {
    stopScene();
    const engine = await loadEngine();
    const opts = spec.replay
      ? { seed: spec.replay.seed, replay: true, mistakes: [spec.replay.mistake] }
      : { seed: spec.seed || 1, nodes: spec.nodes || 5 };
    const sim = new Sim(engine, opts);
    if (spec.replay?.jump) {
      sim.do({ op: "step", ticks: spec.replay.jump });
      sim.snapshot(1); // drop the events of the fast-forward
    }
    box.replaceChildren();
    const view = new ClusterView(box, { clients: spec.clients !== false });
    const feed = feedEl ? new Feed(feedEl) : null;
    feed?.clear();
    note.textContent = spec.replay ? `Chaos seed ${spec.replay.seed} with “${spec.replay.mistake}”, fast-forwarded to tick ${spec.replay.jump}.` : "";
    note.classList.remove("alarm");
    const script = new Script(spec.script ? spec.script() : [], {
      onNote: t => { note.textContent = t; note.classList.toggle("alarm", /Caught|crash/i.test(t)); },
    });
    let runner = null;
    runner = new Runner(sim, view, {
      speed: spec.speed || 5,
      beforeTick: r => script.tick(r),
      pauseOffscreen: true,
      onTick: snap => {
        tickOut.textContent = `tick ${snap.tick}` + (snap.leader ? ` · leader S${snap.leader}` : " · no leader");
        if (logs) renderLogs(logs, snap, window.innerWidth < 600 ? 8 : 16);
        if (stores) renderStores(stores, snap);
        if (feed) feed.add(snap.events, snap);
        if (snap.violation && spec.replay && runner?.playing) { runner.pause(); playBtn.textContent = "Play"; }
      },
    });
    scene = { sim, view, runner };
    runner.play();
    playBtn.textContent = "Pause";
  };
  playBtn.onclick = () => { if (!scene) return; playBtn.textContent = scene.runner.toggle() ? "Pause" : "Play"; };
  step.onclick = () => { if (!scene) return; scene.runner.pause(); playBtn.textContent = "Play"; scene.runner.step(1); };
  restart.onclick = () => start().catch(showError);
  const showError = e => box.replaceChildren(h("div", { class: "loading" }, "The engine could not start: " + e.message));
  queueMicrotask(() => start().catch(showError));
  return fig;
}

// ----- questions -----

// The options' order is shuffled, the same way every time for a given question, so the right
// answer isn't always in the same place.
function shuffled(n, seedText) {
  let x = 0;
  for (const c of seedText) x = (x * 31 + c.charCodeAt(0)) >>> 0;
  const order = [...Array(n).keys()];
  for (let i = n - 1; i > 0; i--) {
    x = (x * 1103515245 + 12345) >>> 0;
    const j = (x >>> 16) % (i + 1);
    [order[i], order[j]] = [order[j], order[i]];
  }
  return order;
}

function renderQuiz(mod) {
  const passed = progress.passed.has(mod.id);
  const form = h("form", { class: "quiz" });
  const intro = h("p", {}, passed
    ? "You passed this module. Answer again if you like."
    : "Answer every question correctly to open the next module. Wrong answers just show why; try again as often as you need.");
  mod.questions.forEach((q, qi) => {
    const fs = h("fieldset", {}, h("legend", {}, `${qi + 1}. ${q.q}`));
    for (const oi of shuffled(q.options.length, q.q)) {
      fs.append(h("label", { class: "option" }, h("input", { type: "radio", name: "q" + qi, value: oi, required: true }), h("span", {}, q.options[oi])));
    }
    form.append(h("div", { class: "question", "data-q": qi }, fs));
  });
  const result = h("span", { class: "quiz-result", "aria-live": "polite" });
  const next = h("div", { class: "lesson-nav" },
    h("button", { class: "btn", type: "button", onclick: () => go(current.m, mod.steps.length - 1) }, "← Back to the last step"),
    h("span"));
  form.append(h("div", {}, h("button", { class: "btn primary", type: "submit" }, "Check answers"), result));
  form.addEventListener("submit", e => {
    e.preventDefault();
    let right = 0;
    mod.questions.forEach((q, qi) => {
      const box = form.querySelector(`[data-q="${qi}"]`);
      box.querySelector(".why")?.remove();
      const picked = form.querySelector(`input[name=q${qi}]:checked`);
      box.querySelectorAll(".option").forEach(o => o.classList.remove("right", "wrong"));
      if (!picked) return;
      const ok = Number(picked.value) === q.answer;
      if (ok) right++;
      picked.closest(".option").classList.add(ok ? "right" : "wrong");
      box.append(h("p", { class: "why" + (ok ? "" : " no") }, (ok ? "Right. " : "Not quite. ") + q.why));
    });
    const all = right === mod.questions.length;
    result.textContent = all ? "All correct." : `${right} of ${mod.questions.length} correct.`;
    if (all) {
      progress.passed.add(mod.id);
      saveProgress();
      renderToc();
      const i = current.m;
      next.lastChild.replaceWith(i + 1 < MODULES.length
        ? h("button", { class: "btn primary", type: "button", onclick: () => go(i + 1, 0) }, `Next: ${MODULES[i + 1].title} →`)
        : h("a", { class: "btn primary", href: "workplace.html" }, "Course complete. Open the Workplace →"));
    }
  });
  return [intro, form, next];
}

// ----- start -----

$("unlock-all").addEventListener("change", e => { progress.unlockAll = e.target.checked; saveProgress(); renderToc(); });
$("reset-progress").addEventListener("click", () => {
  if (!confirm("Forget which modules you passed?")) return;
  progress.passed.clear();
  progress.unlockAll = false;
  saveProgress();
  go(0, 0);
  route();
});

(async () => {
  try { values = templateValues(await loadData()); } catch { values = {}; }
  route();
})();
