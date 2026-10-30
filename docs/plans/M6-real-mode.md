# M6 — Real mode

The same Raft core (`raft/`, untouched) driven by real time, a real disk and a real network, so the
project is a system you can run, not only a simulation.

- [x] `storage/`: write-ahead log (one length-prefixed, CRC32-checksummed record per entry) plus a
      hard state file written via temp file, fsync, rename, directory fsync. Replacing entries
      truncates the log file. A torn or corrupt tail is cut off on open. Tests: reopen, replace,
      torn tail, corrupt record, commit capped at the log length.
- [x] `transport/`: messages as JSON over HTTP POST `/raft`, one sender per peer, one batch in
      flight at a time, earlier AppendEntries to the same peer dropped in favour of the latest.
      Best effort: a failed POST is dropped. Tests: delivery, coalescing, dead peer never blocks.
- [x] `server/`: one goroutine owns the node; each loop handles a tick / peer messages / client
      requests, drains whatever else is queued (group commit), then Ready: save + fsync, send,
      apply, answer waiting clients. Waiters get "not leader" when leadership moves on. A disk
      error stops the node (fail-stop).
- [x] Client API: `POST /kv` with client ID + sequence number (retries applied once, 421 + leader
      address from a follower), `/kv/{key}` shortcuts for curl (307 to the leader), `/status`.
- [x] `client/`: follows leader hints, rotates servers on errors, resends the same sequence number.
- [x] Tests (real sockets, disks, time; outcomes only): put/get/append/delete, leader crash then
      restart from disk and catch up, whole-cluster restart keeps data, no writes without a
      majority, a request sent three times is applied once, shortcuts redirect to the leader.
- [x] `cmd/raftd` (one node), `cmd/cluster` (local processes, kill/start/put/get prompt),
      `cmd/bench` (3 and 5 nodes × 1/8/32/128 clients, fsync on/off, leader-crash stall → JSON).
- [x] Learning note 06; roadmap ticked; CLAUDE.md commands.

## Decisions

- **Same address for peers and clients.** One port per node keeps the launcher and the leader
  hint simple (the hint is the address clients should use).
- **Reads go through the log**, as in the simulator: slower than lease or ReadIndex reads, but
  linearizable with no clock assumptions. A faster read path is a possible later extension.
- **Real-mode tests are not seeded schedules.** Real time and sockets can't be replayed; the
  simulator (M2–M5) owns the deterministic, seed-replayable testing. Real-mode tests check
  outcomes with generous deadlines and were run 10× under the race detector before landing.
- **Client IDs are chosen by clients.** A production system would register clients through the
  log; here the client package takes an ID, and the curl shortcuts use a reserved high range.
- **Sessions are never cleaned up.** Fine for a demo; a long-running system would expire them.
