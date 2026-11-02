// The Course: 17 modules that build on each other. Each step has a short text and usually a
// figure run by the real engine (a scripted scene) or a small static diagram. Each module ends
// with questions that must be answered correctly to open the next one.
//
// {{name}} in a text is filled in from the generated data (web/data), so numbers are real.

import { leaderOf, followersOf } from "./script.js";

// ----- scene helpers -----

const client = (sim, v) => (v.c ||= sim.do({ op: "client" }).client);
const send = (kv, key, value = "") => (sim, v) => sim.do({ op: "submit", client: client(sim, v), kv, key, value });
const crashLeader = (sim, v, s) => { v.old = leaderOf(s); sim.do({ op: "crash", node: v.old }); };
const L = s => "S" + leaderOf(s);
const quorum = n => Math.floor(n / 2) + 1;

// Static diagrams are small inline SVGs drawn in the site's own vocabulary.
const box = (x, y, label, cls = "") =>
  `<g class="node ${cls}"><rect class="box" x="${x}" y="${y}" width="64" height="34"/><text class="name" x="${x + 8}" y="${y + 22}">${label}</text></g>`;

const fig = {
  order: `<svg viewBox="0 0 560 190" class="cluster static" role="img" aria-label="Two copies receive the same two writes in different orders and end up different">
    ${box(20, 20, "C1")}${box(20, 130, "C2")}
    ${box(380, 20, "copy A")}${box(380, 130, "copy B")}
    <line x1="90" y1="37" x2="374" y2="37" class="arrow"/><text x="200" y="30" class="lbl">x=1 (fast)</text>
    <line x1="90" y1="37" x2="374" y2="140" class="arrow"/><text transform="translate(160 62) rotate(19.9)" y="-6" text-anchor="middle" class="lbl">x=1 (slow)</text>
    <line x1="90" y1="147" x2="374" y2="147" class="arrow"/><text x="200" y="165" class="lbl">x=2 (fast)</text>
    <line x1="90" y1="147" x2="374" y2="44" class="arrow"/><text transform="translate(160 122) rotate(-19.9)" y="-6" text-anchor="middle" class="lbl">x=2 (slow)</text>
    <text x="452" y="42" class="res">ends with x=2</text><text x="452" y="152" class="res">ends with x=1</text>
  </svg>`,
  overlap: `<svg viewBox="0 0 560 170" class="cluster static" role="img" aria-label="Two majorities of five servers always share at least one server">
    <rect x="12" y="34" width="290" height="70" class="maj a"/><rect x="230" y="66" width="318" height="70" class="maj b"/>
    ${box(30, 52, "S1")}${box(122, 52, "S2")}${box(236, 70, "S3", "leader")}${box(358, 84, "S4")}${box(460, 84, "S5")}
    <text x="16" y="26" class="lbl">a majority: S1 S2 S3</text><text x="548" y="158" class="lbl" text-anchor="end">another majority: S3 S4 S5</text>
  </svg>`,
  terms: `<svg viewBox="0 0 560 120" class="cluster static" role="img" aria-label="Terms on a timeline: each starts with an election; term 3 has no leader">
    ${[["1", "S2 leads", 0, 137], ["2", "S5 leads", 137, 137], ["3", "split vote", 274, 137], ["4", "S1 leads", 411, 137]].map(([t, w, x, wd]) => `
      <rect x="${10 + x}" y="34" width="${wd - 4}" height="44" class="${w === "split vote" ? "term-split" : "term-box"}"/>
      <rect x="${10 + x}" y="34" width="14" height="44" class="election"/>
      <text x="${32 + x}" y="52" class="lbl in${w === "split vote" ? " sea" : ""}">term ${t}</text><text x="${32 + x}" y="68" class="lbl in${w === "split vote" ? " sea" : ""}">${w}</text>`).join("")}
    <text x="10" y="102" class="lbl muted">red band = election</text><text x="550" y="100" class="lbl muted" text-anchor="end">time →</text>
  </svg>`,
  matching: `<div class="mini-logs">
      <div><b>leader</b>${[1, 1, 1, 2, 3, 3].map((t, i) => `<span class="cell term-${t}">${t}</span>`).join("")}<span class="cell term-3 new">3</span></div>
      <div><b>follower</b>${[1, 1, 1, 2, 3, 3].map(t => `<span class="cell term-${t}">${t}</span>`).join("")}<span class="cell empty"></span></div>
      <div class="idx"><b></b>${[1, 2, 3, 4, 5, 6, 7].map(i => `<span>${i}</span>`).join("")}</div>
      <p class="caption">AppendEntries: “entry 7 (term 3), placed after entry 6 which has term 3”. The follower has entry 6 with term 3, so it accepts.</p>
    </div>`,
  repair: `<div class="mini-logs">
      <div><b>leader, term 5</b>${[1, 1, 2, 4, 4, 5].map(t => `<span class="cell term-${t}">${t}</span>`).join("")}</div>
      <div><b>follower</b>${[1, 1, 2, 3, 3].map(t => `<span class="cell term-${t}">${t}</span>`).join("")}<span class="cell empty"></span></div>
      <div class="idx"><b></b>${[1, 2, 3, 4, 5, 6].map(i => `<span>${i}</span>`).join("")}</div>
      <p class="caption">The follower's entries 4–5 (term 3) came from a leader that never committed them. The check fails at 6, then 5, then 4; it passes at 3. The follower drops 4–5 and takes the leader's 4–6.</p>
    </div>`,
  uptodate: `<table class="data compact"><thead><tr><th>candidate's last entry</th><th>voter's last entry</th><th>vote?</th></tr></thead><tbody>
      <tr><td>term 4, index 7</td><td>term 3, index 12</td><td>yes: higher last term wins</td></tr>
      <tr><td>term 3, index 9</td><td>term 3, index 12</td><td>no: same term, voter's log is longer</td></tr>
      <tr><td>term 3, index 12</td><td>term 3, index 12</td><td>yes: at least as up to date</td></tr>
      <tr><td>term 2, index 20</td><td>term 3, index 5</td><td>no: lower last term, length doesn't matter</td></tr></tbody></table>`,
  fig8: `<div class="mini-logs">
      <div><b>S1</b><span class="cell term-1">1</span><span class="cell term-2">2</span><span class="cell term-4 unc">4</span></div>
      <div><b>S2</b><span class="cell term-1">1</span><span class="cell term-2">2</span><span class="cell empty"></span></div>
      <div><b>S3</b><span class="cell term-1">1</span><span class="cell term-2">2</span><span class="cell empty"></span></div>
      <div><b>S4</b><span class="cell term-1">1</span><span class="cell empty"></span><span class="cell empty"></span></div>
      <div><b>S5</b><span class="cell term-1">1</span><span class="cell term-3">3</span><span class="cell empty"></span></div>
      <div class="idx"><b></b><span>1</span><span>2</span><span>3</span></div>
      <p class="caption">S1 leads term 4 and has pushed the term-2 entry at index 2 to S2 and S3: a majority. If it called that committed and then crashed, S5 (last term 3, higher than 2) could win votes from S2, S3, S4 and overwrite index 2 everywhere. A “committed” entry would vanish.</p>
    </div>`,
  table: `<table class="data compact"><thead><tr><th>servers</th><th>majority</th><th>can fail</th></tr></thead><tbody>
      <tr><td>1</td><td>1</td><td>0</td></tr><tr><td>3</td><td>2</td><td>1</td></tr><tr><td>4</td><td>3</td><td>1</td></tr>
      <tr><td>5</td><td>3</td><td>2</td></tr><tr><td>7</td><td>4</td><td>3</td></tr></tbody></table>`,
  layers: `<div class="layers">
      <div class="layer core"><b>raft/</b> the algorithm: roles, terms, votes, log, commit rule. No clock, no network, no disk. It is told “a tick passed” or “this message arrived” and answers with what to save, send and apply.</div>
      <div class="layer-row">
        <div class="layer"><b>sim/</b> virtual time, a network that delays, loses and splits, crashes. Used by the tests, the chaos runner, and this page.</div>
        <div class="layer"><b>server/</b> real time (10 ms ticks), files with fsync, HTTP between processes, a client API.</div>
      </div>
      <div class="layer"><b>kv/</b> the key-value state machine with client sessions, on top of either.</div>
    </div>`,
};

