// Package play is the playground's engine room: one simulated cluster that a viewer drives with
// small commands (step, crash, partition, send a request, …) and reads back as a snapshot of
// everything visible. The browser reaches it through the WebAssembly bridge in wasm/; it is plain
// Go so it can be tested like the rest of the project.
package play

import (
	"fmt"
	"time"

	"github.com/pBariskan/Raft/check"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/sim"
)

// Options configure a new session. Zero values get the simulator's defaults.
type Options struct {
	Seed          uint64   `json:"seed"`
	Nodes         int      `json:"nodes"`
	ElectionTick  int      `json:"election_tick"`
	HeartbeatTick int      `json:"heartbeat_tick"`
	MinDelay      int      `json:"min_delay"`
	MaxDelay      int      `json:"max_delay"`
	DropRate      float64  `json:"drop_rate"`
	Mistakes      []string `json:"mistakes"` // names from sim.MistakeSwitches

	// Replay runs the chaos runner's scenario for Seed (its faults and clients) instead of a
	// quiet cluster, so a failing seed from the chaos runner can be watched step by step.
	// Nodes, delays and drop rate then come from the scenario.
	Replay bool `json:"replay"`
}

// Session is one playground cluster.
type Session struct {
	opt     Options
	c       *sim.Cluster
	traffic *sim.Workload
	auto    bool
	chaos   *sim.Chaos // set in replay mode
	result  *sim.ChaosResult
}

// New starts a session.
func New(opt Options) (*Session, error) {
	if opt.Nodes == 0 {
		opt.Nodes = 5
	}
	if opt.Nodes < 1 || opt.Nodes > 9 {
		return nil, fmt.Errorf("play: %d nodes; pick 1 to 9", opt.Nodes)
	}
	var m sim.Mistakes
	for _, name := range opt.Mistakes {
		on, ok := sim.MistakeSwitches[name]
		if !ok {
			return nil, fmt.Errorf("play: unknown mistake %q", name)
		}
		on(&m)
	}
	s := &Session{opt: opt}
	if opt.Replay {
		s.chaos = sim.NewChaos(sim.ChaosConfig{Seed: opt.Seed, Mistakes: m})
		s.c = s.chaos.Cluster()
		s.opt.Nodes = len(s.c.IDs())
	} else {
		s.c = sim.New(sim.Config{
			Seed: opt.Seed, Nodes: opt.Nodes,
			ElectionTick: opt.ElectionTick, HeartbeatTick: opt.HeartbeatTick,
			MinDelay: opt.MinDelay, MaxDelay: opt.MaxDelay, DropRate: opt.DropRate,
			Mistakes: m,
		})
	}
	s.c.RecordEvents(true)
	return s, nil
}

// Cluster gives direct access to the simulation (for tests and scripted lessons).
func (s *Session) Cluster() *sim.Cluster { return s.c }

// Command is one thing a viewer does. Op picks the action; the other fields are its arguments.
type Command struct {
	Op string `json:"op"`

	Ticks  int             `json:"ticks,omitempty"`  // step, until
	Node   raft.NodeID     `json:"node,omitempty"`   // crash, restart, submit (first server to try)
	Groups []raft.NodeID   `json:"groups,omitempty"` // partition: the nodes cut off as one side
	Sides  [][]raft.NodeID `json:"sides,omitempty"`  // partition: several sides at once
	Rate   float64         `json:"rate,omitempty"`   // drop
	Min    int             `json:"min,omitempty"`    // delay
	Max    int             `json:"max,omitempty"`    // delay
	Client uint64          `json:"client,omitempty"` // submit
	KV     string          `json:"kv,omitempty"`     // submit: GET, PUT, DELETE, APPEND
	Key    string          `json:"key,omitempty"`    // submit
	Value  string          `json:"value,omitempty"`  // submit
	On     bool            `json:"on,omitempty"`     // traffic
	Until  string          `json:"until,omitempty"`  // until: "leader" or "idle"
}

