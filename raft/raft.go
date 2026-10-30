package raft

import (
	"errors"
	"fmt"
)

// NodeID identifies a server in the cluster. Zero means "nobody".
type NodeID uint64

// None is the zero NodeID: no vote cast, no known leader.
const None NodeID = 0

// StateType is the role a node plays in its current term.
type StateType uint8

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

func (s StateType) String() string {
	switch s {
	case StateFollower:
		return "follower"
	case StateCandidate:
		return "candidate"
	case StateLeader:
		return "leader"
	}
	return fmt.Sprintf("StateType(%d)", uint8(s))
}

// ErrNotLeader is returned by Propose on a node that is not the leader.
var ErrNotLeader = errors.New("raft: not the leader")

// Entry is one command in the replicated log. Index starts at 1. Term is the term of the leader
// that created it. An entry with no Data is a no-op a new leader writes at the start of its term.
type Entry struct {
	Term  uint64
	Index uint64
	Data  []byte
}

// MessageType names the RPCs of the Raft paper, plus their replies.
type MessageType uint8

const (
	// MsgVote is RequestVote: a candidate asks for a vote in its term.
	MsgVote MessageType = iota
	// MsgVoteResp answers MsgVote. Reject is false when the vote is granted.
	MsgVoteResp
	// MsgApp is AppendEntries: entries to place right after (Index, LogTerm), plus the leader's
	// commit index. With no entries it is the heartbeat.
	MsgApp
	// MsgAppResp answers MsgApp. On success Index is the last entry the message covered. On a
	// rejection Index is the rejected prev index and Hint the receiver's last index; a rejection
	// with a newer Term tells a stale leader it was replaced.
	MsgAppResp
)

func (t MessageType) String() string {
	switch t {
	case MsgVote:
		return "MsgVote"
	case MsgVoteResp:
		return "MsgVoteResp"
	case MsgApp:
		return "MsgApp"
	case MsgAppResp:
		return "MsgAppResp"
	}
	return fmt.Sprintf("MessageType(%d)", uint8(t))
}

// Message is one message between two nodes. Term is always the sender's current term.
type Message struct {
	Type   MessageType
	From   NodeID
	To     NodeID
	Term   uint64
	Reject bool

	// Index and LogTerm: for MsgApp the entry the new entries follow; for MsgVote the candidate's
	// last entry; for MsgAppResp see MsgAppResp.
	Index   uint64
	LogTerm uint64
	Entries []Entry
	Commit  uint64 // MsgApp: the leader's commit index
	Hint    uint64 // MsgAppResp rejection: the receiver's last index
}

// Rand is the source of randomness for election timeouts. The caller injects it, seeded, so a
// run can be replayed exactly. *math/rand/v2.Rand satisfies it.
type Rand interface {
	IntN(n int) int
}

// Config describes one node. All time is counted in ticks of the caller's clock.
type Config struct {
	ID    NodeID
	Peers []NodeID // every member of the cluster, including ID

	// ElectionTick is the minimum election timeout. Each timeout is drawn at random from
	// [ElectionTick, 2*ElectionTick) so that nodes rarely time out together.
	ElectionTick int
	// HeartbeatTick is how often a leader sends heartbeats. Keep it well below ElectionTick.
	HeartbeatTick int

	Rand Rand

	// Mistakes switches on well-known Raft bugs on purpose, to show that the tests catch them.
	// Always zero in real use.
	Mistakes Mistakes

	// HardState and Entries restore a node from storage after a restart. Leave them empty for a
	// brand-new node.
	HardState HardState
	Entries   []Entry
}

// Mistakes are classic Raft bugs that can be switched on deliberately (for the "I tried to break
// it" demos). Each one breaks a rule from the Raft paper.
type Mistakes struct {
	// CommitOldTerms lets a leader commit an entry from an earlier term just because a majority
	// stores it (Figure 8 of the paper). It also drops the new leader's no-op entry: with the no-op
	// in place, every acknowledgement covers a current-term entry too, which hides this bug.
	CommitOldTerms bool
	// VoteIgnoresLog grants votes without checking that the candidate's log is up to date.
	VoteIgnoresLog bool
	// AcceptStaleLeader lets a follower take entries from a leader of an older term instead of
	// rejecting them.
	AcceptStaleLeader bool
}

// HardState is the part of a node's state that must be on stable storage before the node sends
// any message that depends on it: its term, its vote in that term, and how far it knows the log
// is committed.
type HardState struct {
	Term   uint64
	Vote   NodeID
	Commit uint64
}

