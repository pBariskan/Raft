package sim

import (
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/pBariskan/Raft/raft"
)

// Config describes a simulated cluster. Zero values get sensible defaults.
type Config struct {
	Seed  uint64 // the only source of randomness: same seed, same run
	Nodes int    // cluster size, IDs 1..Nodes (default 5)

	ElectionTick  int // default 10
	HeartbeatTick int // default 1

	MinDelay int     // minimum message delay in ticks (default 1)
	MaxDelay int     // maximum message delay in ticks (default 3)
	DropRate float64 // probability that any message is lost
}

func (c *Config) setDefaults() {
	if c.Nodes == 0 {
		c.Nodes = 5
	}
	if c.ElectionTick == 0 {
		c.ElectionTick = 10
	}
	if c.HeartbeatTick == 0 {
		c.HeartbeatTick = 1
	}
	if c.MinDelay == 0 {
		c.MinDelay = 1
	}
	if c.MaxDelay < c.MinDelay {
		c.MaxDelay = c.MinDelay + 2
	}
}

type envelope struct {
	msg       raft.Message
	deliverAt int
	seq       int // send order, breaks ties so delivery order is deterministic
}

type node struct {
	rn *raft.Node
	up bool
}

// Cluster is a deterministic simulation of a Raft cluster: a virtual clock counted in ticks, a
// network that delays, drops and partitions messages, and nodes that crash and restart.
//
// Until persistence exists (M3), Restart resumes a crashed node with its memory intact, as if it
// had been paused rather than rebooted.
type Cluster struct {
	cfg   Config
	net   *rand.Rand
	now   int
	seq   int
	ids   []raft.NodeID
	nodes map[raft.NodeID]*node

	inflight []envelope
	group    map[raft.NodeID]int // partition group per node; nil means fully connected
	dropRate float64

	leaderOf map[uint64]raft.NodeID // every leader ever seen, per term
	err      error                  // first safety violation
}

// New builds a cluster of followers in term 0.
func New(cfg Config) *Cluster {
	cfg.setDefaults()
	c := &Cluster{
		cfg:      cfg,
		net:      rand.New(rand.NewPCG(cfg.Seed, 0x6e6574)),
		nodes:    map[raft.NodeID]*node{},
		dropRate: cfg.DropRate,
		leaderOf: map[uint64]raft.NodeID{},
	}
	for i := 1; i <= cfg.Nodes; i++ {
		c.ids = append(c.ids, raft.NodeID(i))
	}
	for _, id := range c.ids {
		rn, err := raft.New(raft.Config{
			ID:            id,
			Peers:         c.ids,
			ElectionTick:  cfg.ElectionTick,
			HeartbeatTick: cfg.HeartbeatTick,
			Rand:          rand.New(rand.NewPCG(cfg.Seed, uint64(id))),
		})
		if err != nil {
			panic(err) // the simulator builds the config itself; an error is a bug here
		}
		c.nodes[id] = &node{rn: rn, up: true}
	}
	return c
}

// Seed returns the seed this cluster was built from.
func (c *Cluster) Seed() uint64 { return c.cfg.Seed }

// Now returns the number of ticks since the start.
func (c *Cluster) Now() int { return c.now }

// Err returns the first safety violation seen, or nil.
func (c *Cluster) Err() error { return c.err }

// Tick advances virtual time by one tick: due messages are delivered in a fixed order, then
// every running node's clock ticks.
func (c *Cluster) Tick() {
	c.now++
	sort.Slice(c.inflight, func(i, j int) bool {
		a, b := c.inflight[i], c.inflight[j]
		if a.deliverAt != b.deliverAt {
			return a.deliverAt < b.deliverAt
		}
		return a.seq < b.seq
	})
	due := 0
	for due < len(c.inflight) && c.inflight[due].deliverAt <= c.now {
		due++
	}
	deliver := c.inflight[:due:due]
	c.inflight = append([]envelope(nil), c.inflight[due:]...)
	for _, e := range deliver {
		to := c.nodes[e.msg.To]
		if !to.up || !c.connected(e.msg.From, e.msg.To) {
			continue
		}
		to.rn.Step(e.msg)
		c.after(e.msg.To)
	}
	for _, id := range c.ids {
		if c.nodes[id].up {
			c.nodes[id].rn.Tick()
			c.after(id)
		}
	}
}

