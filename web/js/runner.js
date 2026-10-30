// Plays a simulation in real time: advances the engine by whole ticks at the chosen speed and
// lets the drawing move messages smoothly in between.

export class Runner {
  constructor(sim, view, opts = {}) {
    this.sim = sim;
    this.view = view;
    this.speed = opts.speed ?? 6;   // ticks per second
    this.logTail = opts.logTail ?? 24;
    this.onTick = opts.onTick || (() => {});
    this.beforeTick = opts.beforeTick || null; // may run commands before each tick
    this.playing = false;
    this.acc = 0;
    this.last = 0;
    this.raf = 0;
    this.snap = sim.snapshot(this.logTail);
    view.update(this.snap);
    view.frame(0);
    this.onTick(this.snap);
    this.loop = this.loop.bind(this);
    this.visible = true;
    if ("IntersectionObserver" in window && opts.pauseOffscreen && view.svg) {
      new IntersectionObserver(es => { this.visible = es[0].isIntersecting; }).observe(view.svg);
    }
  }

  play() {
    if (this.playing) return;
    this.playing = true;
    this.last = performance.now();
    this.raf = requestAnimationFrame(this.loop);
  }

  pause() {
    this.playing = false;
    cancelAnimationFrame(this.raf);
  }

  toggle() { this.playing ? this.pause() : this.play(); return this.playing; }

  setSpeed(s) { this.speed = s; }

  // step runs n ticks right away and redraws.
  step(n = 1) {
    for (let i = 0; i < n; i++) {
      if (this.beforeTick) this.beforeTick(this);
      this.sim.do({ op: "step", ticks: 1 });
    }
    this.acc = 0;
    this.refresh();
  }

  // run sends a command to the engine and redraws (for buttons: crash, partition, …).
  run(cmd) {
    const r = this.sim.do(cmd);
    this.refresh();
    return r;
  }

  refresh() {
    this.snap = this.sim.snapshot(this.logTail);
    this.view.update(this.snap);
    this.view.frame(this.acc);
    this.onTick(this.snap);
  }

  loop(now) {
    if (!this.playing) return;
    const dt = Math.min(250, now - this.last);
    this.last = now;
    if (this.visible) {
      this.acc += (dt * this.speed) / 1000;
      const whole = Math.floor(this.acc);
      if (whole > 0) {
        const n = Math.min(whole, 200);
        if (this.beforeTick) {
          for (let i = 0; i < n; i++) {
            this.beforeTick(this);
            this.sim.do({ op: "step", ticks: 1 });
          }
        } else {
          this.sim.do({ op: "step", ticks: n });
        }
        this.acc -= whole;
        this.snap = this.sim.snapshot(this.logTail);
        this.view.update(this.snap);
        this.onTick(this.snap);
      }
      this.view.frame(this.acc);
    }
    this.raf = requestAnimationFrame(this.loop);
  }
}
