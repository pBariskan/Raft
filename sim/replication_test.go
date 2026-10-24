package sim

import (
	"fmt"
	"testing"

	"github.com/pBariskan/Raft/raft"
)

// values returns the client commands in a list of committed entries, skipping no-ops.
func values(es []raft.Entry) []string {
	var out []string
	for _, e := range es {
		if e.Data != nil {
			out = append(out, string(e.Data))
		}
	}
	return out
}

func proposeN(t *testing.T, c *Cluster, leader raft.NodeID, prefix string, n int) []string {
	t.Helper()
	var want []string
	for i := 0; i < n; i++ {
		v := fmt.Sprintf("%s%d", prefix, i)
		if err := c.Propose(leader, []byte(v)); err != nil {
			t.Fatalf("seed %d: propose %s: %v", c.Seed(), v, err)
		}
		want = append(want, v)
		c.Tick()
	}
	return want
}

func appliedEverywhere(c *Cluster, ids []raft.NodeID, n int) func() bool {
	return func() bool {
		for _, id := range ids {
			if len(values(c.Applied(id))) < n {
				return false
			}
		}
		return true
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReplicationReachesEveryNode(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		l := mustStableLeader(t, c, "at start")
		want := proposeN(t, c, l, "v", 20)
		all := []raft.NodeID{1, 2, 3, 4, 5}
		if !c.RunUntil(appliedEverywhere(c, all, len(want)), settle) {
			t.Fatalf("seed %d: not every node committed all 20 values", seed)
		}
		for _, id := range all {
			if got := values(c.Applied(id)); !equalStrings(got, want) {
				t.Fatalf("seed %d: node %d committed %v, want %v", seed, id, got, want)
			}
		}
		c.CheckLogs()
		checkSafe(t, c)
	}
}

func TestCommitsWithTwoFollowersDownAndCatchesThemUp(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		c := New(Config{Seed: seed, Nodes: 5, DropRate: 0.05})
		l := mustStableLeader(t, c, "at start")
		down := []raft.NodeID{l%5 + 1, (l+1)%5 + 1}
		for _, id := range down {
			c.Crash(id)
		}
		want := proposeN(t, c, l, "v", 10)
		if !c.RunUntil(func() bool { return len(values(c.Applied(l))) == len(want) }, settle) {
			t.Fatalf("seed %d: leader could not commit with 3 of 5 up", seed)
		}
		for _, id := range down {
			c.Restart(id)
		}
		if !c.RunUntil(appliedEverywhere(c, down, len(want)), settle) {
			t.Fatalf("seed %d: restarted followers did not catch up", seed)
		}
		checkSafe(t, c)
	}
}

func TestNothingCommitsWithoutAMajority(t *testing.T) {
	for seed := uint64(1); seed <= 30; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		l := mustStableLeader(t, c, "at start")
		c.Run(settle) // let the no-op commit everywhere
		before := len(c.Applied(l))
		c.Crash(l%5 + 1)
		c.Crash((l+1)%5 + 1)
		c.Crash((l+2)%5 + 1)
		for i := 0; i < 5; i++ {
			if err := c.Propose(l, []byte(fmt.Sprint("lost", i))); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
		}
		c.Run(5 * settle)
		if got := len(c.Applied(l)); got != before {
			t.Fatalf("seed %d: leader committed %d new entries with only 2 of 5 up", seed, got-before)
		}
		checkSafe(t, c)
	}
}

func TestCommittedEntriesSurviveLeaderChanges(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		c := New(Config{Seed: seed, Nodes: 5, DropRate: 0.1})
		var want []string
		for round := 0; round < 4; round++ {
			l := mustStableLeader(t, c, fmt.Sprintf("in round %d", round))
			want = append(want, proposeN(t, c, l, fmt.Sprintf("r%d-", round), 5)...)
			if !c.RunUntil(func() bool { return len(values(c.Applied(l))) >= len(want) }, settle) {
				t.Fatalf("seed %d: round %d values not committed", seed, round)
			}
			c.Crash(l)
			c.Run(30)
			c.Restart(l)
		}
		all := []raft.NodeID{1, 2, 3, 4, 5}
		if !c.RunUntil(appliedEverywhere(c, all, len(want)), settle) {
			t.Fatalf("seed %d: not everyone caught up", seed)
		}
		for _, id := range all {
			if got := values(c.Applied(id)); !equalStrings(got, want) {
				t.Fatalf("seed %d: node %d committed %v, want %v", seed, id, got, want)
			}
		}
		checkSafe(t, c)
	}
}
