package sim

import (
	"testing"

	"github.com/pBariskan/Raft/raft"
)

func TestRestartedNodeRemembersTermAndVote(t *testing.T) {
	for seed := uint64(1); seed <= 30; seed++ {
		c := New(Config{Seed: seed, Nodes: 5})
		l := mustStableLeader(t, c, "at start")
		f := l%5 + 1
		before := c.Status(f)
		c.Crash(f)
		c.Restart(f)
		after := c.Status(f)
		if after.Term != before.Term || after.Vote != before.Vote || after.LastIndex != before.LastIndex {
			t.Fatalf("seed %d: before crash %+v, after restart %+v", seed, before, after)
		}
		if after.Lead != raft.None {
			t.Fatalf("seed %d: a freshly booted node already knows a leader: %+v", seed, after)
		}
		mustStableLeader(t, c, "after the restart")
		checkSafe(t, c)
	}
}

func TestWholeClusterRestartKeepsCommittedData(t *testing.T) {
	ids := []raft.NodeID{1, 2, 3, 4, 5}
	for seed := uint64(1); seed <= 30; seed++ {
		c := New(Config{Seed: seed, Nodes: 5, DropRate: 0.05})
		l := mustStableLeader(t, c, "at start")
		want := proposeN(t, c, l, "v", 15)
		if !c.RunUntil(func() bool { return len(values(c.Applied(l))) == len(want) }, settle) {
			t.Fatalf("seed %d: values not committed", seed)
		}
		for _, id := range ids {
			c.Crash(id)
		}
		c.Run(20)
		for _, id := range ids {
			c.Restart(id)
		}
		// Every node rebuilds from its own disk. The leader had committed everything, and each
		// follower replays what it knew was committed; after a new election all catch up.
		if !c.RunUntil(appliedEverywhere(c, ids, len(want)), settle) {
			t.Fatalf("seed %d: cluster did not recover all committed values after a full restart", seed)
		}
		for _, id := range ids {
			if got := values(c.Applied(id)); !equalStrings(got, want) {
				t.Fatalf("seed %d: node %d has %v after restart, want %v", seed, id, got, want)
			}
		}
		checkSafe(t, c)
	}
}
