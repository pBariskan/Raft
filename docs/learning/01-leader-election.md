# 01 — Leader election

Milestone M1. Interactive version: the "Lider seçimi" section of the Raft Atölyesi learning page.

## The problem

Five servers want to agree on one sequence of commands. The simplest way is to let one server, the
leader, decide the order and have everyone else copy it. The hard part is not picking a leader; it
is guaranteeing that two servers never both act as leader at the same time, even when messages are
delayed or lost and servers crash.

## How the cluster behaves

Every server is in one of three roles: **follower**, **candidate** or **leader**. Time is split into
numbered **terms**. Each term starts with an election and has at most one leader.

1. Everyone starts as a follower in term 0, with an **election timeout** drawn at random (here 10–19
   ticks).
2. A follower that hears nothing from a leader before its timeout runs out becomes a candidate: it
   increments its term, votes for itself and sends **RequestVote** to everyone.
3. Each server grants **at most one vote per term**, to the first candidate that asks. A request
   from an older term is refused.
4. A candidate that collects votes from a **majority** (3 of 5) becomes leader and immediately sends
   heartbeats (empty AppendEntries) so the others don't start elections of their own.
5. A follower that receives a heartbeat from the current term's leader resets its timer. A
   candidate that receives one gives up: someone else won this term.
6. **Any server that sees a higher term than its own adopts it and becomes a follower.** This is how
   a stale leader or candidate learns it has been overtaken.

## Why there can never be two leaders in one term

Winning needs 3 votes out of 5. Two winners in the same term would need 6 votes, but there are only
5 voters, so at least one server would have voted twice in that term. Rule 3 forbids that. This
"any two majorities overlap" argument comes back in every later part of Raft.

## Failure cases handled

- **Leader crashes.** Heartbeats stop, the first follower to time out wins the next term.
  When the old leader returns it sees the higher term in the first message it receives and steps
  down (`TestReelectionAfterLeaderCrash`).
- **Split vote.** Two candidates time out together and split the votes; nobody reaches 3. Their
  timers are re-randomized, so the next round almost always has a single early candidate. Without
  randomness the cluster can split forever (try it on the learning page).
- **Network partition.** The side with 3 servers elects a new leader in a higher term. The side with
  2 servers can never reach 3 votes, so it never elects anyone; its followers keep starting
  elections and their term keeps climbing. The old leader on that side still thinks it leads, but
  it cannot reach a majority. On healing, the higher term wins (`TestMinorityPartitionCannotElect`).
- **Too many failures.** With 3 of 5 servers down no leader can be elected at all
  (`TestMajorityNeededToElect`). Raft chooses to stop rather than risk two leaders. A 5-server
  cluster tolerates 2 failures; a 3-server cluster tolerates 1.
- **Lost and duplicated messages.** A retried vote request from the same candidate gets the same
  answer; a duplicated vote is not counted twice.

## Design choices in this code

- The Raft core never reads a clock or uses global randomness. Time arrives as `Tick()` calls and
  the random timeouts come from a seeded source the caller injects. That makes every run
  replayable: the simulator takes one seed and produces the exact same run every time
  (`TestSameSeedSameRun`).
- Timeouts are 10–19 ticks and heartbeats every tick, the same ratios etcd uses. The rule of thumb
  from the paper: broadcast time ≪ election timeout ≪ time between failures.
- The simulator checks Election Safety after every single step of every node, over the whole run,
  not only at the end.
- Not yet handled: the vote also depends on how up to date the candidate's log is (M2), and the term
  and vote must be on disk before replying (M3). Until M3 a "restart" in the simulator is really a
  pause: the node keeps its memory.

## Bugs along the way

No real bug was hit in M1. To check that the chaos test can actually catch a broken election, the
one-vote-per-term rule was switched off by hand for one run: the checker reported "term 21 has two
leaders, 5 and 1" on seed 1 within 1274 ticks. That switch becomes a permanent "classic mistake"
demo in M5.

## Interview questions

1. **Why are election timeouts randomized?** So servers rarely become candidates at the same
   moment. With equal timeouts they all vote for themselves, nobody gets a majority, and it repeats
   forever.
2. **5 servers, 2 crash. Can the cluster elect a leader? What if 3 crash?** With 3 left, yes: 3 is a
   majority of 5. With 2 left, never: Raft stays unavailable instead of risking inconsistency.
3. **Why can't there be two leaders in one term?** A leader needs a majority; two majorities of the
   same cluster share at least one server, and that server votes only once per term.
4. **The network splits and the old leader ends up on the 2-server side. What happens?** The 3-server
   side elects a new leader in a higher term. The old leader keeps believing it leads but can reach
   no majority, so (from M2 on) it cannot commit anything. When the network heals it sees the higher
   term and steps down.
5. **Why must a server remember its term and vote across a restart?** If it forgot, it could vote
   for a second candidate in a term it already voted in, which breaks the "one vote per term"
   argument and allows two leaders. That is why term and vote are persisted before replying (M3).
