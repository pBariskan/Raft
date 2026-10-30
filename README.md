# Raft, built from scratch

The Raft consensus algorithm and a replicated key-value store in Go, written from the paper (no Raft
library), tested with a deterministic simulator and a chaos runner, runnable as real servers, and
compiled to WebAssembly for a website where you can break a live cluster and learn how it works.

- **Workplace** (`web/workplace.html`): crash servers, split the network, lose and delay messages,
  send requests, switch on a classic mistake, replay any chaos seed, watch the safety checks.
- **Course** (`web/course.html`): 17 modules from "why copies disagree" to linearizability and
  testing, each with live figures and questions.
- **Under the hood** (`web/under-the-hood.html`): chaos results, the six classic mistakes and how
  often each was caught, real-server benchmarks, architecture.

## What's in it

| Package | What it does |
| --- | --- |
| `raft/` | The algorithm: elections, replication, commit rule, persistence hooks. Pure and deterministic. |
| `kv/` | Key-value state machine with client sessions (a retried request is applied once). |
| `sim/` | Deterministic simulator, clients, safety checks, chaos runner, classic mistakes. |
| `check/` | Linearizability checking with [Porcupine](https://github.com/anishathalye/porcupine). |
| `storage/`, `transport/`, `server/`, `client/` | Real mode: write-ahead log with fsync, HTTP, the server loop. |
| `play/`, `wasm/` | The simulator as a session the browser drives, compiled to WebAssembly. |
| `cmd/` | `chaos` (runner), `raftd` (one server), `cluster` (local cluster with a prompt), `bench`. |
| `web/` | The site: plain HTML, CSS and JavaScript. |

## Run it

```sh
go test ./...                              # all tests, including 300 chaos seeds
go run ./cmd/chaos -seeds 1000             # chaos runner; -mistake NAME switches a bug on
go run ./cmd/cluster -n 5                  # five real servers on localhost; kill/start/put/get
scripts/serve-web.sh                       # the site on http://localhost:8080
scripts/site-data.sh                       # regenerate the numbers the site shows
```

Plans and learning notes written along the way are in `docs/`. `PROJECT.md` has the goals and the
roadmap.
