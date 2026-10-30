package sim

import (
	"fmt"
	"testing"

	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
)

// scripted runs the same busy scenario on a cluster: elections, client traffic, a crash, a
// partition and lossy links.
func scripted(c *Cluster) {
	cl := c.NewClient()
	for step := 0; step < 1200; step++ {
		switch step {
		case 200:
			c.Crash(1)
		case 300:
			c.Partition([]raft.NodeID{2, 3})
		case 500:
			c.Heal()
			c.Restart(1)
			c.SetDropRate(0.2)
		case 700:
			c.SetDropRate(0)
		}
		if !c.ClientBusy(cl) {
			c.Submit(cl, kv.OpAppend, "x", fmt.Sprint(step%10))
		}
		c.Tick()
	}
}

func TestRecordingDoesNotChangeTheRun(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		plain, watched := New(Config{Seed: seed}), New(Config{Seed: seed})
		watched.RecordEvents(true)
		scripted(plain)
		scripted(watched)
		watched.Events()
		if a, b := fmt.Sprint(plain.Statuses(), plain.History()), fmt.Sprint(watched.Statuses(), watched.History()); a != b {
			t.Fatalf("seed %d: recording events changed the run\n%s\n%s", seed, a, b)
		}
	}
}

func TestEventsTellTheStory(t *testing.T) {
	c := New(Config{Seed: 7})
	c.RecordEvents(true)
	scripted(c)
	count := map[string]int{}
	leaders := 0
	for _, e := range c.Events() {
		count[e.Kind]++
		if e.Kind == EvRole && e.Text == "leader" {
			leaders++
		}
		if (e.Kind == EvLost || e.Kind == EvDropped) && e.Flight == nil {
			t.Fatalf("%s event without its message: %+v", e.Kind, e)
		}
	}
	for _, k := range []string{EvRole, EvCommit, EvCrash, EvRestart, EvPartition, EvHeal, EvNetwork, EvCall, EvReturn, EvLost, EvDropped} {
		if count[k] == 0 {
			t.Errorf("no %q event; got %v", k, count)
		}
	}
	if leaders == 0 {
		t.Error("no node ever became leader in the events")
	}
	if len(c.Events()) != 0 {
		t.Error("Events should drain")
	}
}

func TestInflightShowsMessagesInTransit(t *testing.T) {
	c := New(Config{Seed: 3, MinDelay: 2, MaxDelay: 2})
	mustStableLeader(t, c, "at start")
	fl := c.Inflight()
	if len(fl) == 0 {
		t.Fatal("a leader is heartbeating, yet nothing is in flight")
	}
	for _, f := range fl {
		if f.DeliverAt-f.SentAt != 2 || f.Kind == "" || f.From == 0 || f.To == 0 {
			t.Fatalf("bad flight %+v", f)
		}
	}
}

func TestDescribeEntry(t *testing.T) {
	cmd := kv.Command{Client: 1, Seq: 1, Op: kv.OpAppend, Key: "x", Value: "ab"}
	if got := DescribeEntry(raft.Entry{Data: cmd.Encode()}); got != "APPEND x+=ab" {
		t.Fatalf("DescribeEntry = %q", got)
	}
	if got := DescribeEntry(raft.Entry{}); got != "no-op" {
		t.Fatalf("DescribeEntry(no-op) = %q", got)
	}
}