func (c Config) validate() error {
	if c.ID == None {
		return errors.New("raft: ID must not be zero")
	}
	if c.ElectionTick < 2 || c.HeartbeatTick < 1 || c.HeartbeatTick >= c.ElectionTick {
		return errors.New("raft: need 1 <= HeartbeatTick < ElectionTick and ElectionTick >= 2")
	}
	if c.Rand == nil {
		return errors.New("raft: Rand is required")
	}
	seen := map[NodeID]bool{}
	for _, p := range c.Peers {
		if p == None || seen[p] {
			return fmt.Errorf("raft: peer list has a zero or duplicate ID %d", p)
		}
		seen[p] = true
	}
	if !seen[c.ID] {
		return fmt.Errorf("raft: peer list does not contain own ID %d", c.ID)
	}
	prevTerm := uint64(0)
	for i, e := range c.Entries {
		if e.Index != uint64(i+1) || e.Term < prevTerm || e.Term > c.HardState.Term {
			return fmt.Errorf("raft: restored entry %d (index %d, term %d) is out of place", i, e.Index, e.Term)
		}
		prevTerm = e.Term
	}
	if c.HardState.Commit > uint64(len(c.Entries)) {
		return fmt.Errorf("raft: restored commit %d is past the log (%d entries)", c.HardState.Commit, len(c.Entries))
	}
	return nil
}

// Status is a read-only snapshot of a node's state.
type Status struct {
	ID        NodeID
	State     StateType
	Term      uint64
	Vote      NodeID // who this node voted for in Term
	Lead      NodeID // the leader this node knows of in Term
	Commit    uint64 // highest log index known to be committed
	LastIndex uint64 // index of the last entry in the log
}

// Ready is what the node wants the outside world to do after a Tick, Step or Propose. The caller
// must handle it in this order:
//
//  1. Write HardState (if not nil) and Entries to stable storage, and sync. Entries replace
//     everything in storage from Entries[0].Index on.
//  2. Send Messages. They may promise things (a vote, "I have your entries") that are only true
//     once step 1 is durable.
//  3. Apply CommittedEntries to the state machine.
type Ready struct {
	HardState *HardState
	Entries   []Entry
	Messages  []Message
	// CommittedEntries are newly committed entries, in log order, each delivered exactly once.
	// After a restart they are delivered again from index 1, so the state machine can be rebuilt.
	CommittedEntries []Entry
}

// Node is one Raft server's state machine. It never does I/O and never reads a clock: the caller
// feeds it time with Tick and messages with Step, and collects its output with Ready.
type Node struct {
	id    NodeID
	peers []NodeID
	rand  Rand
	bugs  Mistakes

	state StateType
	term  uint64
	vote  NodeID
	lead  NodeID

	votes map[NodeID]bool // granted votes received in the current election

	log      []Entry // log[i] has Index i+1
	commit   uint64
	applied  uint64    // highest index handed out in Ready.CommittedEntries
	unstable uint64    // first index not yet handed out in Ready.Entries
	prevHS   HardState // hard state as last handed out in Ready

	// Leader only: for each follower, the next index to send and the highest index known to match.
	next  map[NodeID]uint64
	match map[NodeID]uint64

	electionTick      int
	heartbeatTick     int
	electionElapsed   int
	heartbeatElapsed  int
	randomizedTimeout int

	msgs []Message
}

// New returns a follower in term 0.
func New(c Config) (*Node, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	n := &Node{
		id:            c.ID,
		peers:         append([]NodeID(nil), c.Peers...),
		rand:          c.Rand,
		bugs:          c.Mistakes,
		electionTick:  c.ElectionTick,
		heartbeatTick: c.HeartbeatTick,
		term:          c.HardState.Term,
		vote:          c.HardState.Vote,
		commit:        c.HardState.Commit,
		log:           append([]Entry(nil), c.Entries...),
		prevHS:        c.HardState,
	}
	n.unstable = n.lastIndex() + 1
	n.becomeFollower(n.term, None) // same term: keeps the restored vote
	return n, nil
}

// Status reports the node's current role, term, vote and known leader.
func (n *Node) Status() Status {
	return Status{ID: n.id, State: n.state, Term: n.term, Vote: n.vote, Lead: n.lead, Commit: n.commit, LastIndex: n.lastIndex()}
}

// Log returns a copy of the node's log.
func (n *Node) Log() []Entry { return append([]Entry(nil), n.log...) }

