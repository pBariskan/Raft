# 02 — Log replication and the commit rule

Milestone M2. Interactive version: "02 Log replication" on the Raft Workshop learning page
(Stories A, B and C).

## The problem

A leader alone is useless; the point is to keep data. Raft keeps it as a **log**: a numbered list of
commands. Every server holds a copy, and if every server runs the same commands in the same order,
every server ends up with the same data (a *replicated state machine*). The leader's job is to make
all copies identical and to decide when a command is safe to run.

## How the cluster behaves

- Each entry has an **index** (its position), the **term** of the leader that created it, and the
  command. A new leader starts by writing an empty **no-op** entry in its own term.
- The leader sends **AppendEntries** to each follower: "put these entries right after entry #N of
  term T; I have committed up to #C". With nothing new to send, the same message is the heartbeat.
- **Consistency check.** A follower that doesn't hold entry #N with term T refuses. The leader then
  steps back (the refusal carries a hint: "my log ends at #K") and retries until the two logs agree
  on some entry. From there the follower deletes anything that conflicts and copies the leader's
  entries. A follower never deletes entries that already match, so a late, duplicated message
  can't undo newer progress.
- **Commit.** The leader marks an entry committed when a majority stores it *and* it is from the
  leader's current term; all entries before it are committed with it. Only then does the command run
  and the client hear "done".
- Followers learn the commit index from the leader's next message, but only up to the last entry
  that message actually checked against the leader's log.
- **Vote restriction.** A server refuses to vote for a candidate whose log is behind its own (older
  last term, or same last term and shorter).

## Why it is safe

- **Log Matching.** A leader creates at most one entry per index per term, and a follower only
  appends after a matching entry. So if two logs hold the same index with the same term, they are
  identical up to there.
- **Leader Completeness.** A committed entry is on a majority. A winner needs votes from a majority.
  The two majorities overlap, and the overlapping voter refuses any candidate missing that entry.
  So every leader holds every committed entry, and the follower repair (which only deletes what the
  leader doesn't have) never deletes a committed entry.
- **Why only the current term counts (Figure 8).** An old-term entry can sit on a majority and still
  be overwritten: a server holding a *newer-term* entry at that index can still win an election and
  replace it. Once an entry of the leader's own term is on a majority, no server with an older log
  can win, so everything up to it is safe. The no-op gives every new leader such an entry right away.

## Failure cases handled

- Followers that are slow, down, or lose messages catch up from the leader's next messages
  (`TestCommitsWithTwoFollowersDownAndCatchesThemUp`).
- With 3 of 5 servers down, nothing new commits at all (`TestNothingCommitsWithoutAMajority`).
- Leaders crash and change repeatedly; every committed value survives on every node, in order
  (`TestCommittedEntriesSurviveLeaderChanges`).
- An uncommitted entry can be lost. Seen in the chaos run: after a partition healed, two servers
  that had spent it running elections came back with terms 59 and 68, forced a new election, and the
  new leader's log replaced an entry the old leader had just accepted. That is correct Raft: the entry
  was never committed, so no client was told "done". Clients must retry (M4 makes retries safe).

## Checks in the simulator

On every step of every run: Election Safety, **State Machine Safety** (no two servers commit
different entries at the same index), **Leader Completeness** (a new leader holds every committed
entry). Every 100 ticks and at the end: **Log Matching**. The chaos test runs 300 seeds of crashes,
partitions and message loss while clients propose; the last run committed 13,005 client values with
no violations.

## What the tests caught

To check the checkers, classic mistakes can be switched on (see note 05). Two findings from M2/M5:

- Counting old-term entries (Figure 8) is hidden by the no-op: every acknowledgement also covers
  the new leader's no-op, so the bug never changes a commit decision. With the no-op removed, the
  chaos runner catches it, but only on rare timings (4 of 6000 seeds).
- A follower trusting the leader's commit index beyond what a message verified is harmless in this
  implementation, because every AppendEntries carries the leader's whole missing suffix. It would
  matter with empty heartbeats or size-limited batches. The unit test
  `TestFollowerCommitIsCappedByWhatTheMessageVerified` keeps the rule in place anyway.

## Interview questions

1. **When is an entry committed, and when does the client get its answer?** When the leader knows a
   majority stores it and it is from the leader's current term. The leader then applies it and
   answers. Before that the client waits; if the leader dies, the client must retry.
2. **A follower has extra entries the leader doesn't have. What happens?** The consistency check
   finds the last point where the logs agree; the follower deletes everything after it and copies
   the leader's entries. Safe, because those entries were never committed (Leader Completeness).
3. **Why does the vote depend on the candidate's log?** So that every leader already has every
   committed entry: committed entries are on a majority, and a candidate needs a majority of votes,
   so some voter has the entry and refuses a candidate that lacks it.
4. **Why can't a leader commit an old-term entry because a majority has it?** A server with a
   newer-term entry at that index could still win and overwrite it (Figure 8). Counting only
   current-term entries rules that out; the new leader's no-op makes it quick.
5. **Why does the consistency check compare terms, not just indexes?** Two logs can both have an
   entry #3 from different leaders. Same index *and* same term means same entry, and by induction
   the same history before it.
