# M7 — WebAssembly bridge

The browser runs the real simulator (the same `raft/`, `kv/`, `sim/` and `check/` code the tests
and chaos runner use), compiled to WebAssembly. No JavaScript re-implementation of Raft.

- [x] `sim` view layer (`sim/view.go`): messages in flight with send/arrival ticks and contents,
      client states, and an event stream (role changes, commits, lost and dropped messages,
      crashes, restarts, partitions, network changes, client calls/retries/redirects/answers,
      duplicates, safety violations). Recording is off by default and never changes a run
      (`TestRecordingDoesNotChangeTheRun` compares 20 seeds with and without it).
- [x] `raft.Status` exposes the election timer (elapsed and randomized timeout) so the site can
      draw it.
- [x] `sim.SetDelay` (message latency can change mid-run), `sim.MistakeSwitches` shared by the
      chaos runner and the playground.
- [x] `play/`: one session = one cluster plus optional background traffic. Commands: step, until
      (leader / idle), crash, restart, partition, heal, drop, delay, client, submit, traffic,
      check (Porcupine on the session's history). `Snapshot(logTail)` returns nodes (role, term,
      vote, timer, commit, log tail with decoded commands, store contents, partition side),
      flights, clients, events, stable leader, violation.
- [x] `wasm/`: `raftEngine.create / do / snapshot`, all JSON. `scripts/build-web.sh` builds
      `web/engine/raft.wasm` and copies Go's `wasm_exec.js`; CI builds it.
- [x] Smoke test in headless Chromium: created a 5-node cluster, elected a leader in 15 ticks,
      ran 5000 ticks of traffic (about 83 ms per 1000 ticks), snapshot, Porcupine said
      linearizable over 1244 requests.

## Notes for the site

- The page animates between ticks itself: a flight's position is
  `(now + fraction − sent_at) / (deliver_at − sent_at)` along the line from sender to receiver.
- Snapshots get expensive only when thousands of events piled up; take one per rendered tick.
- The `.wasm` is about 3.5 MB (around 1 MB gzipped); load it once, show a skeleton meanwhile.
