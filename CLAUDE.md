# Raft

Raft consensus + replicated key-value store in Go, with a website where visitors break a live
cluster in the browser. Built by Poyraz as a portfolio project and to learn distributed systems.

**Read `PROJECT.md` first, every session.** It holds the why, the goals and anti-goals, the
architecture, the website plan and the roadmap. The roadmap checkboxes are the progress tracker:
start at the first unticked milestone and tick items as they land. The agreed site structure is in
`docs/mockups/site-sections.html` (structure agreed, visuals rough).

## Talking to Poyraz

- Lessons are in English (Poyraz's request, 2026-10-24): topic explanations, check questions and
  the learning page. Short chat can follow the language Poyraz writes in. Code, comments, docs,
  commit messages and site text are in English.
- Since 2026-10-24 (15:05) Poyraz wants the whole project built first, then learns from the site's
  Course. Don't stop per milestone for lessons or check questions; still test-first and still write
  learning notes (they feed the Course). Plan: `docs/plans/full-build.md`.
- Poyraz wants to learn **how the system works, not the code**. Explain behaviour (servers,
  messages, failures, why a rule exists), never walk through Go line by line, no Go tutorials.

## Where things stand

2026-10-30: M0–M6 done (Raft core, key-value store, linearizability checker, deterministic
simulator, chaos runner with six classic mistakes, real mode with disk/HTTP/benchmarks; learning
notes 01–06). Next step: M7 (WASM bridge), then the site.
Visual explanations for Poyraz live on a
separate learning page ("Raft Workshop" artifact), not in this repo. For each milestone, write its plan in `docs/plans/` first, then build. Keep this
section current in a line or two as milestones land.

## Working rules

Poyraz's global preferences, copied here because cloud sessions don't see their local config.

1. Before starting, make sure the finish line is clear; ask if Poyraz hasn't said what "done"
   looks like.
2. State the approach in one message, then start. When a step doesn't need Poyraz, keep going; put
   status notes in the same message as the next action.
3. Stop and ask only when you can't continue without Poyraz, or before anything destructive or
   outward-facing: deleting data, force-pushing, pushing, deploying, spending money on paid APIs,
   or changing anything outside this repository.
4. For work with more than a few steps, keep the checklist in a plan file (`docs/plans/`, plus the
   roadmap in `PROJECT.md`) and tick items as you go.
5. When a bug occurs, first write a test that reproduces it, then fix it until the test passes.
   Real bugs also go into `docs/bugs.md` (symptom, seed or test name to replay, root cause, fix
   commit).
6. End every run with these headings, in this order, skipping a heading only when it is empty:
   - **Senden beklenen** — decisions or approvals needed from Poyraz.
   - **Değişenler** — what changed.
   - **Doğrulanmadı** — what couldn't be confirmed, and what was checked.
   - **Uç durumlar ve testler** — after code: possible edge cases and tests that cover them.
   - **Ders** — after a fix: what went wrong, and the plan to avoid it next time.
   - **Commit mesajı** — after a code change: `<type>(<scope>): <short description>`, under 72
     characters, in a code block.

## Code rules

- `raft/` stays pure and deterministic: no goroutines, no `time`, no I/O, no global randomness.
  Time comes from `Tick()`, randomness from an injected seeded source.
- Every test and simulation is seeded and reproducible; a failing chaos run prints its seed and
  the command to replay it.
- Do not import or copy etcd/raft or any other Raft library. Reading them for ideas is fine.
- Never put a number or a story on the site that a real run didn't produce.
- If `go` is missing in the environment, install the current stable Go toolchain.

## Commands

Add commands here as they come to exist (tests, chaos runner, local cluster, WASM build, site).

- `go test ./...` — all tests (from M0 on).
- `gofmt -l . && go vet ./... && go test -race ./...` — what CI runs (`.github/workflows/test.yml`).
- `go test ./sim -run TestChaosSafety -sim.seed=N` — replay one chaos seed.
- `go run ./cmd/chaos -seeds 1000 [-mistake NAME] [-json out.json] [-stop]` — chaos runner;
  `-from N -seeds 1 -v` replays seed N.
- `go test -short ./...` — quicker run (fewer chaos seeds).
- `go run ./cmd/cluster -n 3 [-fresh]` — real cluster on this machine (raftd processes on ports
  7001+, data in `data/`); prompt: `status`, `kill N`, `start N`, `put K V`, `get K`, `quit`.
- `go run ./cmd/raftd -id 1 -cluster 1=127.0.0.1:7001,2=…` — one node; `curl -L -X PUT
  127.0.0.1:7001/kv/x -d hi`, `curl -L 127.0.0.1:7001/kv/x`, `curl 127.0.0.1:7001/status`.
- `go run ./cmd/bench -json web/data/bench.json` — throughput, latency, leader-crash stall.
