# Site design

Poyraz's one hard requirement: it must not look AI-generated. The first design (2026-10-30, a
"field manual": cream paper, Source Serif, IBM Plex, a terracotta accent) went live and read as
exactly that generic look, so it was replaced the same day. The history of that decision is in
`docs/plans/site-theme.md`.

## Idea: a raft of logs

The name is the design. John Ousterhout explains it in his talk on the algorithm: a raft is
something you build out of a collection of logs, and something you can use to get away from the
island of Paxos. So the site is a shore and a sea:

- The page itself is the shore: sand-coloured paper, dark ink.
- Every live figure is the sea, a teal band with a few drawn waves. The cluster floats in it.
- The landing page puts the island of Paxos on the horizon, behind the raft.
- The footer is a strip of sea with a wavy edge; section headings get a wavy underline.

The live cluster is the picture. There are no stock illustrations; the only drawn decoration is
the island, the waves and the logo (three logs and a rope).

## How the cluster is drawn

- **Server = a log.** A wooden log seen from the side, all of equal length, with the cut end
  (`S1`, growth rings) on the left. Logs are stacked top to bottom.
- **Entries = painted segments** along the log, aligned by log index across all logs so a column
  is one index. Each is tinted by its term (eight tints, cycling) and labelled with the term; a
  small ring marks a leader's no-op. Uncommitted entries are hatched and faded.
- **Rope = connectivity.** Two rope lashings join neighbouring logs that can reach each other. A
  rope to a crashed server goes slack (dashed).
- **Leader flies the flag** at the end of its log, with its term (`T3`). A candidate shows an
  empty dashed pennant with `?`.
- **Crashed server sinks:** the log turns grey, drops a few pixels and leaves a ripple.
- **Partition = the raft breaks apart.** Each side is its own raft, with open water and a wavy
  "cut off" line between them.
- **Election timer:** a small bar above each follower's log, filling up.
- **Messages** travel on arcs in the water to the left of the logs. Votes: flag-red circles
  (hollow when refused). AppendEntries with entries: pale squares labelled `+n` (label hidden on
  phones); heartbeats: small hollow squares. Lost or dropped messages burst into a small ×.
- **Clients are boats** on the right (below the raft on phones), with the request they are
  waiting on. Client messages are rope-coloured diamonds on straight lines to the log ends.

The drawing is `web/js/cluster-view.js`. Its API (`update`, `frame`, `setSelected`, `setMarked`,
`destroy`) is shared by the landing hero, the Workplace and every Course scene.

## Tokens

Defined on `:root` in `web/css/site.css`; dark mode is chosen, not inverted: the shore becomes
night water and the sea gets deeper.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--paper` | `#efe5cf` | `#0f1c1e` | page (sand / night) |
| `--paper-2` | `#e6d9bc` | `#152729` | panels, notes |
| `--ink` | `#1f2624` | `#ece4d2` | text, rules, primary button |
| `--ink-2` | `#51574e` | `#a9b3aa` | secondary text |
| `--rule` | `#cbbb98` | `#2a3e40` | thin lines |
| `--sea`, `--sea-2` | `#1f5b60`, `#194b4f` | `#123c40`, `#0c2d30` | figures, legends, footer |
| `--foam` | `#e8efe6` | same | text and marks on the sea |
| `--wood`, `--wood-3` | `#b57d4b`, `#5f3a18` | same | logs, their outline |
| `--rope` | `#ead39b` | same | lashings, client messages |
| `--flag` | `#d4452b` | same | leader flag, votes, current step |
| `--signal` | `#b8381e` | `#f0704f` | alerts on the page |
| `--ok` | `#2c6a3a` | `#77c98d` | safety checks holding |
| `--t1`…`--t8` | pale tints | same | entry colour by term |
| `--c1`, `--c2` | `#1f6aa5`, `#c8452a` | `#4a9ad8`, `#e8673f` | chart series (3 / 5 servers) |

The chart pair was checked with the dataviz palette validator against both page colours.

**Type:** Zilla Slab (headings, buttons: a slab serif with a carved, wooden feel), Karla (text),
Spline Sans Mono (labels, numbers, server names, commands). Body 17px.

**Shape:** buttons are slab-type rectangles with a 2px ink border and 4px corners; the primary one
is ink-filled and turns flag-red on hover. Figures have 8px corners. No shadows except the log's
own, no gradients, no blur.

## The "AI look" checklist (none of these)

- Purple/blue gradients, gradient text, glows, glassmorphism, blur.
- Centered hero with a giant headline over a vague abstract background.
- Rows of three identical feature cards with icons; emoji as icons; sparkles.
- Rounded-everything with soft drop shadows; pill buttons everywhere.
- A tasteful neutral palette with one accent and a serif headline: the look that got the first
  design rejected. A new visual idea must come from the subject, not from a palette.
- Marketing words: seamless, effortless, unlock, supercharge, revolutionize, cutting-edge.
- Invented numbers or testimonials. Every number comes from `web/data/*.json`, produced by a run.
- Inter as the only typeface; generic dashboard grey.
