# 03 — Persistence and crash recovery

## The problem

A crash wipes a server's memory. If it forgot what it promised before the crash, it could break
the promises the safety arguments rely on: "I vote once per term" and "I store the entries I told
the leader I store".

## What must be on disk, and when

- **Term and vote.** Before replying to a vote request. Otherwise: vote for A in term 7, crash,
  reboot with no memory of the vote, vote for B in term 7. Two candidates can now both reach a
  majority in the same term.
- **Log entries.** Before telling the leader "I have them". The leader counts that reply toward
  a majority and may commit and answer the client. If the entries were only in memory and the
  follower crashed, the "majority" that justified the commit no longer exists.
- **Commit index.** Optional for safety (a rebooted server can relearn it from the leader), but
  saving it lets a server rebuild its state machine immediately after a reboot.

The node never touches the disk itself. Each `Ready` lists what to save; the caller saves and syncs
it, and only then sends the messages in the same `Ready`. The state machine is not saved at all: on
reboot, committed entries are replayed from index 1 to rebuild it (snapshots, a stretch goal, would
shorten that replay).

## How the cluster behaves

- A rebooted server starts as a follower in its saved term, with its saved vote, knowing no leader.
  It hears the current leader's next heartbeat (or a higher term) and rejoins.
- If every server crashes at once, nothing committed is lost: committed entries are on a majority of
  disks, and the election rules guarantee the next leader has them
  (`TestWholeClusterRestartKeepsCommittedData`).

## Interview questions

1. **What does a Raft server write to disk, and when?** Current term, vote, and new log entries,
   synced before sending any reply that depends on them.
2. **What breaks if the vote isn't persisted?** A rebooted server can vote twice in one term, which
   can produce two leaders in that term.
3. **What breaks if log entries are acknowledged before they are on disk?** The leader may commit
   based on copies that vanish in a crash, and a committed entry can then be lost.
4. **Why isn't the state machine saved?** It can be rebuilt by replaying the committed log. Real
   systems add snapshots so the replay doesn't grow forever.
