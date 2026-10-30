# 04 — The key-value store and its clients

## What runs on top of Raft

Raft only agrees on a log. The key-value store gives the log a meaning: each committed entry is a
command (`PUT x=5`, `GET x`, `DELETE x`) and every server applies the committed commands in order to
its own copy of the data. Same commands, same order, same data everywhere.

**Reads go through the log too.** A leader that answered `GET` from its own memory could be a stale
leader cut off by a partition (lesson 01b) and return an old value. Putting the read in the log
means it is answered only after a majority agreed on its position. (Faster read paths, ReadIndex
and leases, are stretch goals.)

## How a client talks to the cluster

1. The client sends its request to the server it believes leads.
2. A follower answers "not the leader, try S3" (if it knows), and the client goes there.
3. The leader appends the command to its log. When the entry is committed and applied, the leader
   replies with the result.
4. If no answer comes in time (server down, message lost, leader replaced), the client sends the
   **same** request to another server.

## The retry problem, and client sessions

Step 4 is dangerous. The first attempt may have been committed even though the answer was lost.
The retry is then committed a second time. For `PUT x=5` that is harmless; for "add 10 to my
balance" or for a `PUT` that another client has since overwritten, applying it twice is wrong.

Fix: each client has an ID and numbers its requests 1, 2, 3… (one at a time), reusing the number
for a retry. Every server remembers, per client, the last number it applied and its answer. A
command with a number it has already applied is skipped, and the stored answer is returned. Because
this table is built by applying the log, every server has the same table.

In the simulator the retry path is exercised all the time: one 40-seed failure run skipped 30
duplicate commands. Switching duplicate detection off makes the linearizability check fail.

## Linearizability: what "correct" means for clients

The store must behave as if there were **one copy** of the data and every request took effect at
one instant between being sent and being answered. Concretely: once a write has been answered,
every later read sees it (or something newer), and reads never go back in time.

The simulator records every request with its send time, answer time and answer, and the Porcupine
checker searches for an order of operations that explains all answers. If none exists, the history
is not linearizable. Requests that were never answered are treated honestly: an unanswered write
may or may not have happened, an unanswered read is ignored.

## Interview questions

1. **Why do reads go through the log?** A leader may have been replaced without knowing; answering
   from its own memory could return stale data.
2. **A client's request timed out. Did it happen?** Maybe. That's why it retries with the same
   request number, and the servers skip it if it was already applied.
3. **Why must the duplicate table live in the replicated state machine, not in the leader's
   memory?** The next leader must know what was already applied; it learns that by applying the
   same log.
4. **What is linearizability?** Every operation appears to take effect at a single instant between
   its call and its response, consistent with one copy of the data.
