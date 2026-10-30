# Site design

Decided 2026-10-30, before building any page. Poyraz's one hard requirement: it must not look
AI-generated.

## Idea: a field manual for a machine

The site reads like a well-made technical manual or lab notebook: warm paper, black ink, one
signal colour, numbered sections, figures with captions, data set in a monospaced face. It is
about a mechanism, so the mechanism (the live cluster) is the picture; there are no stock
illustrations or decorative graphics.

## Tokens

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--paper` | `#f3efe6` | `#15140f` | background |
| `--paper-2` | `#ebe5d8` | `#1d1c16` | panels, figure ground |
| `--ink` | `#1d1b17` | `#ece6d8` | text, leader fill, rules |
| `--ink-2` | `#5f5a50` | `#a59f90` | secondary text |
| `--rule` | `#cfc7b6` | `#38352b` | 1px lines |
| `--signal` | `#c4441c` | `#ec6a3f` | elections (votes, candidates), alerts, primary links |
| `--wire` | `#1f5f74` | `#5fb0c9` | log replication (AppendEntries) |
| `--client` | `#a87400` | `#e0a530` | client traffic |
| `--ok` | `#2f6b3a` | `#78bf84` | checks passing |

Terms in logs get a muted hue each, cycling through eight.

Type: **Source Serif 4** for headings, **IBM Plex Sans** for text, **IBM Plex Mono** for labels,
numbers, server names, commands. Sizes on a 1.25 scale; body 17px; line length ≤ 70ch.

Shape: square corners (2px at most), 1px rules, no shadows, no gradients, no blur. Buttons are
rectangles with a mono label; the primary one is filled with ink.

Layout: left-aligned, a 12-column grid with a wide figure column and a narrow margin column for
notes and controls. Sections are numbered (`§ 03`). Figures are captioned (`Fig. 2 — …`).

## How the cluster is drawn

- Server: a rectangle with its name (S1…), role, term, and an election-timer bar. Follower =
  outlined; candidate = signal colour; leader = solid ink; down = dashed and hatched.
- Messages travel in straight lines. Votes: signal circles (hollow when refused). AppendEntries:
  wire-coloured squares (small and hollow for empty heartbeats, filled with an entry count when
  they carry entries). Client requests and replies: client-coloured diamonds.
- A partition re-arranges the servers into islands side by side, with a dashed fault line
  between them.
- Logs: one row per server, columns aligned by index, each entry tinted by term; committed
  entries solid, uncommitted hatched.

## The "AI look" checklist (none of these)

- Purple/blue gradients, gradient text, glows, glassmorphism, blur.
- Centered hero with a giant headline over a vague abstract background.
- Rows of three identical feature cards with icons; emoji as icons; sparkles.
- Rounded-everything with soft drop shadows; pill buttons everywhere.
- Marketing words: seamless, effortless, unlock, supercharge, revolutionize, cutting-edge.
- Invented numbers or testimonials. Every number comes from `web/data/*.json`, produced by a run.
- Inter as the only typeface; generic dashboard grey.
