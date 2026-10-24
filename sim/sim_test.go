package sim

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/pBariskan/Raft/raft"
)

var replaySeed = flag.Uint64("sim.seed", 0, "run the chaos test for this one seed only")

const settle = 300 // ticks within which a healthy majority must elect a leader

func mustStableLeader(t *testing.T, c *Cluster, what string) raft.NodeID {
	t.Helper()
	if !c.RunUntil(func() bool { _, ok := c.StableLeader(); return ok }, settle) {
		t.Fatalf("seed %d: no stable leader %s within %d ticks; statuses %v", c.Seed(), what, settle, c.Statuses())
	}
	id, _ := c.StableLeader()
	return id
}

func checkSafe(t *testing.T, c *Cluster) {
	t.Helper()
	if err := c.Err(); err != nil {
		t.Fatalf("%v\nreplay: go test ./sim -run %s -sim.seed=%d", err, t.Name(), c.Seed())
	}
}

func TestOneLeaderEmerges(t *testing.T) {
	for seed := uint64(1); seed <= 100; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		id := mustStableLeader(t, c, "at start")
		for _, s := range c.Statuses() {
			if s.ID != id && s.State == raft.StateLeader {
				t.Fatalf("seed %d: %d is also leader: %v", seed, s.ID, c.Statuses())
			}
		}
		checkSafe(t, c)
	}
}

func TestReelectionAfterLeaderCrash(t *testing.T) {
	for seed := uint64(1); seed <= 100; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		old := mustStableLeader(t, c, "at start")
		oldTerm := c.Status(old).Term
		c.Crash(old)
		id := mustStableLeader(t, c, "after the leader crashed")
		if id == old || c.Status(id).Term <= oldTerm {
			t.Fatalf("seed %d: leader %d term %d after crash of %d (term %d)", seed, id, c.Status(id).Term, old, oldTerm)
		}
		// The old leader comes back still believing it leads; it must step down.
		c.Restart(old)
		c.Run(settle)
		if s := c.Status(old); s.State != raft.StateFollower || s.Lead != id {
			t.Fatalf("seed %d: restarted old leader is %+v, want follower of %d", seed, s, id)
		}
		checkSafe(t, c)
	}
}

func TestMajorityNeededToElect(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		l := mustStableLeader(t, c, "at start")
		// Two of five down (including the leader): three remain, a majority.
		c.Crash(l)
		c.Crash(l%5 + 1)
		l2 := mustStableLeader(t, c, "with two nodes down")
		// A third crash leaves two nodes: they must never elect anyone.
		c.Crash(l2)
		c.Run(5 * settle)
		for _, s := range c.Statuses() {
			if c.IsUp(s.ID) && s.State == raft.StateLeader {
				t.Fatalf("seed %d: %d became leader with only two nodes up", seed, s.ID)
			}
		}
		checkSafe(t, c)
	}
}

func TestMinorityPartitionCannotElect(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		old := mustStableLeader(t, c, "at start")
		oldTerm := c.Status(old).Term
		buddy := old%5 + 1
		var rest []raft.NodeID
		for id := raft.NodeID(1); id <= 5; id++ {
			if id != old && id != buddy {
				rest = append(rest, id)
			}
		}
		c.Partition([]raft.NodeID{old, buddy}, rest)
		id := mustStableLeader(t, c, "on the majority side")
		if id == old || id == buddy {
			t.Fatalf("seed %d: minority node %d is the stable leader", seed, id)
		}
		c.Run(settle)
		if s := c.Status(buddy); s.State == raft.StateLeader {
			t.Fatalf("seed %d: minority node %d became leader", seed, buddy)
		}
		if s := c.Status(old); s.State == raft.StateLeader && s.Term != oldTerm {
			t.Fatalf("seed %d: old leader won a new term inside the minority", seed)
		}
		c.Heal()
		mustStableLeader(t, c, "after healing")
		c.Run(settle)
		leaders := 0
		for _, s := range c.Statuses() {
			if s.State == raft.StateLeader {
				leaders++
			}
		}
		if leaders != 1 {
			t.Fatalf("seed %d: %d leaders after healing: %v", seed, leaders, c.Statuses())
		}
		checkSafe(t, c)
	}
}

func TestSameSeedSameRun(t *testing.T) {
	run := func(seed uint64) []string {
		c := New(Config{Seed: seed, Nodes: 5, DropRate: 0.1})
		var trace []string
		for i := 0; i < 1500; i++ {
			switch i {
			case 300:
				c.Crash(2)
			case 600:
				c.Partition([]raft.NodeID{1, 3}, []raft.NodeID{2, 4, 5})
			case 900:
				c.Restart(2)
				c.Heal()
			}
			c.Tick()
			trace = append(trace, fmt.Sprint(c.Statuses()))
		}
		return trace
	}
	a, b := run(7), run(7)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seed 7 diverged at tick %d:\n%s\n%s", i, a[i], b[i])
		}
	}
	other := run(8)
	same := true
	for i := range a {
		if a[i] != other[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("seeds 7 and 8 produced identical runs; the seed is not used")
	}
}

// TestChaosElectionSafety throws random crashes, restarts, partitions and message loss at the
// cluster and checks after every step that no term ever has two leaders. At the end it heals
// everything and checks that a leader is elected again.
func TestChaosElectionSafety(t *testing.T) {
	seeds := make([]uint64, 0, 300)
	if *replaySeed != 0 {
		seeds = append(seeds, *replaySeed)
	} else {
		n := uint64(300)
		if testing.Short() {
			n = 30
		}
		for s := uint64(1); s <= n; s++ {
			seeds = append(seeds, s)
		}
	}
	for _, seed := range seeds {
		c := New(Config{Seed: seed, Nodes: 5})
		chaos := rand.New(rand.NewPCG(seed, 0xc4a05))
		ids := []raft.NodeID{1, 2, 3, 4, 5}
		for i := 0; i < 3000 && c.Err() == nil; i++ {
			switch r := chaos.IntN(1000); {
			case r < 4:
				c.Crash(ids[chaos.IntN(5)])
			case r < 10:
				c.Restart(ids[chaos.IntN(5)])
			case r < 13:
				perm := chaos.Perm(5)
				cut := 1 + chaos.IntN(4)
				var a, b []raft.NodeID
				for k, p := range perm {
					if k < cut {
						a = append(a, ids[p])
					} else {
						b = append(b, ids[p])
					}
				}
				c.Partition(a, b)
			case r < 18:
				c.Heal()
			case r < 21:
				c.SetDropRate(float64(chaos.IntN(4)) / 10)
			}
			c.Tick()
		}
		checkSafe(t, c)
		for _, id := range ids {
			c.Restart(id)
		}
		c.Heal()
		c.SetDropRate(0)
		if !c.RunUntil(func() bool { _, ok := c.StableLeader(); return ok }, settle) {
			t.Fatalf("seed %d: no leader after healing; statuses %v\nreplay: go test ./sim -run %s -sim.seed=%d", seed, c.Statuses(), t.Name(), seed)
		}
		checkSafe(t, c)
	}
}
