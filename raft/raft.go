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

// MessageType names the RPCs of the Raft paper, plus their replies.
type MessageType uint8

const (
	// MsgVote is RequestVote: a candidate asks for a vote in its term.
	MsgVote MessageType = iota
	// MsgVoteResp answers MsgVote. Reject is false when the vote is granted.
	MsgVoteResp
	// MsgApp is AppendEntries. Until log replication exists it is the leader's heartbeat.
	MsgApp
	// MsgAppResp answers MsgApp. A rejection carries the receiver's newer term.
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
	return nil
}

// Status is a read-only snapshot of a node's election state.
type Status struct {
	ID    NodeID
	State StateType
	Term  uint64
	Vote  NodeID // who this node voted for in Term
	Lead  NodeID // the leader this node knows of in Term
}

// Ready is what the node wants the outside world to do after a Tick or Step.
type Ready struct {
	Messages []Message
}

// Node is one Raft server's state machine. It never does I/O and never reads a clock: the caller
// feeds it time with Tick and messages with Step, and collects its output with Ready.
type Node struct {
	id    NodeID
	peers []NodeID
	rand  Rand

	state StateType
	term  uint64
	vote  NodeID
	lead  NodeID

	votes map[NodeID]bool // granted votes received in the current election

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
		electionTick:  c.ElectionTick,
		heartbeatTick: c.HeartbeatTick,
	}
	n.becomeFollower(0, None)
	return n, nil
}

// Status reports the node's current role, term, vote and known leader.
func (n *Node) Status() Status {
	return Status{ID: n.id, State: n.state, Term: n.term, Vote: n.vote, Lead: n.lead}
}

// Ready returns the messages produced since the last call and clears them.
func (n *Node) Ready() Ready {
	rd := Ready{Messages: n.msgs}
	n.msgs = nil
	return rd
}

// Tick advances the node's clock by one tick.
func (n *Node) Tick() {
	if n.state == StateLeader {
		n.heartbeatElapsed++
		if n.heartbeatElapsed >= n.heartbeatTick {
			n.heartbeatElapsed = 0
			n.broadcastHeartbeat()
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
			n.send(Message{Type: MsgAppResp, To: m.From, Reject: true})
		}
		return
	}

	switch m.Type {
	case MsgVote:
		n.handleVote(m)
	case MsgVoteResp:
		n.handleVoteResp(m)
	case MsgApp:
		n.handleHeartbeat(m)
	case MsgAppResp:
		// Nothing to do until log replication: a newer term was already handled above.
	}
}

func (n *Node) handleVote(m Message) {
	// One vote per term. Granting again to the same candidate is safe (its request may have
	// been retried or its first reply lost).
	grant := n.vote == None || n.vote == m.From
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

func (n *Node) handleHeartbeat(m Message) {
	// Same term: m.From won this term's election, so a candidate gives up.
	if n.state != StateFollower {
		n.becomeFollower(n.term, m.From)
	}
	n.lead = m.From
	n.resetElectionTimer()
	n.send(Message{Type: MsgAppResp, To: m.From})
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
			n.send(Message{Type: MsgVote, To: p})
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
	n.resetElectionTimer()
}

func (n *Node) becomeLeader() {
	n.state = StateLeader
	n.lead = n.id
	n.votes = nil
	n.heartbeatElapsed = 0
	n.broadcastHeartbeat() // tell everyone right away, before anyone else times out
}

func (n *Node) broadcastHeartbeat() {
	for _, p := range n.peers {
		if p != n.id {
			n.send(Message{Type: MsgApp, To: p})
		}
	}
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
