# 06 — Real mode: disks, networks and wall clocks

Up to M5 the cluster lived inside a simulator. In real mode the *same* Raft logic runs as separate
processes that talk over HTTP and write to real disks. Nothing in the Raft rules changes; what
changes is everything around them.

## What a node does, over and over

Each node has one loop. Every turn it takes whatever has arrived (a clock tick, messages from
other servers, client requests), lets Raft react, and then does what Raft asks, always in this
order:

1. **Write to disk and wait for the disk to confirm** (fsync): new log entries, the current term,
   who it voted for.
2. **Then send messages** to the other servers.
3. **Then apply committed commands** to the key-value store and answer the clients waiting for
   them.

The order is the whole point. A "yes, I stored it" or "you have my vote" sent *before* the disk
confirmed is a promise that a power cut can break, and Raft's safety argument assumes those
promises hold.

## The disk

The log is a write-ahead file: each entry is appended as a record with its length and a checksum.
If the machine dies halfway through a write, the last record is incomplete; on restart the
checksum doesn't match, so that record is cut off. That's safe, because it was never confirmed to
anyone. The term and vote live in a small file that is replaced atomically (write a new file,
sync it, rename it over the old one), so it is always the old version or the new one, never half
of each.

**Group commit.** fsync is slow (milliseconds), but one fsync can cover many entries. When many
clients write at once, the loop picks up all waiting requests first and syncs once for all of
them. That's why throughput grows with the number of clients while each request's latency only
grows a little.

## The network

Messages travel as HTTP requests. Delivery is best effort: if a server is down the message is
simply lost. That's fine, because Raft already repeats what matters: the leader resends
unacknowledged entries with every heartbeat, and candidates retry elections. If several updates
for the same follower pile up while one is in flight, only the newest is sent; it carries
everything the older ones did.

## Clients in the real world

A client picks a unique ID and numbers its requests. It can start at any server: a follower
answers "not leader, try this address", a dead server doesn't answer at all, and the client moves
on. If it times out it **sends the same request number again**, possibly to a different server,
and duplicate detection makes sure the write happens once. When a leader loses its leadership,
it tells everyone waiting on it "not leader", because it can no longer promise their command will
commit.

## What crashes look like

- **A follower crashes:** the leader carries on with the rest. When the follower comes back it
  reads its log from disk, then the leader sends what it missed.
- **The leader crashes:** writes stall until a follower's election timer runs out (100–200 ms
  with the default settings), an election finishes, and clients find the new leader. The
  benchmark measures this stall.
- **A majority is down:** nothing can commit. Clients wait; no wrong answer is ever given.
- **Everything restarts:** every node reloads its log from disk, a leader is elected, and all
  committed data is still there.

## Interview questions

1. **Why must a node fsync before replying to AppendEntries or a vote?** The reply is a promise
   the leader counts towards a majority (or a candidate counts as a vote). If it vanishes in a
   crash, a "committed" entry may exist on fewer than a majority, or a node may vote twice in a
   term.
2. **How do you make fsync affordable?** Batch: one fsync for everything that arrived since the
   last one (group commit), and pipeline so the network and disk work in parallel.
3. **Why is losing messages acceptable?** Raft's messages are idempotent and repeated: a lost
   heartbeat or append is resent, a lost vote just means another election.
4. **What determines how long writes stall when the leader dies?** Mainly the election timeout,
   plus one election round trip and the time for clients to find the new leader. Shorter timeouts
   mean faster failover but more needless elections when the network is slow.
5. **What does "fail-stop" mean here?** A node that can't write its disk stops instead of carrying
   on, because carrying on would mean making promises it hasn't stored.