// Result is what a command returns besides the new state.
type Result struct {
	Client  uint64 `json:"client,omitempty"`  // the new client's ID
	Held    bool   `json:"held,omitempty"`    // until: the condition was reached
	Ticks   int    `json:"ticks,omitempty"`   // until: ticks it took
	Verdict string `json:"verdict,omitempty"` // check: linearizable, not linearizable, unknown
	Ops     int    `json:"ops,omitempty"`     // check: requests in the history

	Chaos *sim.ChaosResult `json:"chaos,omitempty"` // finish: the replayed scenario's result
}

// Do runs one command.
func (s *Session) Do(cmd Command) (Result, error) {
	c := s.c
	node := func() error {
		if cmd.Node < 1 || int(cmd.Node) > s.opt.Nodes {
			return fmt.Errorf("play: no node %d", cmd.Node)
		}
		return nil
	}
	switch cmd.Op {
	case "step":
		for i := 0; i < max(1, cmd.Ticks); i++ {
			s.tick()
		}
	case "until":
		cond := func() bool { _, ok := c.StableLeader(); return ok }
		if cmd.Until == "idle" {
			cond = s.idle
		} else if cmd.Until != "leader" {
			return Result{}, fmt.Errorf("play: until %q? use leader or idle", cmd.Until)
		}
		limit := cmd.Ticks
		if limit <= 0 {
			limit = 1000
		}
		for t := 0; ; t++ {
			if cond() {
				return Result{Held: true, Ticks: t}, nil
			}
			if t == limit {
				return Result{Ticks: t}, nil
			}
			s.tick()
		}
	case "crash":
		if err := node(); err != nil {
			return Result{}, err
		}
		c.Crash(cmd.Node)
	case "restart":
		if err := node(); err != nil {
			return Result{}, err
		}
		c.Restart(cmd.Node)
	case "partition":
		sides := cmd.Sides
		if len(cmd.Groups) > 0 {
			sides = append(sides, cmd.Groups)
		}
		if len(sides) == 0 {
			return Result{}, fmt.Errorf("play: partition needs groups or sides")
		}
		c.Partition(sides...)
	case "heal":
		c.Heal()
	case "drop":
		if cmd.Rate < 0 || cmd.Rate > 1 {
			return Result{}, fmt.Errorf("play: drop rate %v outside 0..1", cmd.Rate)
		}
		c.SetDropRate(cmd.Rate)
	case "delay":
		c.SetDelay(cmd.Min, cmd.Max)
	case "client":
		return Result{Client: c.NewClient()}, nil
	case "submit":
		op, err := kv.ParseOp(cmd.KV)
		if err != nil {
			return Result{}, err
		}
		if cmd.Client < 1 || int(cmd.Client) > len(c.Clients()) {
			return Result{}, fmt.Errorf("play: no client %d", cmd.Client)
		}
		if err := c.Submit(cmd.Client, op, cmd.Key, cmd.Value); err != nil {
			return Result{}, err
		}
	case "traffic":
		if cmd.On && s.traffic == nil {
			s.traffic = sim.NewWorkload(c, s.opt.Seed+1, 3, []string{"x", "y"}, 6)
		}
		s.auto = cmd.On
	case "finish":
		if s.chaos == nil {
			return Result{}, fmt.Errorf("play: finish is for replays")
		}
		if s.result == nil {
			r := s.chaos.Finish()
			s.result = &r
		}
		return Result{Chaos: s.result}, nil
	case "check":
		h := c.History()
		v := check.History(h, 5*time.Second)
		return Result{Verdict: v.String(), Ops: len(h)}, nil
	default:
		return Result{}, fmt.Errorf("play: unknown command %q", cmd.Op)
	}
	return Result{}, nil
}

func (s *Session) tick() {
	if s.chaos != nil && s.result == nil && s.chaos.Step() {
		return
	}
	if s.auto {
		s.traffic.Step()
	}
	s.c.Tick()
}

func (s *Session) idle() bool {
	for _, cl := range s.c.Clients() {
		if cl.Busy {
			return false
		}
	}
	return true
}

