// Timed scripts on top of a running simulation: "wait for a leader, crash it, wait 40 ticks,
// heal…". Used by the Workplace scenarios and the Course figures. A step waits for a number of
// ticks after the previous step, or for a condition on the snapshot, then runs its action.

export function leaderOf(snap) {
  if (snap.leader) return snap.leader;
  let best = null;
  for (const n of snap.nodes) if (n.up && n.state === "leader" && (!best || n.term > best.term)) best = n;
  return best ? best.id : 0;
}

export function followersOf(snap, leader = leaderOf(snap)) {
  return snap.nodes.filter(n => n.id !== leader && n.up).map(n => n.id);
}

const conditions = {
  leader: s => s.leader !== 0,
  idle: s => s.clients.every(c => !c.busy),
  "no-leader": s => s.leader === 0,
};

export class Script {
  constructor(steps, opts = {}) {
    this.steps = steps;
    this.i = 0;
    this.waited = 0;
    this.vars = {};
    this.onNote = opts.onNote || (() => {});
    this.onDone = opts.onDone || (() => {});
    this.done = steps.length === 0;
  }

  // Call before every tick (Runner's beforeTick).
  tick(runner) {
    if (this.done) return;
    this.waited++;
    // Several steps can fire on the same tick if their waits are already met.
    for (let guard = 0; guard < 20 && !this.done; guard++) {
      const step = this.steps[this.i];
      const snap = runner.sim.peek(1);
      if (!this.ready(step, snap)) break;
      if (step.do) step.do(runner.sim, this.vars, snap);
      if (step.note) this.onNote(typeof step.note === "function" ? step.note(this.vars, snap) : step.note);
      this.i++;
      this.waited = 0;
      if (this.i >= this.steps.length) {
        this.done = true;
        this.onDone();
      }
    }
  }

  ready(step, snap) {
    const w = step.wait ?? 0;
    if (typeof w === "number") return this.waited >= w;
    if (typeof w === "function") return w(snap, this.vars);
    return conditions[w] ? conditions[w](snap) : true;
  }
}
