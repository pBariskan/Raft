# M3 — Persistence + crash recovery

Goal: a crash wipes a server's memory; it reboots from what it saved and the cluster stays safe.

- [x] `Ready` carries `HardState` (term, vote, commit; only when changed) and `Entries` to persist
      (replacing storage from `Entries[0].Index`). Contract: persist, then send, then apply.
- [x] `Config.HardState` / `Config.Entries` restore a node; committed entries are replayed from
      index 1 so the state machine can be rebuilt.
- [x] Simulator: every node has a simulated disk; Crash wipes memory, Restart boots from the disk.
- [x] Tests: restart keeps term/vote/log, a restarted node refuses a second vote in the same term,
      whole-cluster restart keeps every committed value.

## Findings

- Mutation check: restarting without the saved vote, or never saving it, is caught by the unit test
  `TestRestartRestoresTermVoteLogAndCommit` but not by the 300-seed chaos run (a double vote needs
  a crash and reboot in the middle of a close election). Carried to M5 with the Figure-8 item.
- Unsynced writes lost on crash are not modelled yet: the simulator syncs before sending, as the
  contract requires. M5 adds a "send before sync" classic mistake that a crash can expose.