// Snapshot is everything a viewer can see at one moment.
type Snapshot struct {
	Tick      int              `json:"tick"`
	Seed      uint64           `json:"seed"`
	Nodes     []Node           `json:"nodes"`
	Flights   []sim.Flight     `json:"flights"`
	Clients   []sim.ClientView `json:"clients"`
	Events    []sim.Event      `json:"events"`
	Leader    raft.NodeID      `json:"leader"` // a leader a majority follows, or 0
	Violation string           `json:"violation,omitempty"`
	Broken    string           `json:"broken,omitempty"` // the safety property that failed
	MinDelay  int              `json:"min_delay"`
	MaxDelay  int              `json:"max_delay"`
	DropRate  float64          `json:"drop_rate"`
	Traffic   bool             `json:"traffic"`
	Mistakes  []string         `json:"mistakes"`
	Replay    *Replay          `json:"replay,omitempty"`
}

// Replay describes a chaos scenario being replayed.
type Replay struct {
	Profile   string           `json:"profile"`
	Remaining int              `json:"remaining"` // chaos ticks left before the healing phase
	Result    *sim.ChaosResult `json:"result,omitempty"`
}

// Node is one server as a viewer sees it.
type Node struct {
	ID              raft.NodeID       `json:"id"`
	Up              bool              `json:"up"`
	State           string            `json:"state"` // follower, candidate, leader (last known, if down)
	Term            uint64            `json:"term"`
	Vote            raft.NodeID       `json:"vote"`
	Lead            raft.NodeID       `json:"lead"`
	Commit          uint64            `json:"commit"`
	LastIndex       uint64            `json:"last_index"`
	Applied         uint64            `json:"applied"`
	ElectionElapsed int               `json:"election_elapsed"`
	ElectionTimeout int               `json:"election_timeout"`
	Group           int               `json:"group"`     // partition side; 0 when whole
	LogStart        uint64            `json:"log_start"` // index of Log[0]
	Log             []Entry           `json:"log"`
	Store           map[string]string `json:"store"`
}

// Entry is one log entry as a viewer sees it.
type Entry struct {
	Index   uint64 `json:"index"`
	Term    uint64 `json:"term"`
	Command string `json:"command"`
}

// Snapshot returns the current state and the events since the last snapshot. Each node's log is
// cut to its last logTail entries (0 means all).
func (s *Session) Snapshot(logTail int) Snapshot {
	snap := s.Peek(logTail)
	if ev := s.c.Events(); ev != nil {
		snap.Events = ev
	}
	return snap
}

// Peek is Snapshot without taking the pending events, for a script that needs to look at the
// cluster without stealing the feed's events.
func (s *Session) Peek(logTail int) Snapshot {
	c := s.c
	snap := Snapshot{
		Tick: c.Now(), Seed: c.Seed(),
		Flights: c.Inflight(), Clients: c.Clients(), Events: []sim.Event{},
		DropRate: c.DropRate(), Traffic: s.auto, Mistakes: append([]string{}, s.opt.Mistakes...),
	}
	snap.MinDelay, snap.MaxDelay = c.Delay()
	if s.chaos != nil {
		snap.Replay = &Replay{Profile: s.chaos.Profile().Name, Remaining: s.chaos.Remaining(), Result: s.result}
	}
	if id, ok := c.StableLeader(); ok {
		snap.Leader = id
	}
	if err := c.Err(); err != nil {
		snap.Violation, snap.Broken = err.Error(), c.ErrKind()
	}
	for _, id := range c.IDs() {
		st := c.Status(id)
		n := Node{
			ID: id, Up: c.IsUp(id), State: st.State.String(), Term: st.Term, Vote: st.Vote,
			Lead: st.Lead, Commit: st.Commit, LastIndex: st.LastIndex,
			ElectionElapsed: st.ElectionElapsed, ElectionTimeout: st.ElectionTimeout,
			Group: c.Group(id), Store: map[string]string{},
		}
		store := c.Store(id)
		n.Applied = store.Applied()
		for _, k := range store.Keys() {
			n.Store[k], _ = store.Get(k)
		}
		log := c.Log(id)
		if logTail > 0 && len(log) > logTail {
			log = log[len(log)-logTail:]
		}
		n.LogStart = 1
		if len(log) > 0 {
			n.LogStart = log[0].Index
		}
		n.Log = make([]Entry, 0, len(log))
		for _, e := range log {
			n.Log = append(n.Log, Entry{Index: e.Index, Term: e.Term, Command: sim.DescribeEntry(e)})
		}
		snap.Nodes = append(snap.Nodes, n)
	}
	return snap
}