// Ready returns the messages and newly committed entries produced since the last call and clears
// them.
func (n *Node) Ready() Ready {
	rd := Ready{Messages: n.msgs}
	n.msgs = nil
	if hs := (HardState{Term: n.term, Vote: n.vote, Commit: n.commit}); hs != n.prevHS {
		rd.HardState = &hs
		n.prevHS = hs
	}
	if n.unstable <= n.lastIndex() {
		rd.Entries = append([]Entry(nil), n.log[n.unstable-1:]...)
	}
	n.unstable = n.lastIndex() + 1
	if n.commit > n.applied {
		rd.CommittedEntries = append([]Entry(nil), n.log[n.applied:n.commit]...)
		n.applied = n.commit
	}
	return rd
}

// Propose appends a command to the leader's log and starts replicating it. It is not committed
// until it shows up in Ready.CommittedEntries.
func (n *Node) Propose(data []byte) error {
	if n.state != StateLeader {
		return ErrNotLeader
	}
	n.appendEntry(data)
	return nil
}

// Tick advances the node's clock by one tick.
func (n *Node) Tick() {
	if n.state == StateLeader {
		n.heartbeatElapsed++
		if n.heartbeatElapsed >= n.heartbeatTick {
			n.heartbeatElapsed = 0
			n.broadcastAppend()
		}
		return
	}
	n.electionElapsed++
	if n.electionElapsed >= n.randomizedTimeout {
		n.campaign()
	}
}

// Step processes one incoming message.
func (n *Node) Step(m Message) {
	if !n.isMember(m.From) || m.From == n.id {
		return
	}

	// Rule for all servers: a newer term means our view of the world is out of date.
	if m.Term > n.term {
		lead := None
		if m.Type == MsgApp {
			lead = m.From
		}
		n.becomeFollower(m.Term, lead)
	}

	if m.Term < n.term {
		// A message from an older term. Answer requests so the sender learns the newer term;
		// stale replies need no answer.
		switch m.Type {
		case MsgVote:
			n.send(Message{Type: MsgVoteResp, To: m.From, Reject: true})
		case MsgApp:
			if n.bugs.AcceptStaleLeader {
				n.handleAppend(m)
				return
			}
			n.send(Message{Type: MsgAppResp, To: m.From, Reject: true, Index: m.Index, Hint: n.lastIndex()})
		}
		return
	}

	switch m.Type {
	case MsgVote:
		n.handleVote(m)
	case MsgVoteResp:
		n.handleVoteResp(m)
	case MsgApp:
		n.handleAppend(m)
	case MsgAppResp:
		n.handleAppendResp(m)
	}
}

func (n *Node) handleVote(m Message) {
	// One vote per term. Granting again to the same candidate is safe (its request may have
	// been retried or its first reply lost). And never vote for a candidate whose log is behind
	// ours: it could be missing committed entries.
	grant := (n.vote == None || n.vote == m.From) && (n.bugs.VoteIgnoresLog || n.logUpToDate(m.Index, m.LogTerm))
	if grant {
		n.vote = m.From
		n.resetElectionTimer()
	}
	n.send(Message{Type: MsgVoteResp, To: m.From, Reject: !grant})
}

func (n *Node) handleVoteResp(m Message) {
	if n.state != StateCandidate || m.Reject {
		return
	}
	n.votes[m.From] = true
	if len(n.votes) >= n.quorum() {
		n.becomeLeader()
	}
}

func (n *Node) handleAppend(m Message) {
	// Same term: m.From won this term's election, so a candidate gives up.
	if n.state != StateFollower {
		n.becomeFollower(n.term, m.From)
	}
	n.lead = m.From
	n.resetElectionTimer()

	// Consistency check: the new entries must follow an entry we hold with the same term.
	if m.Index > n.lastIndex() || n.termAt(m.Index) != m.LogTerm {
		n.send(Message{Type: MsgAppResp, To: m.From, Reject: true, Index: m.Index, Hint: n.lastIndex()})
		return
	}
	for i, e := range m.Entries {
		if e.Index <= n.lastIndex() {
			if n.termAt(e.Index) == e.Term {
				continue // already have it; a late duplicate must not truncate what follows
			}
			if e.Index <= n.commit {
				panic(fmt.Sprintf("raft: node %d asked to overwrite committed entry %d", n.id, e.Index))
			}
			n.log = n.log[:e.Index-1]
		}
		n.unstable = min(n.unstable, e.Index)
		n.log = append(n.log, m.Entries[i:]...)
		break
	}
	// Only entries this message verified against the leader's log may be marked committed.
	lastNew := m.Index + uint64(len(m.Entries))
	if c := min(m.Commit, lastNew); c > n.commit {
		n.commit = c
	}
	n.send(Message{Type: MsgAppResp, To: m.From, Index: lastNew})
}

