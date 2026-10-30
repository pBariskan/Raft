package sim

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/pBariskan/Raft/check"
	"github.com/pBariskan/Raft/kv"
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
	req       *request  // set for a client → server request instead of msg
	resp      *response // set for a server → client answer instead of msg
	deliverAt int
	seq       int // send order, breaks ties so delivery order is deterministic
}

type node struct {
	rn       *raft.Node
	up       bool
	applied  []raft.Entry // committed entries handed out since the last (re)start, in order
	disk     disk
	restarts uint64

	store   *kv.Store         // rebuilt from the log after every boot
	waiting map[reqKey]uint64 // client requests this node proposed, by the term it proposed in
}

// disk is a node's simulated stable storage. It survives crashes; everything else is lost.
type disk struct {
	hs  raft.HardState
	log []raft.Entry
}

func (d *disk) save(hs *raft.HardState, entries []raft.Entry) {
	if hs != nil {
		d.hs = *hs
	}
	if len(entries) > 0 {
		d.log = append(d.log[:entries[0].Index-1], entries...)
	}
}

// Cluster is a deterministic simulation of a Raft cluster: a virtual clock counted in ticks, a
// network that delays, drops and partitions messages, and nodes that crash and restart.
//
// A crash wipes a node's memory. Restart boots a fresh node from what it saved to its simulated
// disk, as a real server would.
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

	clients    []*client
	history    []check.Operation
	duplicates int

	leaderOf  map[uint64]raft.NodeID // every leader ever seen, per term
	committed []raft.Entry           // committed[i] is the entry first committed at index i+1
	err       error                  // first safety violation
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
		n := &node{}
		c.nodes[id] = n
		c.boot(id)
	}
	return c
}

// boot starts a node from its disk with an empty memory.
func (c *Cluster) boot(id raft.NodeID) {
	n := c.nodes[id]
	rn, err := raft.New(raft.Config{
		ID:            id,
		Peers:         c.ids,
		ElectionTick:  c.cfg.ElectionTick,
		HeartbeatTick: c.cfg.HeartbeatTick,
		Rand:          rand.New(rand.NewPCG(c.cfg.Seed, uint64(id)<<32|n.restarts)),
		HardState:     n.disk.hs,
		Entries:       n.disk.log,
	})
	if err != nil {
		panic(err) // the simulator wrote this disk itself; an error is a bug here
	}
	n.rn, n.up, n.applied = rn, true, nil
	n.store, n.waiting = kv.NewStore(), map[reqKey]uint64{}
	c.after(id) // a restored node hands its committed entries out again
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
		if e.req != nil || e.resp != nil {
			c.deliverClientTraffic(e)
			continue
		}
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
	c.tickClients()
	if c.now%100 == 0 {
		c.CheckLogs()
	}
}

// Propose hands a command to a node, as a client would. Only a running leader accepts it.
func (c *Cluster) Propose(id raft.NodeID, data []byte) error {
	if !c.nodes[id].up {
		return fmt.Errorf("sim: node %d is down", id)
	}
	if err := c.nodes[id].rn.Propose(data); err != nil {
		return err
	}
	c.after(id)
	return nil
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
	n := c.nodes[id]
	rd := n.rn.Ready()
	n.disk.save(rd.HardState, rd.Entries) // persist before anything is sent
	for _, m := range rd.Messages {
		c.send(m)
	}
	for _, e := range rd.CommittedEntries {
		c.checkCommitted(id, e)
		n.applied = append(n.applied, e)
		c.serverApplied(id, n.store.Apply(e))
	}
	c.checkElectionSafety(id)
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

func (c *Cluster) fail(format string, args ...any) {
	if c.err == nil {
		c.err = fmt.Errorf("tick %d (seed %d): "+format, append([]any{c.now, c.cfg.Seed}, args...)...)
	}
}

// checkElectionSafety enforces Election Safety (at most one leader per term, over the whole run)
// and Leader Completeness (a new leader holds every entry committed so far).
func (c *Cluster) checkElectionSafety(id raft.NodeID) {
	s := c.nodes[id].rn.Status()
	if s.State != raft.StateLeader {
		return
	}
	prev, seen := c.leaderOf[s.Term]
	if seen && prev != s.ID {
		c.fail("election safety violated: term %d has two leaders, %d and %d", s.Term, prev, s.ID)
		return
	}
	if seen {
		return
	}
	c.leaderOf[s.Term] = s.ID
	log := c.nodes[id].rn.Log()
	for _, e := range c.committed {
		if e.Index > uint64(len(log)) || !sameEntry(log[e.Index-1], e) {
			c.fail("leader completeness violated: node %d leads term %d without committed entry %d", id, s.Term, e.Index)
			return
		}
	}
}

// checkCommitted enforces State Machine Safety: every node commits the same entry at each index,
// and hands out committed entries in order with no gaps.
func (c *Cluster) checkCommitted(id raft.NodeID, e raft.Entry) {
	if want := uint64(len(c.nodes[id].applied)) + 1; e.Index != want {
		c.fail("node %d delivered committed entry %d, expected %d", id, e.Index, want)
		return
	}
	if e.Index <= uint64(len(c.committed)) {
		if first := c.committed[e.Index-1]; !sameEntry(first, e) {
			c.fail("state machine safety violated: node %d committed %s at index %d, but %s was committed there before",
				id, describe(e), e.Index, describe(first))
		}
		return
	}
	c.committed = append(c.committed, e)
}

// CheckLogs enforces Log Matching: if two logs hold an entry with the same index and term, the
// logs are identical up to that index. It runs every 100 ticks; call it at the end of a run too.
func (c *Cluster) CheckLogs() {
	logs := make([][]raft.Entry, len(c.ids))
	for i, id := range c.ids {
		logs[i] = c.nodes[id].rn.Log()
	}
	for i := range logs {
		for j := i + 1; j < len(logs); j++ {
			a, b := logs[i], logs[j]
			k := min(len(a), len(b))
			for k > 0 && a[k-1].Term != b[k-1].Term {
				k--
			}
			for x := 0; x < k; x++ {
				if !sameEntry(a[x], b[x]) {
					c.fail("log matching violated: nodes %d and %d agree at index %d (term %d) but differ at index %d",
						c.ids[i], c.ids[j], k, a[k-1].Term, x+1)
					return
				}
			}
		}
	}
}

func sameEntry(a, b raft.Entry) bool {
	return a.Index == b.Index && a.Term == b.Term && bytes.Equal(a.Data, b.Data)
}

func describe(e raft.Entry) string {
	if e.Data == nil {
		return fmt.Sprintf("(term %d, no-op)", e.Term)
	}
	return fmt.Sprintf("(term %d, %q)", e.Term, e.Data)
}

func (c *Cluster) connected(a, b raft.NodeID) bool {
	return c.group == nil || c.group[a] == c.group[b]
}

// Crash stops a node: it no longer ticks, messages sent to it are lost, and everything it had not
// saved to disk is gone.
func (c *Cluster) Crash(id raft.NodeID) { c.nodes[id].up = false }

// Restart boots a crashed node from its disk. Restarting a running node does nothing.
func (c *Cluster) Restart(id raft.NodeID) {
	n := c.nodes[id]
	if n.up {
		return
	}
	n.restarts++
	c.boot(id)
}

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

// Log returns a copy of one node's log.
func (c *Cluster) Log(id raft.NodeID) []raft.Entry { return c.nodes[id].rn.Log() }

// Applied returns the committed entries a node has handed out so far, in order.
func (c *Cluster) Applied(id raft.NodeID) []raft.Entry {
	return append([]raft.Entry(nil), c.nodes[id].applied...)
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
