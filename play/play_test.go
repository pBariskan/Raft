package play

import (
	"encoding/json"
	"testing"

	"github.com/pBariskan/Raft/raft"
)

func mustDo(t *testing.T, s *Session, cmd Command) Result {
	t.Helper()
	r, err := s.Do(cmd)
	if err != nil {
		t.Fatalf("%+v: %v", cmd, err)
	}
	return r
}

func TestAWholeStory(t *testing.T) {
	s, err := New(Options{Seed: 1, Nodes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r := mustDo(t, s, Command{Op: "until", Until: "leader"}); !r.Held {
		t.Fatal("no leader")
	}
	lead := s.Snapshot(0).Leader
	cl := mustDo(t, s, Command{Op: "client"}).Client
	mustDo(t, s, Command{Op: "submit", Client: cl, KV: "PUT", Key: "x", Value: "1"})
	if r := mustDo(t, s, Command{Op: "until", Until: "idle", Ticks: 500}); !r.Held {
		t.Fatal("PUT never answered")
	}
	mustDo(t, s, Command{Op: "crash", Node: lead})
	mustDo(t, s, Command{Op: "submit", Client: cl, KV: "APPEND", Key: "x", Value: "2"})
	if r := mustDo(t, s, Command{Op: "until", Until: "idle", Ticks: 1000}); !r.Held {
		t.Fatal("APPEND never answered after the leader crashed")
	}
	mustDo(t, s, Command{Op: "restart", Node: lead})
	mustDo(t, s, Command{Op: "step", Ticks: 100})

	snap := s.Snapshot(0)
	if snap.Violation != "" {
		t.Fatal(snap.Violation)
	}
	for _, n := range snap.Nodes {
		if n.Store["x"] != "12" {
			t.Errorf("node %d has x=%q, want 12", n.ID, n.Store["x"])
		}
	}
	if r := mustDo(t, s, Command{Op: "check"}); r.Verdict != "linearizable" || r.Ops != 2 {
		t.Errorf("check = %+v", r)
	}
	if len(snap.Events) == 0 {
		t.Error("no events")
	}
	if _, err := json.Marshal(snap); err != nil {
		t.Fatal(err)
	}
}

func TestPartitionAndTraffic(t *testing.T) {
	s, _ := New(Options{Seed: 4, Nodes: 5})
	mustDo(t, s, Command{Op: "until", Until: "leader"})
	mustDo(t, s, Command{Op: "traffic", On: true})
	mustDo(t, s, Command{Op: "partition", Groups: []raft.NodeID{1, 2}})
	snap := s.Snapshot(0)
	if snap.Nodes[0].Group == snap.Nodes[4].Group {
		t.Fatal("1 and 5 should be on different sides")
	}
	mustDo(t, s, Command{Op: "drop", Rate: 0.1})
	mustDo(t, s, Command{Op: "delay", Min: 1, Max: 6})
	mustDo(t, s, Command{Op: "step", Ticks: 400})
	mustDo(t, s, Command{Op: "heal"})
	mustDo(t, s, Command{Op: "drop", Rate: 0})
	mustDo(t, s, Command{Op: "traffic", On: false})
	if r := mustDo(t, s, Command{Op: "until", Until: "idle", Ticks: 2000}); !r.Held {
		t.Fatal("clients never finished after healing")
	}
	snap = s.Snapshot(10)
	if snap.Violation != "" {
		t.Fatal(snap.Violation)
	}
	for _, n := range snap.Nodes {
		if len(n.Log) > 10 || (len(n.Log) > 0 && n.Log[0].Index != n.LogStart) {
			t.Fatalf("log tail wrong: start %d, %d entries", n.LogStart, len(n.Log))
		}
	}
	if r := mustDo(t, s, Command{Op: "check"}); r.Verdict != "linearizable" || r.Ops == 0 {
		t.Errorf("check = %+v", r)
	}
}

func TestMistakesAreSwitchedOn(t *testing.T) {
	s, err := New(Options{Seed: 1, Mistakes: []string{"vote-ignores-log", "no-dedupe"}})
	if err != nil {
		t.Fatal(err)
	}
	m := s.Cluster().Mistakes()
	if !m.VoteIgnoresLog || !m.NoDedupe || m.ForgetVote {
		t.Fatalf("mistakes = %+v", m)
	}
	if got := s.Snapshot(0).Mistakes; len(got) != 2 {
		t.Fatalf("snapshot mistakes = %v", got)
	}
}

func TestBadCommands(t *testing.T) {
	if _, err := New(Options{Mistakes: []string{"nope"}}); err == nil {
		t.Error("unknown mistake accepted")
	}
	if _, err := New(Options{Nodes: 12}); err == nil {
		t.Error("12 nodes accepted")
	}
	s, _ := New(Options{Seed: 1})
	for _, cmd := range []Command{
		{Op: "fly"}, {Op: "crash", Node: 9}, {Op: "submit", Client: 1, KV: "GET"},
		{Op: "submit", KV: "INCR"}, {Op: "drop", Rate: 2}, {Op: "partition"}, {Op: "until", Until: "never"},
	} {
		if _, err := s.Do(cmd); err == nil {
			t.Errorf("%+v accepted", cmd)
		}
	}
}
