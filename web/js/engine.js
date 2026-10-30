// Loads the real Go simulator (compiled to WebAssembly) and wraps it in a small class.
// Everything the page shows comes from here; nothing about Raft is re-implemented in JavaScript.

const base = new URL("../engine/", import.meta.url);
let loading = null;

function loadScript(src) {
  return new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = src;
    s.onload = resolve;
    s.onerror = () => reject(new Error("could not load " + src));
    document.head.appendChild(s);
  });
}

export function loadEngine() {
  if (!loading) {
    loading = (async () => {
      await loadScript(new URL("wasm_exec.js", base).href);
      const go = new window.Go();
      const url = new URL("raft.wasm", base).href;
      let instance;
      try {
        ({ instance } = await WebAssembly.instantiateStreaming(fetch(url), go.importObject));
      } catch {
        // Some static servers send the wrong content type; fall back to bytes.
        const bytes = await (await fetch(url)).arrayBuffer();
        ({ instance } = await WebAssembly.instantiate(bytes, go.importObject));
      }
      go.run(instance);
      return window.raftEngine;
    })();
  }
  return loading;
}

// Sim is one simulated cluster.
export class Sim {
  constructor(engine, options) {
    this.engine = engine;
    this.options = { ...options };
    const r = JSON.parse(engine.create(JSON.stringify(this.options)));
    if (r.error) throw new Error(r.error);
    this.id = r.id;
  }

  static async create(options) {
    return new Sim(await loadEngine(), options);
  }

  do(cmd) {
    const r = JSON.parse(this.engine.do(this.id, JSON.stringify(cmd)));
    if (r.error) throw new Error(r.error);
    return r.result || {};
  }

  snapshot(logTail = 0) {
    return JSON.parse(this.engine.snapshot(this.id, logTail));
  }

  // peek is snapshot without taking the pending events.
  peek(logTail = 1) {
    return JSON.parse(this.engine.peek(this.id, logTail));
  }

  destroy() {
    this.engine.destroy(this.id);
  }
}

// A small seeded random generator for page-side choices (so shared links replay the same way).
export function rng(seed) {
  let s = (seed >>> 0) || 1;
  return () => {
    s ^= s << 13; s >>>= 0;
    s ^= s >>> 17;
    s ^= s << 5; s >>>= 0;
    return s / 4294967296;
  };
}