// ----- the modules -----

export const MODULES = [
  {
    id: "why",
    title: "Why one machine isn't enough",
    steps: [
      {
        title: "One server is one point of failure",
        text: `<p>Picture a key-value store: a server that remembers values under names, like <code>x = 5</code>. Clients send <code>PUT x 5</code> to store and <code>GET x</code> to read.</p>
          <p>On one machine this is easy, and fragile. If that machine crashes, nobody can read or write until it is back. If its disk dies, the data is gone.</p>
          <p>The obvious fix is to keep copies on several machines, so the service survives when some of them fail.</p>`,
      },
      {
        title: "Copies disagree",
        text: `<p>Copies bring a new problem. Two clients write at nearly the same time: one sends <code>x=1</code>, the other <code>x=2</code>, each to every copy. Messages take different amounts of time, so the copies can receive the writes in different orders.</p>
          <p>Each copy applies what it gets in the order it arrives. Copy A ends with <code>x=2</code>, copy B with <code>x=1</code>. Both are “correct” by their own view, and the system as a whole now gives different answers depending on whom you ask.</p>`,
        html: fig.order,
        caption: "Same writes, different arrival order, different results.",
      },
      {
        title: "What Raft does",
        text: `<p>The fix is to agree on <b>one order</b>. If every copy applies the same commands in the same order, and applying a command is deterministic, every copy ends in the same state. The ordered list of commands is called the <b>log</b>.</p>
          <p><b>Raft</b> is a <em>consensus algorithm</em>: a set of rules that lets a group of servers agree on one log even when some of them crash and the network delays or loses messages. It does not defend against servers that lie; it assumes servers are honest but can fail.</p>
          <p>Below are five servers running the real implementation. Three clients are sending requests. Watch the rows of the logs: they fill up identically.</p>`,
        scene: { nodes: 5, seed: 11, logs: true, script: () => [
          { wait: "leader", do: sim => sim.do({ op: "traffic", on: true }), note: "Clients send requests; every server's log grows the same way." },
        ] },
      },
    ],
    questions: [
      { q: "Two copies of a store receive x=1 and x=2 in different orders and apply them as they arrive. What happens?",
        options: ["The later timestamp wins on both", "They end with different values for x", "Both end with x=2"],
        answer: 1, why: "Each copy applies writes in arrival order, so the last write differs between them. Nothing reconciles them unless they agree on an order." },
      { q: "What do Raft servers agree on?",
        options: ["The order of commands in a shared log", "Which server has the fastest network", "The current time"],
        answer: 0, why: "Raft's job is agreement on a log. The same commands in the same order give the same state everywhere." },
    ],
  },
  {
    id: "majority",
    title: "Majorities",
    steps: [
      {
        title: "Decide with a majority",
        text: `<p>Waiting for <em>every</em> server before doing anything would make one crash stop the whole system. Raft waits for a <b>majority</b> instead: more than half the servers.</p>
          <p>The key fact: <b>any two majorities share at least one server.</b> With five servers, a majority is three; two groups of three out of five must overlap. That shared server is how knowledge passes from one decision to the next: whatever a majority accepted, some member of any later majority has seen it.</p>`,
        html: fig.overlap,
        caption: "S3 belongs to both majorities. Every pair of majorities has such a server.",
      },
      {
        title: "How many failures a cluster survives",
        text: `<p>With N servers a majority is ⌊N/2⌋ + 1, so the cluster keeps working while a majority is up. Three servers survive one failure; five survive two; seven survive three.</p>
          <p>Even numbers buy nothing: four servers need three for a majority, so they survive one failure, the same as three servers, with one more machine that can fail. That is why clusters have 3, 5 or 7 servers.</p>`,
        html: fig.table,
      },
      {
        title: "See it",
        text: `<p>Here two servers crash: three of five remain, a majority, and requests keep completing. Then a third crashes. Now nothing can commit: clients wait. Nobody gets a wrong answer; the system just stops until a majority is back.</p>`,
        scene: { nodes: 5, seed: 21, logs: true, script: () => [
          { wait: "leader", do: sim => sim.do({ op: "traffic", on: true }), note: "Clients are sending requests." },
          { wait: 30, do: (sim, v, s) => { v.down = followersOf(s).slice(0, 2); v.down.forEach(id => sim.do({ op: "crash", node: id })); },
            note: v => `${v.down.map(x => "S" + x).join(" and ")} crash. Three of five are up: still a majority, still working.` },
          { wait: 70, do: (sim, v, s) => { v.last = followersOf(s)[0]; sim.do({ op: "crash", node: v.last }); },
            note: v => `S${v.last} crashes too. Two of five: no majority. New entries stay hatched (not committed) and clients wait.` },
          { wait: 90, do: (sim, v) => sim.do({ op: "restart", node: v.last }),
            note: v => `S${v.last} comes back. A majority again: the waiting requests complete.` },
        ] },
      },
    ],
    questions: [
      { q: "A cluster has 5 servers. How many can crash while it still accepts writes?",
        options: ["1", "2", "3"], answer: 1,
        why: "A majority of 5 is 3. With 2 down, 3 remain, which is still a majority." },
      { q: "Why is a 4-server cluster no better than 3 servers at surviving crashes?",
        options: ["Its majority is 3, so it survives only 1 failure", "Raft needs an odd number to start", "Votes always tie with 4"],
        answer: 0, why: "Four servers need three for a majority, so they tolerate one failure, exactly like three servers." },
    ],
  },
  {
    id: "roles",
    title: "Roles and terms",
    steps: [
      {
        title: "Three roles",
        text: `<p>Each server is in one of three roles at any moment:</p>
          <ul><li><b>Follower</b>: passive. It answers the leader and candidates, nothing else.</li>
          <li><b>Candidate</b>: trying to become leader by collecting votes.</li>
          <li><b>Leader</b>: handles every client request and tells the followers what to add to their logs.</li></ul>
          <p>Having one leader keeps things simple: the leader alone decides the order of the log, and everyone else copies it. In the drawing, the leader is the log flying the red flag with its term; the little hollow squares it keeps sending are heartbeats.</p>`,
        scene: { nodes: 5, seed: 31, script: () => [
          { wait: 0, note: "Everyone starts as a follower in term 0." },
          { wait: "leader", note: (v, s) => `${L(s)} became leader. The hollow squares it sends are heartbeats: “I'm still here.”` },
        ] },
      },
      {
        title: "Terms",
        text: `<p>Raft divides time into numbered <b>terms</b>. Each term starts with an election. If a candidate wins, it leads for the rest of that term. If the vote is split, the term ends with no leader and a new term (with a new election) starts.</p>
          <p><b>At most one leader per term.</b> Every message carries its sender's term, and two rules follow:</p>
          <ul><li>See a <b>higher</b> term: adopt it, and become a follower.</li>
          <li>Receive a message with a <b>lower</b> term: reject it; it comes from the past.</li></ul>`,
        html: fig.terms,
      },
      {
        title: "Terms expose stale leaders",
        text: `<p>Terms work like a clock that everyone agrees on. A leader that was cut off doesn't know it was replaced; the moment it talks to anyone from a newer term, it learns, and steps down.</p>
          <p>Here the leader is isolated, the others elect a new leader in a higher term, and then the network heals.</p>`,
        scene: { nodes: 5, seed: 32, script: () => [
          { wait: "leader", note: "Waiting for a leader…" },
          { wait: 15, do: (sim, v, s) => { v.old = leaderOf(s); sim.do({ op: "partition", groups: [v.old] }); },
            note: v => `S${v.old} is cut off. It still calls itself leader: nothing has told it otherwise.` },
          { wait: (s, v) => s.leader && s.leader !== v.old, note: (v, s) => `The others elect S${s.leader} in term ${s.nodes.find(n => n.id === s.leader).term}.` },
          { wait: 30, do: sim => sim.do({ op: "heal" }), note: v => `Healed. S${v.old} receives a message with a higher term and becomes a follower at once.` },
        ] },
      },
    ],
    questions: [
      { q: "A leader in term 3 receives a message carrying term 5. What does it do?",
        options: ["Ignores it: it is the leader", "Adopts term 5 and becomes a follower", "Starts an election for term 6"],
        answer: 1, why: "A higher term means the server is out of date. It adopts the term and becomes a follower." },
      { q: "Can a term end without any leader?",
        options: ["No, every term has exactly one leader", "Yes, when the votes split", "Only if every server crashes"],
        answer: 1, why: "At most one leader per term, possibly none: a split vote ends the term without a winner." },
    ],
  },
  {
    id: "elections",
    title: "Elections",
    steps: [
      {
        title: "Heartbeats and the election timer",
        text: `<p>The leader sends heartbeats regularly (in this simulation, every tick). Every follower runs an <b>election timer</b>; each message from the leader resets it. The small bar above each follower's log shows its timer filling up.</p>
          <p>If the timer runs out, the follower concludes the leader is gone and becomes a candidate. As long as heartbeats arrive, that never happens.</p>`,
        scene: { nodes: 5, seed: 41, speed: 4, script: () => [
          { wait: "leader", note: "Followers' timers keep resetting as heartbeats arrive." },
        ] },
      },
      {
        title: "Asking for votes",
        text: `<p>A follower whose timer runs out:</p>
          <ol><li>increases its term by one,</li><li>votes for itself,</li><li>sends <b>RequestVote</b> (the red circles) to everyone.</li></ol>
          <p>Each server grants <b>at most one vote per term</b>, to the first candidate that asks (there is one more condition, about logs, in module 11). A candidate that collects votes from a majority, itself included, becomes leader and immediately sends heartbeats so nobody else starts an election.</p>`,
        scene: { nodes: 5, seed: 42, speed: 4, script: () => [
          { wait: "leader", note: "Waiting for a leader…" },
          { wait: 15, do: crashLeader, note: v => `S${v.old}, the leader, crashes. Watch the timers; red circles are vote requests (filled replies: yes, hollow: no).` },
          { wait: s => s.leader !== 0, note: (v, s) => `${L(s)} collected a majority and leads term ${s.nodes.find(n => n.id === s.leader).term}.` },
        ] },
      },
      {
        title: "One vote per term, written down",
        text: `<p>Why can't two candidates win the same term? Each needs votes from a majority, any two majorities share a server, and that server votes only once per term.</p>
          <p>That argument holds only if a server really never votes twice, even across a crash. So a server writes its vote to disk <em>before</em> replying. If it forgot its vote on reboot, it could vote again in the same term, and two leaders could appear. We'll see exactly that bug in module 13.</p>`,
      },
    ],
    questions: [
      { q: "A follower's election timer runs out. What does it do?",
        options: ["Waits for the next heartbeat", "Increases its term, votes for itself, and asks the others for votes", "Asks the old leader to step down"],
        answer: 1, why: "It becomes a candidate: new term, own vote, RequestVote to everyone." },
      { q: "Why can't two candidates both win the same term?",
        options: ["Each server votes once per term, and two majorities always share a server", "The candidate with the lower ID always wins", "Candidates check with each other first"],
        answer: 0, why: "Both winners would need a majority; the shared server would have voted twice in one term, which it never does." },
    ],
  },
  {
    id: "split",
    title: "Split votes and random timeouts",
    steps: [
      {
        title: "Split votes",
        text: `<p>If two followers time out at nearly the same moment, both become candidates in the same term. Each votes for itself, and the rest split their votes. If nobody reaches a majority, the term ends without a leader. Both wait for their timers again and start a new term.</p>
          <p>With unlucky timing this could repeat forever.</p>`,
      },
      {
        title: "Randomness breaks the tie",
        text: `<p>Raft's answer is simple: each server picks its election timeout <b>at random</b> from a range (here between 10 and 19 ticks, freshly drawn every time). Usually one server times out clearly first, collects its votes and sends heartbeats before anyone else wakes up.</p>
          <p>Below, messages are slow (4–12 ticks, about as long as a timeout). Elections overlap and votes split, until random timeouts separate the candidates.</p>`,
        scene: { nodes: 5, seed: 51, speed: 8, script: () => [
          { wait: "leader", do: sim => sim.do({ op: "delay", min: 4, max: 12 }), note: "Messages now take 4–12 ticks." },
          { wait: 20, do: crashLeader, note: "The leader crashes. Several candidates may appear in the same term and split the vote." },
          { wait: s => s.leader !== 0, note: (v, s) => `${L(s)} finally wins term ${s.nodes.find(n => n.id === s.leader).term}. Count the terms it took.` },
        ] },
      },
      {
        title: "Choosing the timeout",
        text: `<p>The timeout has to be much longer than a message round trip, or elections never finish before the next one starts. It should be much shorter than the time between failures, so the cluster isn't leaderless for long.</p>
          <p>In real mode, a tick is 10 ms, so timeouts are 100–200 ms. Measured on a real 3-server cluster: when the leader was killed, writes stalled for a median of <b>{{bench.failover3}} ms</b> before the new leader answered.</p>`,
      },
    ],
    questions: [
      { q: "Why are election timeouts random?",
        options: ["So that one server usually times out first and wins before others start", "To make the system harder to attack", "To spread the CPU load"],
        answer: 0, why: "Random timeouts make simultaneous candidates rare, so most elections finish in one round." },
      { q: "What happens if the election timeout is shorter than a message round trip?",
        options: ["Elections finish faster", "Elections keep restarting before votes come back", "Nothing changes"],
        answer: 1, why: "Candidates time out again before the replies arrive, so no election can complete." },
    ],
  },
  {
    id: "partitions",
    title: "Network partitions",
    steps: [
      {
        title: "Islands",
        text: `<p>A <b>partition</b> splits the network: servers on one side can only reach servers on the same side. Nothing has crashed, yet the cluster is effectively two clusters.</p>
          <p>Only a side with a <b>majority</b> can elect a leader or commit anything. The other side can try (its servers time out and run elections), but never gets enough votes.</p>`,
      },
      {
        title: "The stale leader",
        text: `<p>The interesting case: the leader ends up on the minority side. It does not know anything is wrong. It keeps accepting requests and appending them to its log, but it can't get them onto a majority, so they never commit. Clients sending to it wait, and eventually try another server.</p>
          <p>Meanwhile the majority side elects a new leader with a higher term and carries on. For a while two servers call themselves leader, in different terms. Only one of them can commit.</p>`,
        scene: { nodes: 5, seed: 61, logs: true, script: () => [
          { wait: "leader", do: sim => sim.do({ op: "traffic", on: true }), note: "Clients are sending requests." },
          { wait: 25, do: (sim, v, s) => { v.old = leaderOf(s); v.side = [v.old, followersOf(s, v.old)[0]]; sim.do({ op: "partition", groups: v.side }); },
            note: v => `Split: ${v.side.map(x => "S" + x).join(" and ")} | the rest. S${v.old} keeps appending, but its new entries stay hatched.` },
          { wait: (s, v) => s.leader && s.leader !== v.old, note: (v, s) => `The majority elects S${s.leader}, in a higher term, and commits again.` },
          { wait: 60, do: sim => sim.do({ op: "heal" }), note: v => `Healed. S${v.old} sees the higher term and steps down; its uncommitted entries are replaced.` },
        ] },
      },
      {
        title: "Healing",
        text: `<p>When the network heals, the old leader receives a message with a higher term and steps down. Its uncommitted entries conflict with the new leader's log and are overwritten (module 10 shows how).</p>
          <p>That is safe because nothing in them was committed: no client was told those requests succeeded. Clients that were waiting retry with the new leader.</p>`,
      },
    ],
    questions: [
      { q: "A 5-server cluster splits 2 | 3, with the leader on the side of 2. A client sends it a write. What happens while the split lasts?",
        options: ["It is committed by the two servers", "It is appended but never committed; the client gets no success", "It is forwarded across the partition"],
        answer: 1, why: "Two of five is not a majority, so the entry can't be committed and the leader never reports success." },
      { q: "During the split, which side can elect a new leader?",
        options: ["Both sides", "Only the side with 3 servers", "Neither, until it heals"],
        answer: 1, why: "Winning needs votes from a majority of all 5 servers: 3. Only that side has them." },
    ],
  },
  {
    id: "log",
    title: "The log",
    steps: [
      {
        title: "Entries",
        text: `<p>The log is a list of <b>entries</b>. Each has an <b>index</b> (its position: 1, 2, 3…), a <b>term</b> (the term in which a leader received it) and a <b>command</b> such as <code>PUT x=5</code>.</p>
          <p>In the log rows below, each square is an entry, labelled and tinted by its term. The first entry of every term is the new leader's empty “no-op” (ringed); module 12 explains why it exists.</p>`,
        scene: { nodes: 3, seed: 71, logs: true, script: () => [
          { wait: "leader", do: send("PUT", "x", "5"), note: "A client sends PUT x=5." },
          { wait: "idle", do: send("APPEND", "x", "6"), note: "Then APPEND x+=6." },
          { wait: "idle", do: send("GET", "x"), note: "Then GET x: reads go through the log too." },
          { wait: "idle", note: "Three commands, three entries after the no-op, identical on every server." },
        ] },
      },
      {
        title: "The replicated state machine",
        text: `<p>Each server feeds the commands of its log, in order, into its own copy of the key-value store, its <b>state machine</b>. Applying a command is deterministic, so the same log gives the same store everywhere.</p>
          <p>Below the logs you can see each server's store. They match.</p>`,
        scene: { nodes: 3, seed: 72, logs: true, stores: true, script: () => [
          { wait: "leader", do: send("PUT", "color", "red"), note: "PUT color=red" },
          { wait: "idle", do: send("PUT", "size", "9"), note: "PUT size=9" },
          { wait: "idle", do: send("APPEND", "color", "dish"), note: "APPEND color+=dish" },
          { wait: "idle", note: "Every server applied the same three commands in the same order." },
        ] },
      },
      {
        title: "The leader decides the order",
        text: `<p>Clients send requests to the leader. The leader appends each command to the end of its own log, which fixes its position, and then copies it to the followers. Followers never invent entries; they only copy the leader's.</p>
          <p>Even a <code>GET</code> becomes a log entry here. That is slower than answering from memory, but it is the simplest way to make reads safe (module 15 shows what goes wrong otherwise).</p>`,
      },
    ],
    questions: [
      { q: "Which pair identifies one entry, the same on every server that has it?",
        options: ["Its index and its term", "Its command alone", "The time it was received"],
        answer: 0, why: "Index and term together pin down an entry (module 8 shows why that is enough)." },
      { q: "Why do all servers end up with the same key-value store?",
        options: ["They copy the leader's store every second", "They apply the same commands in the same order, deterministically", "Clients write to every server"],
        answer: 1, why: "Same log, same order, deterministic commands: same state." },
    ],
  },
  {
    id: "append",
    title: "AppendEntries and the consistency check",
    steps: [
      {
        title: "AppendEntries",
        text: `<p>The leader copies entries with an <b>AppendEntries</b> message (the pale squares; “+1” means it carries one entry). Besides the new entries it says where they go: the <b>index and term of the entry just before them</b>. It also carries the leader's commit index (module 9).</p>
          <p>A heartbeat is simply an AppendEntries with no entries (the small hollow squares).</p>`,
        scene: { nodes: 3, seed: 81, speed: 3, logs: true, script: () => [
          { wait: "leader", note: "Heartbeats only." },
          { wait: 8, do: send("PUT", "x", "1"), note: "A client sends PUT x=1. Watch the leader send “+1” to each follower, and their replies." },
          { wait: "idle", note: "Committed and answered." },
        ] },
      },
      {
        title: "The consistency check",
        text: `<p>A follower accepts AppendEntries only if its own log has an entry at that previous index <b>with that term</b>. Otherwise it rejects the message.</p>
          <p>This little check gives a strong guarantee, <b>Log Matching</b>: if two logs have an entry with the same index and term, they are identical up to that point. When the follower accepts entry 7, it has just confirmed entry 6 matches; entry 6 was accepted only after entry 5 matched; and so on back to the start.</p>`,
        html: fig.matching,
      },
      {
        title: "What a rejection means",
        text: `<p>A rejection tells the leader “my log doesn't match yours there”. That happens to servers that were down or cut off, or that hold leftovers from an old leader. The leader then works backwards until it finds where the logs agree; that is module 10.</p>`,
      },
    ],
    questions: [
      { q: "A follower receives AppendEntries saying “after entry 7 of term 3”, but its entry 7 has term 2. What does it do?",
        options: ["Accepts and overwrites entry 7", "Rejects the message", "Asks the other followers"],
        answer: 1, why: "The previous entry doesn't match, so the follower rejects; the leader will back up and retry." },
      { q: "Why does the leader keep sending heartbeats when there is nothing new?",
        options: ["To reset followers' election timers and carry the commit index", "To measure the network speed", "Followers would delete their logs otherwise"],
        answer: 0, why: "Heartbeats prove the leader is alive (no elections) and tell followers what is committed." },
    ],
  },
  {
    id: "commit",
    title: "Committing",
    steps: [
      {
        title: "When an entry is committed",
        text: `<p>An entry is <b>committed</b> once the leader knows it is stored on a majority of servers (the leader counts itself), and the entry is from the leader's current term. From then on it is permanent: every future leader will have it (module 11 proves that).</p>
          <p>Committing an entry also commits every entry before it.</p>`,
      },
      {
        title: "Applying and answering",
        text: `<p>Once committed, the leader applies the command to its state machine and only then answers the client. Followers learn the new commit index from the leader's next AppendEntries and apply too.</p>
          <p>In the logs below, watch an entry turn from hatched (stored, not committed) to solid (committed) on the leader first, then on the followers one heartbeat later. The client's answer (the diamond) leaves only after that.</p>`,
        scene: { nodes: 5, seed: 91, speed: 3, logs: true, script: () => [
          { wait: "leader", note: "Waiting for a leader…" },
          { wait: 6, do: send("PUT", "x", "7"), note: "PUT x=7 sent to the cluster." },
          { wait: "idle", note: "Answered once a majority stored it. Followers marked it committed on the next heartbeat." },
          { wait: 10, do: send("PUT", "y", "8"), note: "Once more: PUT y=8." },
        ] },
      },
      {
        title: "Commit means it survives",
        text: `<p>A committed entry sits on a majority. Any later majority (needed to elect a leader) includes at least one of those servers. That's why a committed entry survives the crash of any minority, including the leader.</p>
          <p>And it's why the leader waits before answering: telling a client “done” for an entry that isn't committed would be a promise that a crash could break.</p>`,
      },
    ],
    questions: [
      { q: "When does the leader answer the client's PUT?",
        options: ["As soon as it appends the entry to its own log", "After the entry is committed and applied", "When every follower has the entry"],
        answer: 1, why: "Only a committed entry is permanent, so only then may the client hear “done”. Waiting for every follower would let one crash block everything." },
      { q: "How do followers find out an entry is committed?",
        options: ["The client tells them", "From the commit index in the leader's next AppendEntries", "They count each other's replies"],
        answer: 1, why: "Every AppendEntries (heartbeats included) carries the leader's commit index." },
    ],
  },
  {
    id: "repair",
    title: "Repairing followers",
    steps: [
      {
        title: "Followers fall behind, or go astray",
        text: `<p>A follower that was down or cut off misses entries. A follower can also hold <em>extra</em> entries: ones an old leader appended but never committed before it lost leadership.</p>
          <p>In both cases its log differs from the leader's, and the consistency check fails.</p>`,
        html: fig.repair,
      },
      {
        title: "Backing up until the logs agree",
        text: `<p>The leader remembers, per follower, the next index to send. On a rejection it moves that index back and retries, until the check passes. (Here the follower's reply includes a hint, its log length, so the leader can skip ahead instead of stepping back one by one.)</p>
          <p>Once the check passes, the follower deletes everything after that point that conflicts and appends the leader's entries. <b>The leader never deletes or changes its own entries</b>; the followers adapt to it.</p>`,
      },
      {
        title: "See it",
        text: `<p>A follower crashes; while it is down, the others commit several new entries; then it restarts from its disk and catches up.</p>`,
        scene: { nodes: 3, seed: 101, logs: true, script: () => [
          { wait: "leader", do: (sim, v, s) => { v.f = followersOf(s)[0]; sim.do({ op: "crash", node: v.f }); sim.do({ op: "traffic", on: true }); },
            note: v => `S${v.f} crashes. The other two are a majority and keep committing.` },
          { wait: 60, do: (sim, v) => { sim.do({ op: "traffic", on: false }); sim.do({ op: "restart", node: v.f }); },
            note: v => `S${v.f} restarts with its old log. The leader's appends are rejected until it finds the matching point, then S${v.f} catches up.` },
          { wait: 40, note: "Its row now matches the others." },
        ] },
      },
    ],
    questions: [
      { q: "A follower holds uncommitted entries from an old term that the current leader doesn't have. What happens to them?",
        options: ["The leader adopts them", "They are deleted and replaced with the leader's entries", "They are kept after the leader's entries"],
        answer: 1, why: "They conflict with the leader's log. Since they were never committed, replacing them is safe." },
      { q: "Does a leader ever overwrite entries in its own log?",
        options: ["Yes, when a majority disagrees with it", "No, followers always adapt to the leader", "Only during a partition"],
        answer: 1, why: "The leader's log only grows. Rule 11 (next) makes sure a leader's log already has everything committed." },
    ],
  },
  {
    id: "restriction",
    title: "The vote restriction",
    steps: [
      {
        title: "The problem",
        text: `<p>Followers adapt to the leader's log. So what if a server with a <em>short</em> log, one that missed some committed entries, became leader? It would make the others delete them. A committed entry, gone.</p>
          <p>Raft prevents this at election time.</p>`,
      },
      {
        title: "Only up-to-date candidates win",
        text: `<p>A RequestVote includes the index and term of the candidate's last entry. A server <b>refuses its vote</b> if its own log is more up to date: compare last terms first (the higher term wins), and if they are equal, the longer log wins.</p>
          <p>Why that's enough: a committed entry is on a majority. A winner needs votes from a majority. The two majorities share a server, which has the committed entry and refuses any candidate whose log lacks it. So every leader holds every committed entry: <b>Leader Completeness</b>.</p>`,
        html: fig.uptodate,
      },
      {
        title: "Switch the rule off",
        text: `<p>This is the chaos runner's seed 1 with the classic mistake <code>vote-ignores-log</code> switched on: servers vote without comparing logs. Faults and clients come from the chaos scenario. Watch for the safety check: a leader gets elected that lacks a committed entry.</p>
          <p>Across 1,000 seeds, this mistake broke Leader Completeness in <b>{{mistake.vote-ignores-log}}</b> of them.</p>`,
        scene: { replay: { seed: 1, mistake: "vote-ignores-log", jump: 150 }, speed: 6, logs: true, script: () => [
          { wait: s => !!s.violation, note: (v, s) => `Caught at tick ${s.tick}: ${s.violation.replace(/^tick \d+ \(seed \d+\): /, "")}` },
        ] },
      },
    ],
    questions: [
      { q: "Candidate A's last entry is index 9, term 3. Voter B's last entry is index 12, term 2. Does B vote for A?",
        options: ["Yes: A's last term is higher", "No: B's log is longer", "Only if A has a lower ID"],
        answer: 0, why: "Last terms are compared first. Term 3 beats term 2, whatever the lengths." },
      { q: "What does the vote restriction guarantee?",
        options: ["Elections finish in one round", "Every leader has every committed entry", "Followers never fall behind"],
        answer: 1, why: "That's Leader Completeness, which is what makes “committed” permanent." },
    ],
  },
  {
    id: "figure8",
    title: "Old-term entries and the no-op",
    steps: [
      {
        title: "A trap",
        text: `<p>A new leader may find entries from <em>earlier</em> terms in its log, and may get them onto a majority. Is such an entry committed once a majority has it?</p>
          <p><b>No.</b> This is the famous “Figure 8” case from the Raft paper. A server whose last entry has a higher term could still win an election (the vote restriction compares last terms) and overwrite that entry everywhere.</p>`,
        html: fig.fig8,
      },
      {
        title: "The rule",
        text: `<p>A leader counts replicas only for entries from its <b>current term</b>. Earlier entries become committed indirectly: once a current-term entry after them is committed, everything before it is committed too (Log Matching guarantees they're the same everywhere).</p>
          <p>A current-term entry on a majority can't be overwritten: any server that could win next would need a last term at least as high, and the voters that have that entry refuse anyone who lacks it.</p>`,
      },
      {
        title: "The no-op",
        text: `<p>If clients are quiet, old entries could wait a long time for a current-term entry to come along. So a new leader immediately appends an empty <b>no-op</b> entry in its own term. Once it commits, so does everything before it. (Those are the entries marked with a small ring at the start of each term.)</p>
          <p>Our chaos runner switches the rule off with the mistake <code>commit-old-terms</code> (which also drops the no-op, or the bug would hide). It's rare: <b>{{mistake.commit-old-terms}}</b> of 1,000 seeds caught it. Here is seed 906, fast-forwarded to just before it breaks.</p>`,
        scene: { replay: { seed: 906, mistake: "commit-old-terms", jump: 2560 }, speed: 5, logs: true, script: () => [
          { wait: s => !!s.violation, note: (v, s) => `Caught at tick ${s.tick}: ${s.violation.replace(/^tick \d+ \(seed \d+\): /, "")}` },
        ] },
      },
    ],
    questions: [
      { q: "Why doesn't a leader count replicas of an entry from an earlier term?",
        options: ["It would be slower", "A later leader could still overwrite that entry", "Old entries are always committed already"],
        answer: 1, why: "A server with a higher last term can still win and replace it, so a majority alone isn't enough for old-term entries." },
      { q: "What is the new leader's no-op for?",
        options: ["It tests the network", "Committing it commits all earlier entries without waiting for a client", "It tells clients who the leader is"],
        answer: 1, why: "It's a current-term entry; once committed, everything before it is committed too." },
    ],
  },
  {
    id: "persistence",
    title: "Persistence and crash recovery",
    steps: [
      {
        title: "What must survive a crash",
        text: `<p>Three things go to disk: the <b>current term</b>, <b>who this server voted for</b> in it, and the <b>log</b>. Everything else (the commit index, the key-value store) can be rebuilt.</p>
          <p>And the order matters: a server writes to disk and waits for the disk to confirm (<em>fsync</em>) <b>before</b> it replies to anything.</p>`,
      },
      {
        title: "Forgetting a vote",
        text: `<p>Why before? Every reply is a promise. “You have my vote” counts toward someone's majority; “I stored entry 12” counts toward a commit. A promise lost in a crash breaks the overlap argument that all of Raft rests on.</p>
          <p>Here is the mistake <code>forget-vote</code>, chaos seed 189: servers don't save their vote, crash and reboot often, and one of them votes twice in a term. Watch for two leaders in the same term. ({{mistake.forget-vote}} of 1,000 seeds caught it.)</p>`,
        scene: { replay: { seed: 189, mistake: "forget-vote", jump: 100 }, speed: 5, script: () => [
          { wait: s => !!s.violation, note: (v, s) => `Caught at tick ${s.tick}: ${s.violation.replace(/^tick \d+ \(seed \d+\): /, "")}` },
        ] },
      },
      {
        title: "Restarting",
        text: `<p>On restart a server reads its term, vote and log from disk and starts as a follower. Its key-value store is empty; it rebuilds it by applying committed entries again, as it learns the commit index from the leader.</p>`,
        scene: { nodes: 3, seed: 131, logs: true, stores: true, script: () => [
          { wait: "leader", do: send("PUT", "a", "1"), note: "PUT a=1" },
          { wait: "idle", do: send("PUT", "b", "2"), note: "PUT b=2" },
          { wait: "idle", do: (sim, v, s) => { v.f = followersOf(s)[0]; sim.do({ op: "crash", node: v.f }); }, note: v => `S${v.f} crashes. Its memory is gone; its disk isn't.` },
          { wait: 20, do: (sim, v) => sim.do({ op: "restart", node: v.f }), note: v => `S${v.f} restarts: log from disk, store rebuilt by re-applying committed entries.` },
        ] },
      },
    ],
    questions: [
      { q: "What must a server have on disk before it replies to a vote request?",
        options: ["Its key-value store", "Its current term and its vote", "The leader's address"],
        answer: 1, why: "Otherwise a reboot could let it vote twice in one term." },
      { q: "After a restart, how does a server rebuild its key-value store?",
        options: ["It copies the leader's store", "It applies the committed entries of its log again", "It asks the clients"],
        answer: 1, why: "The store is a function of the committed log, so replaying the log rebuilds it." },
    ],
  },
  {
    id: "clients",
    title: "Clients: redirects, retries, duplicates",
    steps: [
      {
        title: "Finding the leader",
        text: `<p>A client can start at any server. A follower answers “not leader, try S3” (it knows the leader from heartbeats), and the client goes there. If a server doesn't answer at all (down, cut off, message lost), the client times out and tries the next one.</p>`,
        scene: { nodes: 5, seed: 141, script: () => [
          { wait: "leader", do: send("PUT", "k", "v"), note: "A client sends PUT k=v to its first server. Follow the diamonds." },
          { wait: "idle", note: "If it hit a follower, it was redirected, then answered by the leader." },
        ] },
      },
      {
        title: "Retries can duplicate",
        text: `<p>Suppose the leader commits a client's <code>APPEND x+=a</code> and crashes before replying. The client times out and sends it again to the new leader. Applied twice, x ends with “aa”, which nobody asked for.</p>
          <p>The fix: each client has an ID and numbers its requests 1, 2, 3…, reusing the number when it retries. Each server's state machine remembers the last number it applied per client, and its answer. A duplicate is skipped and gets the original answer.</p>`,
      },
      {
        title: "See it on a bad network",
        text: `<p>30% of messages are lost here. Answers get lost too, so clients retry; the event feed shows servers skipping duplicates. Every request is applied once.</p>
          <p>Without the sequence numbers (mistake <code>no-dedupe</code>), the linearizability check failed in <b>{{mistake.no-dedupe}}</b> of 1,000 chaos seeds.</p>`,
        scene: { nodes: 5, seed: 142, feed: true, script: () => [
          { wait: "leader", do: sim => { sim.do({ op: "drop", rate: 0.3 }); sim.do({ op: "traffic", on: true }); }, note: "30% of messages lost; three clients busy." },
        ] },
      },
    ],
    questions: [
      { q: "A client's APPEND was committed, but the reply was lost. It retries with the same sequence number. What happens?",
        options: ["It is applied a second time", "It is recognized as a duplicate: not applied again, and the original answer is returned", "The server rejects it with an error"],
        answer: 1, why: "The server remembers each client's last sequence number and answer." },
      { q: "Why do the tests use APPEND and not only PUT?",
        options: ["APPEND is faster", "Applying a PUT twice usually looks the same as once; APPEND twice doesn't", "PUT isn't supported in the simulator"],
        answer: 1, why: "PUT is idempotent, so a duplicate would go unnoticed. APPEND makes duplicates visible." },
    ],
  },
  {
    id: "linearizability",
    title: "Linearizability",
    steps: [
      {
        title: "What “correct” means",
        text: `<p>For clients, the store should behave like a single copy: every operation seems to happen at one instant between when it was sent and when its answer came back, and those instants respect real time.</p>
          <p>Concretely: once <code>PUT x=5</code> has returned, any <code>GET x</code> that starts later must see 5, or something written after it. This is <b>linearizability</b>.</p>`,
      },
      {
        title: "Why reads go through the log",
        text: `<p>A tempting shortcut: let the leader answer <code>GET</code> from its own store. But a leader that was cut off doesn't know it was replaced. It would serve old data while the real leader accepts new writes: a stale read.</p>
          <p>The chaos runner's mistake <code>local-reads</code> does exactly that, and was caught in <b>{{mistake.local-reads}}</b> of 1,000 seeds. This implementation puts reads in the log instead; it costs a round trip. (Production systems use cheaper tricks, such as confirming leadership with a heartbeat round before reading.)</p>`,
      },
      {
        title: "Checking it",
        text: `<p>The simulator records every request: when it was sent, when it was answered, and the answer. A checker (Porcupine) then searches for one order of all operations that explains everything the clients saw. If no such order exists, the history is not linearizable.</p>
          <p>The Workplace has a “Check linearizability” button that runs the same checker on what you did.</p>`,
      },
    ],
    questions: [
      { q: "Client A's PUT x=5 has returned. Afterwards, client B starts GET x. What may B see?",
        options: ["5, or a value written after it", "Any value x ever had", "Only 5"],
        answer: 0, why: "A later read can't go back before a completed write; a newer write may have happened since." },
      { q: "Why can a leader's read from its own memory be stale?",
        options: ["Memory is slower than disk", "It may have been replaced by a new leader without knowing", "Followers have newer data by design"],
        answer: 1, why: "A partitioned old leader still believes it leads; only a majority round confirms it." },
    ],
  },
  {
    id: "testing",
    title: "Testing it: simulation and chaos",
    steps: [
      {
        title: "Deterministic simulation",
        text: `<p>Distributed bugs hide in rare timings. To find them, the whole cluster (servers, network, disks, clock, clients) runs inside one program with simulated time. One number, the <b>seed</b>, decides every random choice. Same seed, same run, every time. A failing seed is a perfect bug report.</p>
          <p>That's also why this page can replay any chaos run step by step.</p>`,
      },
      {
        title: "Chaos",
        text: `<p>Each chaos run crashes and restarts servers, splits the network, and loses messages while five clients send requests; then removes every fault and requires the cluster to recover. Safety properties are checked on every step, linearizability at the end.</p>
          <p>The latest large run: <b>{{chaos.seeds}}</b> seeds, <b>{{chaos.requests}}</b> client requests, <b>{{chaos.crashes}}</b> crashes, <b>{{chaos.partitions}}</b> partitions, <b>{{chaos.violations}}</b> violations.</p>`,
      },
      {
        title: "Testing the tests",
        text: `<p>A checker that never fires might be checking nothing. So six well-known Raft mistakes can be switched on, and each must be caught. You've met the rule behind each one in this course:</p>
          {{mistakes-table}}
          <p>Some are caught on almost every seed; Figure 8 needs rare timing. That's why many seeds matter.</p>`,
      },
    ],
    questions: [
      { q: "What makes a failing simulation run easy to debug?",
        options: ["It prints a lot of logs", "Its seed replays exactly the same run", "It runs on real servers"],
        answer: 1, why: "Determinism: same seed, same messages, same failure, every time." },
      { q: "How do you know the checkers can actually find bugs?",
        options: ["They passed on 10,000 seeds", "Known bugs are switched on deliberately and each one is caught", "The code was reviewed"],
        answer: 1, why: "Passing alone proves little; catching planted mistakes shows the checks have teeth." },
    ],
  },
  {
    id: "real",
    title: "From simulation to real servers",
    steps: [
      {
        title: "One core, two worlds",
        text: `<p>The Raft rules live in one package that never touches a clock, a network or a disk. It is told “a tick passed” or “this message arrived” and answers with what to save, what to send, and what to apply. The simulator drives it in this page and in the tests; a real server drives it with a 10 ms timer, files and HTTP.</p>
          ${fig.layers}
          <p>A real server repeats one loop: collect what arrived, then <b>save and fsync</b>, then <b>send</b>, then <b>apply</b> and answer clients. In that order, always.</p>`,
      },
      {
        title: "Real numbers",
        text: `<p>Measured on {{bench.machine}}, all servers on one machine, with fsync on:</p>
          {{bench-table}}
          <p>fsync is the expensive part: a single client waits for a disk flush on every write. With many clients, the server collects everything that arrived and flushes once for all of it (<b>group commit</b>), so throughput grows much faster than latency.</p>`,
      },
      {
        title: "Where to go from here",
        text: `<p>You have the whole picture: elections with terms and majorities, a log copied with a consistency check, a commit rule, the vote restriction, the current-term rule and the no-op, persistence, client sessions, linearizability, and how it's all tested.</p>
          <p>Now break it yourself in the <a href="workplace.html">Workplace</a>: every scenario from this course, plus any combination you can think of. The numbers behind the tests are in <a href="under-the-hood.html">Under the hood</a>.</p>`,
      },
    ],
    questions: [
      { q: "Why must a server fsync before it sends its AppendEntries reply?",
        options: ["Replies are larger than entries", "The reply is a promise that counts toward a commit; it must survive a crash", "To slow the leader down"],
        answer: 1, why: "If “stored” could be forgotten, a committed entry might exist on less than a majority." },
      { q: "What lets many concurrent writes share the cost of one fsync?",
        options: ["Group commit: flush everything that arrived together", "Skipping fsync for small writes", "Sending writes to different leaders"],
        answer: 0, why: "The server batches whatever is pending into one disk flush." },
    ],
  },
];
