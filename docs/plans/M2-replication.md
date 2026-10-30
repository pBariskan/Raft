# M2 — Log replication + commit rule

Goal: the leader copies a log of commands to every follower; an entry becomes committed once it is
safely on a majority, and a committed entry is never lost or changed, under any mix of crashes,
partitions and message loss in the simulator.

Done when: the tests below pass, the chaos run checks the log safety properties on every seed,
`go test ./...` is green in CI, and learning note 02 is written.

## Behaviour in scope

- Log entries carry index, term and data. `Propose(data)` on the leader appends an entry;
  on anyone else it returns `ErrNotLeader`.
- AppendEntries carries `prevIndex`/`prevTerm`, the entries, and the leader's commit index. The
  follower rejects when it has no entry at `prevIndex` with `prevTerm`; on a match it deletes
  conflicting entries (same index, different term) and appends the rest. It never deletes entries
  that match, so a late, reordered message cannot undo newer progress.
- On a rejection the follower sends a hint (its last index) and the leader moves `next` back.
- Commit rule: the leader advances its commit index to N when a majority has `match >= N` **and**
  entry N is from the current term. Followers take `min(leaderCommit, last new entry)`.
- A new leader appends an empty no-op entry, so entries from earlier terms get committed without
  waiting for a client.
- Election restriction: vote only for a candidate whose log is at least as up to date
  (higher last term, or same last term and at least as long).
- `Ready` returns newly committed entries, in order, exactly once.

## Safety checks in the simulator (every run, every seed)

- Election Safety (from M1).
- State Machine Safety: no two nodes ever commit different entries at the same index.
- Leader Completeness: a node that becomes leader holds every entry committed so far.
- Log Matching: if two logs have an entry with the same index and term, they are identical up to
  that index (checked every 100 ticks and at the end of a run).

## Out of scope

- Persistence and real restarts (M3); restart is still a pause.
- The key-value store and clients (M4). The simulator proposes opaque values (`v1`, `v2`, …).
- Flow control and batching limits (not needed for correctness).

## Steps

- [x] Plan (this file).
- [x] Lesson 02 with storyboards on the learning page.
- [x] Unit tests in `raft/` for each rule above, written before the code.
- [x] `raft/`: log, AppendEntries, commit index, no-op, election restriction, committed entries in
      `Ready`.
- [x] `sim/`: proposals, the three new safety checks, chaos test that proposes while it breaks
      things; liveness check that every proposal accepted after healing gets committed.
- [x] Learning note 02.

## Findings

- The chaos run catches a missing vote restriction but not the Figure-8 commit mistake or an
  uncapped follower commit index; unit tests catch all three. Carried to M5: tune the fault
  schedule until the chaos run catches them too.
