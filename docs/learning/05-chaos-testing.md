# 05 — Chaos testing: trying to break it

## Deterministic simulation

The whole cluster (servers, network, disks, clients, clock) runs inside one deterministic
simulator. One number, the seed, decides every random choice: message delays, losses, which server
crashes when. Same seed, same run, every time. So when a run finds a bug, the seed *is* the bug
report: replay it and watch exactly what happened.

## What one chaos run does

1. Pick a fault profile (steady, churn, flaky network, slow storm, reboot storm).
2. For 3000 ticks, inject faults at random: crash and reboot servers, split the network in two,
   change the message-loss rate, while five clients send GET/PUT/APPEND/DELETE requests.
3. Remove every fault and require the cluster to recover: every client gets an answer, and a final
   write reaches every server.

Checked on every single step: Election Safety, State Machine Safety, Leader Completeness, Log
Matching (every 100 ticks). Checked at the end: linearizability of everything the clients saw.

## Testing the tests: classic mistakes

A checker that never fires might be checking nothing. So six well-known Raft bugs can be switched on
deliberately, and each must be caught:

| Mistake | Caught by |
| --- | --- |
| Commit an old-term entry because a majority has it (Figure 8) | Leader Completeness / State Machine Safety |
| Vote without checking the candidate's log | Leader Completeness |
| Accept entries from a leader of an older term | the node's own "never overwrite committed" guard |
| Don't save the vote to disk | Election Safety (two leaders in one term) |
| Leader answers reads from memory | Linearizability |
| No duplicate detection | Linearizability (an APPEND applied twice) |

Some are caught on almost every seed; Figure 8 took thousands of seeds to hit even once. That is
the point of running many seeds: real systems hit the rare timings eventually.

## Interview questions

1. **Why simulate instead of testing on real servers?** Determinism: every failure is replayable
   from a seed, and thousands of runs take seconds.
2. **How do you know your tests can find bugs?** Switch known bugs on and check they're caught.
3. **Why add APPEND to the workload?** PUT is idempotent, so applying a retry twice is usually
   invisible. APPEND isn't, so missing duplicate detection shows up.
4. **What does the linearizability checker need from a run?** For each client request: when it was
   sent, when (and whether) the answer arrived, and the answer.
