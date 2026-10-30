# M4 — Key-value store + client sessions

- [x] `kv/`: commands (JSON in log entries), store with PUT/GET/DELETE, sessions (client ID +
      sequence number) that skip duplicates and return the original answer.
- [x] `check/`: Porcupine model for the store, partitioned by key; unanswered writes kept open,
      unanswered reads dropped.
- [x] `sim/`: clients outside the cluster network, leader redirects, timeouts and retries; servers
      propose, remember waiting requests by term, answer after applying; store rebuilt after reboot.
- [x] `sim.Workload`: random requests over a few keys, for tests and the chaos runner.
- [x] Tests: healthy cluster answers and is linearizable; leader hint followed; failures with
      message loss stay linearizable and exercise the duplicate path; chaos run checks every seed.
- [x] Mutation check: duplicate detection switched off → linearizability failure.
