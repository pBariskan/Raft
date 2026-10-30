package sim

import (
	"testing"
	"time"

	"github.com/pBariskan/Raft/check"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
)

var keys = []string{"a", "b", "c"}

func checkLinearizable(t *testing.T, c *Cluster) {
	t.Helper()
	if v := check.History(c.History(), 10*time.Second); v != check.Linearizable {
		t.Fatalf("seed %d: client history is %v\nreplay: go test ./sim -run %s -sim.seed=%d", c.Seed(), v, t.Name(), c.Seed())
	}
}

func TestClientsGetAnswersFromHealthyCluster(t *testing.T) {
	for seed := uint64(1); seed <= 30; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		w := NewWorkload(c, seed, 3, keys, 3)
		for i := 0; i < 1000; i++ {
			w.Step()
			c.Tick()
		}
		if !c.RunUntil(w.Idle, settle) {
			t.Fatalf("seed %d: clients still waiting on a healthy cluster", seed)
		}
		done := 0
		for _, op := range c.History() {
			if op.Done {
				done++
			}
		}
		if done < 100 {
			t.Fatalf("seed %d: only %d requests answered in 1000 ticks", seed, done)
		}
		checkLinearizable(t, c)
		checkSafe(t, c)
	}
}

func TestClientFollowsLeaderHint(t *testing.T) {
	c := New(Config{Seed: 3, Nodes: 5})
	l := mustStableLeader(t, c, "at start")
	id := c.NewClient()
	// Point the client at a follower first.
	c.clients[id-1].target = l%5 + 1
	if err := c.Submit(id, kv.OpPut, "x", "1"); err != nil {
		t.Fatal(err)
	}
	if !c.RunUntil(func() bool { return !c.ClientBusy(id) }, 20) {
		t.Fatal("put not answered quickly despite a known leader")
	}
	if got := c.clients[id-1]; got.target != l || got.retries != 0 {
		t.Fatalf("client ended at %d after %d timeouts, want leader %d with no timeouts", got.target, got.retries, l)
	}
}

func TestRetriedWriteIsAppliedOnce(t *testing.T) {
	// Lose every answer for a while: the client keeps retrying a PUT that did get committed.
	c := New(Config{Seed: 5, Nodes: 5})
	l := mustStableLeader(t, c, "at start")
	id := c.NewClient()
	c.clients[id-1].target = l
	other := c.NewClient()
	c.clients[other-1].target = l
	c.Submit(id, kv.OpPut, "x", "first")
	c.Run(3) // the request reaches the leader and is proposed
	c.Crash(l)
	c.Run(2)
	c.Restart(l)
	if !c.RunUntil(func() bool { return !c.ClientBusy(id) }, 2*settle) {
		t.Fatal("client never got an answer")
	}
	c.Submit(other, kv.OpPut, "x", "second")
	c.RunUntil(func() bool { return !c.ClientBusy(other) }, settle)
	c.Run(settle)
	for _, n := range []raft.NodeID{1, 2, 3, 4, 5} {
		if v, _ := c.Store(n).Get("x"); v != "second" {
			t.Fatalf("node %d: x = %q, want second (a late duplicate of the first PUT must not win)", n, v)
		}
	}
	checkLinearizable(t, c)
}

func TestClientsStayLinearizableThroughFailures(t *testing.T) {
	dups := 0
	for seed := uint64(1); seed <= 40; seed++ {
		c := New(Config{Seed: seed, Nodes: 5, DropRate: 0.15})
		w := NewWorkload(c, seed, 4, keys, 2)
		for i := 0; i < 2000; i++ {
			switch {
			case i%300 == 100:
				if l, ok := c.StableLeader(); ok {
					c.Crash(l)
				}
			case i%300 == 150:
				for _, id := range []raft.NodeID{1, 2, 3, 4, 5} {
					c.Restart(id)
				}
			}
			w.Step()
			c.Tick()
		}
		for _, id := range []raft.NodeID{1, 2, 3, 4, 5} {
			c.Restart(id)
		}
		c.SetDropRate(0)
		if !c.RunUntil(w.Idle, 3*settle) {
			t.Fatalf("seed %d: clients still waiting after the cluster healed", seed)
		}
		checkLinearizable(t, c)
		checkSafe(t, c)
		dups += c.Duplicates()
	}
	if dups == 0 {
		t.Fatal("no request was ever committed twice: the duplicate-detection path was never exercised")
	}
	t.Logf("duplicate commands skipped: %d", dups)
}
