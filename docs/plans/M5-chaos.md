# M5 — Chaos runner, linearizability, classic mistakes

- [x] `sim.RunChaos`: one seeded scenario = random faults from a profile while 5 clients send
      GET/PUT/APPEND/DELETE on two keys; then heal everything and require recovery (every client
      answered, a final write reaches every server). Safety checked every step; linearizability
      (Porcupine) at the end.
- [x] Fault profiles, rotated by seed: steady, churn, flaky network, slow storm (slow messages →
      overlapping elections), reboot storm (crash and reboot constantly).
- [x] `cmd/chaos`: many seeds in parallel, summary line, JSON report, failing seed + replay command.
- [x] Classic mistakes, each caught: commit-old-terms (Figure 8), vote-ignores-log,
      accept-stale-leader, forget-vote, local-reads, no-dedupe. Pinned seeds in
      `TestClassicMistakesAreCaught`; the same seeds pass with the mistake off.
- [x] CI: race detector on the short suite, full suite, 1000 chaos seeds.

## What tuning the chaos run taught us

- **Figure 8 was invisible because of the no-op.** A new leader's no-op means every
  acknowledgement also covers a current-term entry, so "count old entries" never changes a commit
  decision. The mistake switch therefore also drops the no-op. Even then it needs rare timing:
  4 of 6000 seeds caught it.
- **"Follower trusts the leader's commit index" was harmless here**, because every AppendEntries
  carries the leader's whole missing suffix, so the commit index never exceeds what the message
  verified. Replaced with "accept-stale-leader", which does break things.
- **Forgetting the vote needs reboots in the middle of overlapping elections.** The steady profiles
  never caught it; the reboot-storm profile catches it in 38 of 6000 seeds.
- **Duplicate detection was invisible with PUT only**: applying `PUT x=5` twice usually looks like
  applying it once. APPEND (not idempotent) makes duplicates visible to the linearizability check.
