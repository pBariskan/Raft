# Full build: engine first, then the Workplace + Course site

Decided by Poyraz on 2026-10-24 (15:05): stop the milestone-by-milestone teaching loop. Build the
whole project first, including the website. The site has two main areas:

1. **Workplace** — a simulation screen where anyone can run any scenario with any intervention.
2. **Course** — teaches how the system works step by step, each step building on the last, with
   good visuals and multiple-choice questions to move forward. Poyraz learns the project from it.

The design must not look AI-generated.

## Finish line

- Engine complete: persistence and crash recovery, key-value store with client sessions,
  linearizability checking, chaos runner with "classic mistakes", real mode with benchmarks, WASM.
- Site in `web/` with Landing, Workplace, Course and an "Under the hood" page (chaos results,
  benchmarks, architecture). Workplace and Course both run the real Go code compiled to WebAssembly.
- Works on phone width. All numbers on the site come from real runs (generated JSON).
- CI builds and tests everything. Deploying to GitHub Pages waits for Poyraz's go-ahead (it is
  public and needs a repository setting).

## Engine steps

- [x] M0 setup, M1 election, M2 replication + commit rule.
- [x] M3 persistence: `Ready` carries hard state (term, vote) and entries to persist; nodes restart
      from storage; the simulated disk loses unsynced writes on a crash; messages are sent only
      after persisting. Tests: restart keeps vote, no double vote after restart, crash before sync.
- [ ] M4 key-value store: PUT/GET/DELETE through the log, client sessions (client id + sequence
      number) so a retry is applied once, reads through the log, simulated clients that retry and
      follow leader redirects.
- [ ] M5 chaos runner: `cmd/chaos` over many seeds, invariant checks every step, Porcupine
      linearizability check per run, failing seed printed for replay, JSON summary for the site,
      fixed seed set in CI. "Classic mistakes" switches in the core, each caught by the checkers
      (includes tuning so the Figure-8 mistake is caught, see learning note 02).
- [ ] M6 real mode: `storage/` (write-ahead log + hard state, fsync), `transport/` (HTTP between
      processes), `cmd/raftd`, `cmd/cluster` launcher, `cmd/bench` writing JSON for the site.
- [ ] M7 WASM bridge: `wasm/` exposes the simulator to JavaScript (step, crash, partition, drop
      rate, latency, client commands, full state snapshot, event stream, checker status).

## Site steps

- [ ] Visual identity (type, colour, layout system) decided before any page is built; reviewed
      against the "AI look" list.
- [ ] Workplace: cluster view, per-server logs, controls (crash/restart, drag-to-partition, loss,
      latency, speed, pause/step, seed), client panel, event feed, term timeline, live safety
      checks, preset scenarios, shareable seed.
- [ ] Course: modules that build on each other, each with short explanation steps, visuals driven
      by the real engine (scripted scenarios), and multiple-choice questions with explanations;
      progress saved in the browser.
- [ ] Landing and Under the hood (chaos results and bugs from real data, benchmarks, architecture).
- [ ] Phone-width pass, accessibility pass.
- [ ] GitHub Actions: build WASM + site; Pages deploy workflow ready (enabled after approval).

## Course outline (draft)

1. Why one machine isn't enough: replication, and why copies disagree.
2. Majorities: why 3 of 5, how many failures a cluster survives.
3. Roles and terms.
4. Elections: timeouts, votes, one vote per term.
5. Split votes and why timeouts are random.
6. Network partitions: islands, stale leaders, higher term wins.
7. The log: index, term, command; the replicated state machine.
8. AppendEntries and the consistency check.
9. Committing: majority, applying, answering the client.
10. Repairing followers.
11. The vote restriction and leader completeness.
12. Figure 8: why only current-term entries are counted; the no-op.
13. Persistence: what must hit disk before replying, and crash recovery.
14. Clients: redirects, retries, duplicate detection.
15. Linearizability: what "correct" means for a client.
16. Testing it: deterministic simulation, chaos, checkers, the classic mistakes.
17. From simulation to real servers: storage, network, benchmarks.