// Run advances n ticks.
func (c *Cluster) Run(n int) {
	for i := 0; i < n; i++ {
		c.Tick()
	}
}

// RunUntil ticks until cond holds or maxTicks pass, and reports whether cond held.
func (c *Cluster) RunUntil(cond func() bool, maxTicks int) bool {
	for i := 0; i < maxTicks; i++ {
		if cond() {
			return true
		}
		c.Tick()
	}
	return cond()
}

// after collects a node's outgoing messages and checks safety once it has acted.
func (c *Cluster) after(id raft.NodeID) {
	rn := c.nodes[id].rn
	for _, m := range rn.Ready().Messages {
		c.send(m)
	}
	c.checkElectionSafety(rn.Status())
}

func (c *Cluster) send(m raft.Message) {
	c.seq++
	// Draw both numbers for every message, so a partition or drop rate change does not shift
	// the random sequence of everything that follows.
	lost := c.net.Float64() < c.dropRate
	delay := c.cfg.MinDelay + c.net.IntN(c.cfg.MaxDelay-c.cfg.MinDelay+1)
	if lost || !c.connected(m.From, m.To) {
		return
	}
	c.inflight = append(c.inflight, envelope{msg: m, deliverAt: c.now + delay, seq: c.seq})
}

// checkElectionSafety enforces the paper's Election Safety property: at most one leader per term,
// over the whole run.
func (c *Cluster) checkElectionSafety(s raft.Status) {
	if s.State != raft.StateLeader || c.err != nil {
		return
	}
	if prev, ok := c.leaderOf[s.Term]; ok && prev != s.ID {
		c.err = fmt.Errorf("election safety violated at tick %d (seed %d): term %d has two leaders, %d and %d",
			c.now, c.cfg.Seed, s.Term, prev, s.ID)
		return
	}
	c.leaderOf[s.Term] = s.ID
}

func (c *Cluster) connected(a, b raft.NodeID) bool {
	return c.group == nil || c.group[a] == c.group[b]
}

// Crash stops a node: it no longer ticks, and messages sent to it are lost.
func (c *Cluster) Crash(id raft.NodeID) { c.nodes[id].up = false }

// Restart resumes a crashed node (see the Cluster comment on what it remembers).
func (c *Cluster) Restart(id raft.NodeID) { c.nodes[id].up = true }

// IsUp reports whether a node is running.
func (c *Cluster) IsUp(id raft.NodeID) bool { return c.nodes[id].up }

// Partition splits the network into the given groups. Nodes in different groups cannot reach
// each other; messages already in flight between them are lost. Nodes not listed form one extra
// group together.
func (c *Cluster) Partition(groups ...[]raft.NodeID) {
	c.group = map[raft.NodeID]int{}
	for _, id := range c.ids {
		c.group[id] = len(groups)
	}
	for g, ids := range groups {
		for _, id := range ids {
			c.group[id] = g
		}
	}
}

// Heal reconnects every node.
func (c *Cluster) Heal() { c.group = nil }

// SetDropRate sets the probability that any newly sent message is lost.
func (c *Cluster) SetDropRate(p float64) { c.dropRate = p }

// Status returns one node's state.
func (c *Cluster) Status(id raft.NodeID) raft.Status { return c.nodes[id].rn.Status() }

// Statuses returns every node's state, in ID order.
func (c *Cluster) Statuses() []raft.Status {
	out := make([]raft.Status, 0, len(c.ids))
	for _, id := range c.ids {
		out = append(out, c.Status(id))
	}
	return out
}

// StableLeader returns a running leader that a majority of the cluster follows: the leader and
// enough running nodes it can reach, all in its term and naming it as leader.
func (c *Cluster) StableLeader() (raft.NodeID, bool) {
	for _, id := range c.ids {
		ls := c.Status(id)
		if !c.nodes[id].up || ls.State != raft.StateLeader {
			continue
		}
		followers := 0
		for _, other := range c.ids {
			s := c.Status(other)
			if c.nodes[other].up && c.connected(id, other) && s.Term == ls.Term && s.Lead == id {
				followers++
			}
		}
		if followers >= len(c.ids)/2+1 {
			return id, true
		}
	}
	return raft.None, false
}
