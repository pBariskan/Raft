# Site build (Workplace, Course, Landing, Under the hood)

Plain static files in `web/`, no build step besides the engine (`scripts/build-web.sh`). Design
decisions: `docs/design.md`.

- [x] Visual identity first (`docs/design.md`): paper/ink/one signal colour, serif headings, mono
      data, square corners, no gradients or shadows; "AI look" checklist.
- [x] Shared pieces: `engine.js` (loads the WASM, several sessions), `cluster-view.js` (servers,
      messages interpolated between ticks, partitions as islands, clients, lost/dropped puffs;
      portrait layout on phones), `runner.js` (real-time playback), `script.js` (timed scenes),
      `panels.js` (logs, stores, event feed, theme), `data.js` (generated numbers only).
- [x] Workplace: crash/restart/isolate by clicking, partition builder, loss and delay, client
      form, background traffic, safety checks + linearizability button, setup (3/5/7 servers,
      seed, classic mistake), chaos-seed replay with "finish", shareable link, six scenarios.
- [x] Course: 17 modules, 51 steps, 34 questions; engine-driven scenes (including replays of the
      chaos runner's failing seeds for three mistakes) and static diagrams; questions gate the
      next module (shuffled options, explanations); progress in localStorage, "open all" switch.
- [x] Landing: live cluster (click to crash), facts from data, contents, what it is.
- [x] Under the hood: chaos totals, mistakes table with "watch" links, what testing found,
      benchmark charts (validated palette, hover tooltips, table view), architecture, commands.
- [x] Phone width (390 px) and dark mode checked with screenshots; keyboard: servers are
      focusable buttons, charts' points focusable.
- [x] CI builds the engine; `pages.yml` deploys `web/` but is manual (workflow_dispatch) until
      Poyraz enables Pages.
- [ ] Deploy: Poyraz enables Pages (Settings → Pages → Source: GitHub Actions) and runs the
      `pages` workflow, after merging to main.
