# M1 — Simulator + leader election

Goal: five nodes elect exactly one leader, re-elect after the leader fails, and never have two
leaders in the same term, all inside a deterministic, seeded simulator.

Done when: the tests below pass on many seeds, `go test ./...` is green in CI, and learning note
01 is written.

## Behaviour in scope

- Follower, candidate, leader. Terms. Randomized election timeout in
  `[ElectionTick, 2*ElectionTick)` ticks; leader heartbeat every `HeartbeatTick` ticks.
- RequestVote: one vote per term, first come first served; stale-term requests rejected.
  (The "candidate's log is at least as up-to-date" check arrives with logs in M2.)
- Heartbeats are empty AppendEntries; a reply carries the follower's term so a stale leader learns
  it was replaced. (Entries, the consistency check and commit arrive in M2.)
- Any message with a higher term turns the receiver into a follower of that term.

## Out of scope (later milestones)

- Logs, commit (M2). Persistence and real restarts (M3): in M1 a "crash" stops the node and a
  "restart" resumes it with its memory intact, like a long pause. M3 replaces this with a restart
  from the simulated disk.

## Steps

- [x] Plan (this file).
- [x] Unit tests for the election rules in `raft/` (written before the code).
- [x] `raft/`: pure state machine driven by `Tick()` and `Step()`, randomness injected.
- [x] `sim/`: virtual clock (ticks), network with random delay, drop rate, partitions, crash and
      restart, seeded from one number; election-safety checker run every tick.
- [x] Simulation tests: one leader emerges, re-election after leader crash, minority partition
      cannot elect, randomized chaos run with no two leaders in a term, same seed → same run.
- [x] Learning note `docs/learning/01-leader-election.md`.

## Choices *(Claude's call)*

- Time unit: the tick. `ElectionTick = 10`, `HeartbeatTick = 1`, message delay 1–3 ticks
  (the same ratios etcd uses by default; with a 10 ms tick that is a 100–200 ms timeout).
- `Step` and `Tick` queue outgoing messages; the caller drains them with `Ready()`. This is the
  "driven from outside" shape `PROJECT.md` asks for, and M3 adds the state to persist to `Ready`.
