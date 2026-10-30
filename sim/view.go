package sim

import (
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
)

// This file lets a viewer (the browser playground) watch the simulation: messages in flight with
// where they are going and when they land, the clients, and a stream of events. None of it
// changes what the simulation does; recording events is off unless asked for.

// Flight is one message on the wire, between servers or between a client and a server.
type Flight struct {
	ID        int         `json:"id"`   // unique per message, stable while it travels
	Kind      string      `json:"kind"` // vote, vote-resp, append, append-resp, request, reply
	From      raft.NodeID `json:"from"` // 0 when a client sent it
	To        raft.NodeID `json:"to"`   // 0 when it goes to a client
	Client    uint64      `json:"client,omitempty"`
	SentAt    int         `json:"sent_at"`
	DeliverAt int         `json:"deliver_at"`

	Term    uint64 `json:"term,omitempty"`
	Reject  bool   `json:"reject,omitempty"`
	Index   uint64 `json:"index,omitempty"`
	LogTerm uint64 `json:"log_term,omitempty"`
	Commit  uint64 `json:"commit,omitempty"`
	Entries int    `json:"entries,omitempty"`

	Command   string `json:"command,omitempty"`    // client request, e.g. "PUT x=5"
	Result    string `json:"result,omitempty"`     // client reply
	NotLeader bool   `json:"not_leader,omitempty"` // client reply: try the hinted leader
}

// Event is something that happened, for a viewer's timeline.
type Event struct {
	Tick   int         `json:"tick"`
	Kind   string      `json:"kind"`
	Node   raft.NodeID `json:"node,omitempty"`
	Client uint64      `json:"client,omitempty"`
	Term   uint64      `json:"term,omitempty"`
	Index  uint64      `json:"index,omitempty"`
	Text   string      `json:"text,omitempty"`
	Flight *Flight     `json:"flight,omitempty"` // for lost and dropped messages
}

// Event kinds.
const (
	EvLost      = "lost"      // a message was lost on the way (drop rate)
	EvDropped   = "dropped"   // a message arrived at a crashed node or across a partition
	EvRole      = "role"      // a node became follower, candidate or leader (Text), in Term
	EvCommit    = "commit"    // a node learned that entries up to Index are committed
	EvCrash     = "crash"     //
	EvRestart   = "restart"   //
	EvPartition = "partition" // Text describes the groups
	EvHeal      = "heal"      //
	EvNetwork   = "network"   // drop rate or delay changed; Text says how
	EvCall      = "call"      // a client started a request (Text)
	EvRetry     = "retry"     // a client timed out and tries another server (Node)
	EvRedirect  = "redirect"  // a client was told to go to Node instead
	EvReturn    = "return"    // a client got its answer (Text)
	EvDuplicate = "duplicate" // a server skipped a command it had already applied
	EvViolation = "violation" // a safety check failed; Text says which and why
)

// ClientView is one client's state.
type ClientView struct {
	ID      uint64      `json:"id"`
	Busy    bool        `json:"busy"`
	Target  raft.NodeID `json:"target"`
	Seq     uint64      `json:"seq"`
	Command string      `json:"command,omitempty"` // the request in progress, or the last one
	Retries int         `json:"retries"`
}

// RecordEvents switches the event stream on or off.
func (c *Cluster) RecordEvents(on bool) {
	c.recording = on
	if on && c.seen == nil {
		c.seen = map[raft.NodeID]raft.Status{}
		for _, id := range c.ids {
			c.seen[id] = c.Status(id)
		}
	}
}

// Events returns the events since the last call.
func (c *Cluster) Events() []Event {
	ev := c.events
	c.events = nil
	return ev
}

func (c *Cluster) emit(e Event) {
	if !c.recording {
		return
	}
	e.Tick = c.now
	c.events = append(c.events, e)
}

// observe turns a node's change of role, term or commit index into events.
func (c *Cluster) observe(id raft.NodeID) {
	if !c.recording {
		return
	}
	s, old := c.Status(id), c.seen[id]
	if s.State != old.State || s.Term != old.Term {
		c.emit(Event{Kind: EvRole, Node: id, Term: s.Term, Text: s.State.String()})
	}
	if s.Commit > old.Commit {
		c.emit(Event{Kind: EvCommit, Node: id, Term: s.Term, Index: s.Commit})
	}
	c.seen[id] = s
}