func (n *Node) handleAppendResp(m Message) {
	if n.state != StateLeader {
		return
	}
	if m.Reject {
		if m.Index != n.next[m.From]-1 {
			return // answers an append we have since moved past
		}
		next := min(m.Index, m.Hint+1)
		n.next[m.From] = max(next, n.match[m.From]+1)
		n.sendAppend(m.From)
		return
	}
	if m.Index > n.match[m.From] {
		n.match[m.From] = m.Index
	}
	n.next[m.From] = max(n.next[m.From], m.Index+1)
	n.maybeCommit()
}

// maybeCommit applies the commit rule: the highest index stored on a majority whose entry is from
// the current term. Entries from earlier terms are committed only along with such an entry.
func (n *Node) maybeCommit() {
	for idx := n.lastIndex(); idx > n.commit; idx-- {
		if n.termAt(idx) != n.term && !n.bugs.CommitOldTerms {
			return // terms only decrease going back, so no earlier index qualifies either
		}
		copies := 1 // the leader's own
		for _, p := range n.peers {
			if p != n.id && n.match[p] >= idx {
				copies++
			}
		}
		if copies >= n.quorum() {
			n.commit = idx
			return
		}
	}
}

func (n *Node) campaign() {
	n.state = StateCandidate
	n.term++
	n.vote = n.id
	n.lead = None
	n.votes = map[NodeID]bool{n.id: true}
	n.resetElectionTimer()
	if len(n.votes) >= n.quorum() {
		n.becomeLeader()
		return
	}
	for _, p := range n.peers {
		if p != n.id {
			n.send(Message{Type: MsgVote, To: p, Index: n.lastIndex(), LogTerm: n.termAt(n.lastIndex())})
		}
	}
}

func (n *Node) becomeFollower(term uint64, lead NodeID) {
	if term > n.term {
		n.term = term
		n.vote = None
	}
	n.state = StateFollower
	n.lead = lead
	n.votes = nil
	n.next, n.match = nil, nil
	n.resetElectionTimer()
}

func (n *Node) becomeLeader() {
	n.state = StateLeader
	n.lead = n.id
	n.votes = nil
	n.heartbeatElapsed = 0
	n.next, n.match = map[NodeID]uint64{}, map[NodeID]uint64{}
	for _, p := range n.peers {
		n.next[p] = n.lastIndex() + 1
	}
	// A no-op from the new term: once it commits, every earlier entry is committed with it. It
	// also goes out right away as the first heartbeat, before anyone else times out.
	if n.bugs.CommitOldTerms {
		n.broadcastAppend()
		return
	}
	n.appendEntry(nil)
}

// appendEntry adds a new entry from the leader's term, sends it out and commits it if the
// leader alone is a majority.
func (n *Node) appendEntry(data []byte) {
	n.log = append(n.log, Entry{Term: n.term, Index: n.lastIndex() + 1, Data: data})
	n.broadcastAppend()
	n.maybeCommit()
}

func (n *Node) broadcastAppend() {
	for _, p := range n.peers {
		if p != n.id {
			n.sendAppend(p)
		}
	}
}

// sendAppend sends a follower everything from its next index on. Until it acknowledges, every
// heartbeat repeats this, which also covers lost messages.
func (n *Node) sendAppend(to NodeID) {
	prev := n.next[to] - 1
	n.send(Message{
		Type:    MsgApp,
		To:      to,
		Index:   prev,
		LogTerm: n.termAt(prev),
		Entries: append([]Entry(nil), n.log[prev:]...), // a copy: our log may be truncated later
		Commit:  n.commit,
	})
}

func (n *Node) lastIndex() uint64 { return uint64(len(n.log)) }

// termAt returns the term of the entry at idx, or 0 for index 0 and indexes past the end.
func (n *Node) termAt(idx uint64) uint64 {
	if idx == 0 || idx > n.lastIndex() {
		return 0
	}
	return n.log[idx-1].Term
}

// logUpToDate reports whether a log ending at (lastIndex, lastTerm) is at least as up to date as
// ours: a later last term wins, and with equal last terms the longer log wins.
func (n *Node) logUpToDate(lastIndex, lastTerm uint64) bool {
	ourTerm := n.termAt(n.lastIndex())
	return lastTerm > ourTerm || (lastTerm == ourTerm && lastIndex >= n.lastIndex())
}

func (n *Node) resetElectionTimer() {
	n.electionElapsed = 0
	n.randomizedTimeout = n.electionTick + n.rand.IntN(n.electionTick)
}

func (n *Node) send(m Message) {
	m.From = n.id
	m.Term = n.term
	n.msgs = append(n.msgs, m)
}

func (n *Node) quorum() int { return len(n.peers)/2 + 1 }

func (n *Node) isMember(id NodeID) bool {
	for _, p := range n.peers {
		if p == id {
			return true
		}
	}
	return false
}
