# Raft — project scope and design

Date: 2026-10-24 · Status: agreed in conversation with Poyraz (brainstorm session, 2026-10-24).
This document is the single source of truth for what this project is and why. The roadmap at the
bottom is the progress tracker: tick items as they land.

Decided by Poyraz: Raft + key-value store; the visual website with the six sections below; no
dependence on users or publishing; Go as the language; no fixed deadline; more than 10 hours a
week; they want to learn how the system works, not walk through the code. Anything marked
*(Claude's call)* was decided by Claude during setup and can be changed.

## Why this project exists

Poyraz is a 2nd-year CS & AI student at IE University (Madrid), an AI engineer at Muse Istanbul
(AI video pipelines) and technical co-founder of Brand AI. Their portfolio
(https://pbariskan.github.io, repo `pBariskan/pBariskan.github.io`) shows five shipped products:
Brand AI, Viesly, and three mobile games. Every one of them is a product for end users.

They want a new project, built with vibe coding, that does two things at once:

1. **Impresses the person reviewing their portfolio during a job search.** The target is software
   engineering internships (and SWE roles in general). Reviewers are engineers and hiring
   managers, who are impressed by depth: a hard system that works, is tested hard, and whose
   design the author can explain.
2. **Teaches Poyraz a new field for real.** The field chosen is distributed systems: consensus,
   replication, failure handling.

The gap this fills: the portfolio has products and AI, but nothing that shows depth in a classic
software engineering area.

## What it is

A from-scratch implementation of the **Raft consensus algorithm** with a **replicated key-value
store** on top, plus a **website where visitors can watch a live cluster and break it
themselves**: kill servers, split the network, drop messages, and see the cluster recover without
losing data.

The same Raft code runs in two modes:

- **Real mode:** 3–5 separate processes on one machine talking over the network, writing to disk.
  Used for benchmarks and to prove it is a real system, not a toy animation.
- **Simulated mode:** the identical code compiled to WebAssembly, running in the browser against
  a simulated network and clock. This powers the website, so the demo needs no server.

## Goals

- A correct Raft: leader election, log replication, commit rule, persistence and crash recovery,
  as described in the extended Raft paper (Figure 2 is the contract).
- A key-value store on top (`PUT`, `GET`, `DELETE`) with linearizable semantics and duplicate
  request detection (client id + sequence number), so a retried request is applied once.
- **Chaos testing:** thousands of randomized, seeded, reproducible scenarios (crashes, restarts,
  partitions, message loss, delay, reordering) with automatic checks after every run: Raft's safety
  properties plus a linearizability check of the client history.
- **The website** (six sections, see below), hosted free on GitHub Pages.
- **Poyraz understands how the system works.** They must be able to explain, in an interview and
  without notes, how the cluster behaves and why: elections, replication, the commit rule, what
  happens in each failure, persistence, client retries, linearizability, and the main design
  trade-offs. Understanding the Go code line by line is *not* the goal (see "How we work").

## Non-goals

- Production use. This is a learning and portfolio system.
- Byzantine (malicious) faults, authentication, security hardening.
- Sharding / multi-Raft, transactions across keys.
- Getting users, marketing, app stores. The project must stand on its own when a reviewer opens it.

## Anti-goals (would make it a failure even if it "works")

- **Anything whose value depends on getting users or players.** Poyraz learned from an earlier
  Roblox game that they don't want to deal with publishing and finding users.
- Visual-generation AI or creator-economy themes (Poyraz's day jobs; they want distance from them).
- **A system Poyraz can't explain.** If an interviewer asks "what happens when the leader dies
  mid-write?" and Poyraz can't answer, the project has hurt, not helped. *(Claude's call, from
  Poyraz's goal of learning the field.)*
- **Fake numbers or fake stories on the site.** Every number shown (scenario counts, violations,
  throughput, latency) is produced by a real run of the code and loaded from generated data. The
  "bugs the tests caught" stories are real bugs, recorded in `docs/bugs.md` as they happen.
  *(Claude's call.)*
- **A Raft animation that isn't Raft.** The browser must run the real implementation, not a
  separate JavaScript re-enactment. *(Claude's call.)*
- **Ongoing hosting cost or ops burden.** Static hosting only; no backend to keep alive.

## Constraints

- Static hosting: GitHub Pages. Planned URL: `https://pbariskan.github.io/Raft/` (served from this
  repo's Pages; the portfolio repo has no `Raft/` folder, so the path is free). Pages paths are
  case-sensitive.
- The repository is public. No secrets, ever.
- Time: no fixed deadline; Poyraz expects to put in more than 10 hours a week. So the roadmap keeps
  "solid engine first, then the site". Poyraz is also a student with exams, so work comes in
  bursts: every milestone ends in a working, tested, committed state and can be paused there.
- Code, comments, docs and site text are in English (international reviewers).

## Prior art (and how this differs)

- **raft.github.io** (RaftScope) and **The Secret Lives of Data** (thesecretlivesofdata.com/raft)
  are well-known Raft visualizations. Both are JavaScript simulations of the idea.
  This project differs: the browser runs the *actual* implementation that also runs over a real
  network, and it ships with a chaos test suite and a linearizability checker.
- **MIT 6.5840 (formerly 6.824) Lab 3/4** has thousands of students implement Raft + a KV store.
  So Raft alone is common; what makes this stand out is the visual playground, the guided
  scenarios, the chaos testing results, and a clear write-up.
- **etcd/raft** (`go.etcd.io/raft/v3`) is the reference for the architecture below. Read it for
  ideas; do not import it or copy it. The point is that Poyraz's code implements Raft.

Reading list for Poyraz: the extended Raft paper (Ongaro & Ousterhout, "In Search of an
Understandable Consensus Algorithm"), Jon Gjengset's "Students' Guide to Raft", the MIT 6.5840
Raft lab handout, and the Porcupine README (linearizability checking).

## Architecture

```
raft/       Pure, deterministic Raft state machine. No goroutines, no clock, no I/O, no global
            randomness. Driven from outside: Tick(), Step(message), Propose(command).
            Produces a Ready batch: messages to send, entries/state to persist, entries committed.
sim/        Deterministic simulator: virtual clock, simulated network (latency, loss, reordering,
            partitions), crash/restart with a simulated disk that can lose unsynced writes.
            One seed → one exact, replayable run. Used by tests, chaos runs and the browser.
kv/         Key-value state machine + client sessions (dedupe by client id + sequence number).
storage/    Real-mode persistence: write-ahead log + hard state (term, vote) with fsync.
transport/  Real-mode network between processes.
cmd/        raftd (one node), a launcher for a local 3/5-node cluster, bench, chaos runner.
wasm/       GOOS=js GOARCH=wasm entry point exposing the simulator to JavaScript.
web/        The static website.
docs/       This file, learning notes, bugs log, mockups.
```

The key design decision: **the Raft core is a deterministic state machine that never touches the
outside world.** That single choice is what lets the same code run over a real network, inside a
reproducible simulator, and in the browser, and what makes chaos-test failures replayable from a
seed.

Safety invariants checked continuously in simulation (from the paper): Election Safety (at most
one leader per term), Log Matching, Leader Completeness, State Machine Safety. Client histories are
checked for linearizability with Porcupine (`github.com/anishathalye/porcupine`).

## The website

Agreed structure (mockup: `docs/mockups/site-sections.html`; Poyraz liked the structure, the
visual design in the mockup is rough and should be improved):

1. **Hero.** Headline ("Five servers. One truth." is a draft), one sentence on what it is, a calm
   live cluster in the background, buttons: "Break it yourself" and GitHub.
2. **Playground (the heart of the site).** Five servers running the real Raft code. Visitors can
   kill/revive servers, partition the network, set message loss and latency, slow down, pause and
   step. A client box sends `PUT`/`GET`. Each server's log is shown (committed vs not), plus an
   event feed ("S3 became candidate", "S1 won 4/5 votes", "PUT x 5 committed").
3. **Guided scenarios.** Short step-by-step stories that teach Raft to someone who has never heard
   of it, each played on the playground with one sentence per step: leader crashes; network
   splits in two; old leader comes back; messages get lost.
4. **Chaos testing: "I tried to break it."** Real numbers from the chaos runner (scenarios run,
   violations found) and short stories of real bugs the tests caught, each linking to its fix.
   Plus a clearly labelled **"classic mistakes" demo** *(Claude's call)*: well-known Raft errors
   switched back on deliberately (e.g. committing an old-term entry just because a majority has
   it, answering a vote before persisting it, resetting the election timer on the wrong message),
   each shown being caught by the checkers. This keeps the section honest and full even if the
   real bug list stays short.
5. **How it's built.** Layer diagram; emphasis that the same code runs on a real network and in
   the browser.
6. **Benchmarks and write-up.** Real-mode throughput and latency for 3 vs 5 nodes, a
   "What I learned building Raft" article, GitHub link.

The site must be usable on a phone (reviewers often open links on mobile), and the playground must
make sense to someone who has never heard of Raft.

## How we work (learning-first vibe coding)

Claude writes the code. Poyraz said explicitly: **don't explain the code; teach how the system
works.** So explanations are about behaviour, never line-by-line Go: what each server does, which
messages fly, what happens under each failure, why the rule is the way it is. No Go primer.

For every milestone:

1. Before building, explain that milestone's idea in plain words, in Turkish in chat: the problem
   it solves, how Raft handles it, and one concrete failure story (step by step, which server
   does what). Short; a diagram when it helps.
2. Build it test-first. Keep the code readable over clever.
3. After it works, write `docs/learning/NN-<topic>.md` (English) about the system, not the code:
   the concept, how the cluster behaves, the failure cases it handles, the design trade-offs, the
   bug(s) hit along the way, and 3–5 interview-style questions with answers. These notes become
   the raw material for the final write-up.
4. Ask Poyraz 2–3 of those questions before moving on; if an answer is shaky, explain again
   *(Claude's call; drop it if Poyraz finds it annoying)*. Once the playground exists, use it to
   show the behaviour instead of describing it.

Every real bug caught by any test, from M1 on, goes into `docs/bugs.md` (symptom, how to replay
it: seed or test name, root cause, fix commit). Site section 4 is built from this file.

## Success looks like

- A reviewer opens the link, understands what it is within 5 seconds, and has broken the cluster
  within 30 seconds.
- The chaos suite runs in CI on a fixed seed set with zero violations, and a long run (e.g.
  thousands of seeds) has been done for the numbers on the site.
- Poyraz can explain election, replication, the commit rule, persistence, client dedupe and
  linearizability without notes, and walk through any failure scenario on the playground.
- The project is linked from the portfolio as a sixth project card.

## Left to the builder

Transport protocol, the frontend stack for `web/`, how the WASM bridge streams events to the UI,
exact chaos parameters, benchmark methodology details, visual design of the site, CI setup.
Pick sensible, boring, well-known options and write the choice down in the relevant learning note
or commit.

## Roadmap

Each milestone ends green (tests pass) and committed.

- [x] **M0 — Setup.** Go module, folder layout, `go test ./...` in GitHub Actions, `.gitignore`,
      empty `docs/bugs.md` and `docs/learning/`.
- [x] **M1 — Simulator + leader election.** Deterministic sim (clock, network, seeded RNG);
      election with randomized timeouts and terms; tests: one leader emerges, re-election after
      leader crash, no two leaders in a term. Learning note 01.
- [ ] **M2 — Log replication + commit rule.** AppendEntries, consistency check, conflict
      resolution, commit only entries from the current term by majority. Learning note 02.
- [ ] **M3 — Persistence + crash recovery.** Persist term/vote/log before replying; simulated disk
      that drops unsynced writes on crash; restart recovery. Learning note 03.
- [ ] **M4 — Key-value store + client sessions.** PUT/GET/DELETE through the log, dedupe, client
      retries and leader redirect. Learning note 04.
- [ ] **M5 — Chaos runner + linearizability.** Randomized fault schedules over many seeds,
      invariant checks every step, Porcupine check per run, failing seed printed for replay; fixed
      seed set in CI; "classic mistakes" switches in the core, each caught by the checkers.
      Learning note 05.
- [ ] **M6 — Real mode.** `storage/` with fsync, `transport/`, `raftd`, local cluster launcher,
      benchmarks producing JSON for the site. Learning note 06.
- [ ] **M7 — WASM bridge + playground.** Simulator in the browser, playground UI with all
      controls, works on phone width.
- [ ] **M8 — Guided scenarios.** The four scenarios, scripted on the simulator.
- [ ] **M9 — Rest of the site + launch.** Hero, chaos results (from real data), architecture,
      benchmarks, write-up; deploy to GitHub Pages; public README for reviewers; add the project
      card to the portfolio (separate repo `pBariskan/pBariskan.github.io`).

Stretch, only after M9: snapshots / log compaction (InstallSnapshot), ReadIndex reads, pre-vote,
membership changes.
