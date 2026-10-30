package sim

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/pBariskan/Raft/check"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
)

// Faults says how often each kind of trouble happens. Rates are chances per 1000 ticks of the
// event happening on a given tick.
type Faults struct {
	Name       string
	Crash      int // crash a random server
	Restart    int // reboot a random crashed server
	Partition  int // split the servers into two random groups
	Heal       int // reconnect everyone
	DropChange int // pick a new message-loss rate between 0 and MaxDrop tenths
	MaxDrop    int
	MaxDelay   int // slowest message delay in ticks (default 3; the election timeout is 10–19)
}

// Profiles are the fault mixes the chaos runner rotates through, one per seed.
var Profiles = []Faults{
	{Name: "steady", Crash: 4, Restart: 6, Partition: 3, Heal: 5, DropChange: 3, MaxDrop: 3},
	{Name: "churn", Crash: 30, Restart: 60, Partition: 15, Heal: 20, DropChange: 5, MaxDrop: 2},
	{Name: "flaky network", Crash: 2, Restart: 10, Partition: 40, Heal: 40, DropChange: 20, MaxDrop: 5},
	// A slow network makes elections overlap (split votes), and quick reboots land in the
	// middle of them: the conditions for a double vote.
	{Name: "slow storm", Crash: 40, Restart: 120, Partition: 10, Heal: 30, DropChange: 10, MaxDrop: 3, MaxDelay: 9},
	// Servers crash and come straight back all the time, mostly in the middle of elections.
	{Name: "reboot storm", Crash: 150, Restart: 500, Partition: 5, Heal: 50, DropChange: 10, MaxDrop: 2, MaxDelay: 6},
}

// ChaosConfig describes one chaos run.
type ChaosConfig struct {
	Seed     uint64
	Ticks    int    // ticks of chaos before healing (default 3000)
	Faults   Faults // default: Profiles[Seed % len(Profiles)]
	Mistakes Mistakes
	Nodes    int // default 5
}

// ChaosResult is what one chaos run did and found.
type ChaosResult struct {
	Seed         uint64 `json:"seed"`
	Profile      string `json:"profile"`
	Ticks        int    `json:"ticks"`
	Crashes      int    `json:"crashes"`
	Partitions   int    `json:"partitions"`
	Leaders      int    `json:"leaders"`  // terms that had a leader
	Requests     int    `json:"requests"` // client requests sent
	Answered     int    `json:"answered"`
	Committed    int    `json:"committed"`  // log entries committed
	Duplicates   int    `json:"duplicates"` // retried commands skipped by duplicate detection
	Linearizable string `json:"linearizable"`

	Violation     string `json:"violation,omitempty"` // which property broke
	Detail        string `json:"detail,omitempty"`
	ViolationTick int    `json:"violation_tick,omitempty"`
}

// Failed reports whether the run found a violation.
func (r ChaosResult) Failed() bool { return r.Violation != "" }

// RunChaos runs one seeded chaos scenario: random faults while clients send requests, then a
// healing phase. Every safety property is checked on every step; at the end the client history
// is checked for linearizability and the cluster must have recovered.
func RunChaos(cfg ChaosConfig) ChaosResult {
	if cfg.Ticks == 0 {
		cfg.Ticks = 3000
	}
	if cfg.Faults.Name == "" {
		cfg.Faults = Profiles[cfg.Seed%uint64(len(Profiles))]
	}
	c := New(Config{Seed: cfg.Seed, Nodes: cfg.Nodes, Mistakes: cfg.Mistakes, MaxDelay: cfg.Faults.MaxDelay})
	ids := c.ids
	f := cfg.Faults
	rng := rand.New(rand.NewPCG(cfg.Seed, 0xc4a05))
	// Several clients hammering two keys: duplicates and stale reads only show up when other
	// requests touch the same key in between.
	w := NewWorkload(c, cfg.Seed, 5, []string{"a", "b"}, 2)
	res := ChaosResult{Seed: cfg.Seed, Profile: f.Name}

	chance := func(perMille int) bool { return rng.IntN(1000) < perMille }
	for i := 0; i < cfg.Ticks && c.Err() == nil; i++ {
		if chance(f.Crash) {
			id := ids[rng.IntN(len(ids))]
			if c.IsUp(id) {
				c.Crash(id)
				res.Crashes++
			}
		}
		if chance(f.Restart) {
			c.Restart(ids[rng.IntN(len(ids))])
		}
		if chance(f.Partition) {
			perm := rng.Perm(len(ids))
			cut := 1 + rng.IntN(len(ids)-1)
			var a, b []raft.NodeID
			for k, p := range perm {
				if k < cut {
					a = append(a, ids[p])
				} else {
					b = append(b, ids[p])
				}
			}
			c.Partition(a, b)
			res.Partitions++
		}
		if chance(f.Heal) {
			c.Heal()
		}
		if chance(f.DropChange) {
			c.SetDropRate(float64(rng.IntN(f.MaxDrop+1)) / 10)
		}
		w.Step()
		c.Tick()
	}

	// Heal everything and check the cluster recovers: every client gets its answer, and a final
	// write reaches every server.
	if c.Err() == nil {
		for _, id := range ids {
			c.Restart(id)
		}
		c.Heal()
		c.SetDropRate(0)
		healthy := c.RunUntil(w.Idle, 1000)
		final := c.NewClient()
		healthy = healthy && c.Submit(final, kv.OpPut, "final", "done") == nil &&
			c.RunUntil(func() bool { return !c.ClientBusy(final) }, 1000) &&
			c.RunUntil(func() bool {
				for _, id := range ids {
					if v, ok := c.Store(id).Get("final"); !ok || v != "done" {
						return false
					}
				}
				return true
			}, 1000)
		c.CheckLogs()
		if c.Err() == nil && !healthy {
			c.fail("Liveness", "the cluster did not recover after every fault was removed")
		}
	}

	res.Ticks = c.Now()
	res.Leaders = len(c.leaderOf)
	res.Committed = len(c.committed)
	res.Duplicates = c.Duplicates()
	for _, op := range c.History() {
		res.Requests++
		if op.Done {
			res.Answered++
		}
	}
	if c.Err() == nil {
		v := check.History(c.History(), 20*time.Second)
		res.Linearizable = v.String()
		if v == check.NotLinearizable {
			c.fail("Linearizability", "no single order of operations explains what the clients saw")
		}
	}
	if c.Err() != nil {
		res.Violation, res.Detail = c.ErrKind(), c.Err().Error()
		res.ViolationTick = c.errTick
	}
	return res
}

// String summarises a result in one line.
func (r ChaosResult) String() string {
	if r.Failed() {
		return fmt.Sprintf("seed %d (%s): %s violated at tick %d: %s", r.Seed, r.Profile, r.Violation, r.ViolationTick, r.Detail)
	}
	return fmt.Sprintf("seed %d (%s): ok, %d ticks, %d crashes, %d partitions, %d leaders, %d/%d requests answered, %s",
		r.Seed, r.Profile, r.Ticks, r.Crashes, r.Partitions, r.Leaders, r.Answered, r.Requests, r.Linearizable)
}
