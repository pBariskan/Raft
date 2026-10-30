# Site theme: a raft of logs

Poyraz, 2026-10-30, after the site went live: the first design (cream paper, serif headlines, a
terracotta accent) read as the generic "AI assistant" look. A first round of three reskins (signal
box, station timetable, man page) was rejected as too safe. A second round sketched concepts built
from Raft itself; Poyraz chose **the raft of logs**.

The idea comes from the name. John Ousterhout explains it in his talk on the algorithm: a raft is
something you build out of a collection of logs, and something you can use to get away from the
island of Paxos. On the site each server is a wooden log; its entries are segments along the log; the
servers that can reach each other are lashed together with rope; the leader flies the flag with its
term; a crashed server's log sinks; a network split cuts the raft into pieces with open water between.

Finish line: every page (Landing, Workplace, Course, Under the hood) uses the theme, in light and
dark, on desktop and phone; the live cluster drawing is the raft everywhere; the old look is gone;
`docs/design.md` describes the new system; the site is redeployed.

## Steps

- [x] Cluster drawing (`cluster-view.js`) rewritten as the raft: logs stacked and aligned by log
      index, messages as arcs between logs, clients as boats, partitions as separate rafts. Same API
      (`update`, `frame`, `setSelected`, `setMarked`, `destroy`).
- [x] Tokens and base styles: sand, sea, wood, rope, flag; Zilla Slab, Karla, Spline Sans Mono.
- [x] Landing: sky and sea scene with the island of Paxos, live raft, the name's origin.
- [x] Workplace, Course (figures and static diagrams), Under the hood restyled; charts re-validated.
- [x] `docs/design.md` rewritten.
- [x] Checked in a browser at 1280 px and 390 px, light and dark; tests and vet pass.
- [ ] Pushed; Pages workflow re-run after Poyraz's go-ahead.