func (c *Cluster) lost(e envelope) {
	if c.recording {
		f := c.flight(e)
		c.emit(Event{Kind: EvLost, Node: f.From, Flight: &f})
	}
}

func (c *Cluster) dropped(e envelope) {
	if c.recording {
		f := c.flight(e)
		c.emit(Event{Kind: EvDropped, Node: f.To, Flight: &f})
	}
}

// Inflight returns every message on the wire, in delivery order.
func (c *Cluster) Inflight() []Flight {
	out := make([]Flight, 0, len(c.inflight))
	for _, e := range c.inflight {
		out = append(out, c.flight(e))
	}
	return out
}

func (c *Cluster) flight(e envelope) Flight {
	f := Flight{ID: e.seq, SentAt: e.sentAt, DeliverAt: e.deliverAt}
	switch {
	case e.req != nil:
		f.Kind, f.To, f.Client, f.Command = "request", e.req.to, e.req.cmd.Client, describeCmd(e.req.cmd)
	case e.resp != nil:
		f.Kind, f.From, f.Client, f.NotLeader = "reply", e.resp.from, e.resp.client, e.resp.notLeader
		if !e.resp.notLeader {
			f.Result = describeResult(e.resp.result)
		}
	default:
		m := e.msg
		f.From, f.To, f.Term, f.Reject = m.From, m.To, m.Term, m.Reject
		f.Index, f.LogTerm, f.Commit, f.Entries = m.Index, m.LogTerm, m.Commit, len(m.Entries)
		switch m.Type {
		case raft.MsgVote:
			f.Kind = "vote"
		case raft.MsgVoteResp:
			f.Kind = "vote-resp"
		case raft.MsgApp:
			f.Kind = "append"
		case raft.MsgAppResp:
			f.Kind = "append-resp"
		}
	}
	return f
}

// describeCmd is a command without the client bookkeeping, e.g. "APPEND x+=ab".
func describeCmd(cmd kv.Command) string {
	switch cmd.Op {
	case kv.OpPut:
		return "PUT " + cmd.Key + "=" + cmd.Value
	case kv.OpAppend:
		return "APPEND " + cmd.Key + "+=" + cmd.Value
	}
	return cmd.Op.String() + " " + cmd.Key
}

func describeResult(r kv.Result) string {
	if r.Found {
		return r.Value
	}
	return "ok"
}

// DescribeEntry is a log entry as a viewer shows it: "no-op" or the command.
func DescribeEntry(e raft.Entry) string {
	if e.Data == nil {
		return "no-op"
	}
	cmd, err := kv.Decode(e.Data)
	if err != nil {
		return "?"
	}
	return describeCmd(cmd)
}

// Clients returns every client's state.
func (c *Cluster) Clients() []ClientView {
	out := make([]ClientView, 0, len(c.clients))
	for _, cl := range c.clients {
		v := ClientView{ID: cl.id, Busy: cl.busy, Target: cl.target, Seq: cl.seq, Retries: cl.retries}
		if cl.seq > 0 {
			v.Command = describeCmd(cl.cmd)
		}
		out = append(out, v)
	}
	return out
}

// Group returns a node's side of the current partition (0 when the network is whole).
func (c *Cluster) Group(id raft.NodeID) int {
	if c.group == nil {
		return 0
	}
	return c.group[id]
}

// IDs returns the node IDs, in order.
func (c *Cluster) IDs() []raft.NodeID { return append([]raft.NodeID(nil), c.ids...) }

// SetDelay sets how many ticks newly sent messages take, at least min and at most max.
func (c *Cluster) SetDelay(min, max int) {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	c.cfg.MinDelay, c.cfg.MaxDelay = min, max
	c.emit(Event{Kind: EvNetwork, Text: "delay " + itoa(min) + "–" + itoa(max) + " ticks"})
}

// Delay returns the current message delay range in ticks.
func (c *Cluster) Delay() (min, max int) { return c.cfg.MinDelay, c.cfg.MaxDelay }

// DropRate returns the current probability that a message is lost.
func (c *Cluster) DropRate() float64 { return c.dropRate }

// Mistakes returns the classic mistakes this cluster runs with.
func (c *Cluster) Mistakes() Mistakes { return c.cfg.Mistakes }
