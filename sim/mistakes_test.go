package sim

import (
	"testing"

	"github.com/pBariskan/Raft/raft"
)

// Each classic mistake must be caught by a checker. The seeds are the first ones the chaos
// runner found (go run ./cmd/chaos -mistake NAME -seeds 6000). If a change to the simulator
// shifts the random sequences, find new seeds the same way.
func TestClassicMistakesAreCaught(t *testing.T) {
	cases := []struct {
		name     string
		mistakes Mistakes
		seed     uint64
		want     []string // properties allowed to report it
	}{
		{"commit-old-terms", Mistakes{Mistakes: raft.Mistakes{CommitOldTerms: true}}, 906, []string{"Leader Completeness", "State Machine Safety"}},
		{"vote-ignores-log", Mistakes{Mistakes: raft.Mistakes{VoteIgnoresLog: true}}, 1, []string{"Leader Completeness", "State Machine Safety"}},
		{"accept-stale-leader", Mistakes{Mistakes: raft.Mistakes{AcceptStaleLeader: true}}, 1, []string{"Internal consistency", "Leader Completeness", "State Machine Safety"}},
		{"forget-vote", Mistakes{ForgetVote: true}, 189, []string{"Election Safety"}},
		{"local-reads", Mistakes{LocalReads: true}, 52, []string{"Linearizability"}},
		{"no-dedupe", Mistakes{NoDedupe: true}, 4, []string{"Linearizability"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := RunChaos(ChaosConfig{Seed: c.seed, Mistakes: c.mistakes})
			if !r.Failed() {
				t.Fatalf("seed %d with %s switched on passed every check", c.seed, c.name)
			}
			for _, w := range c.want {
				if r.Violation == w {
					return
				}
			}
			t.Fatalf("caught as %q, expected one of %v: %s", r.Violation, c.want, r)
		})
		// The same seed with the mistake off must pass, so it is the mistake that was caught.
		if r := RunChaos(ChaosConfig{Seed: c.seed}); r.Failed() {
			t.Fatalf("seed %d fails even without the mistake: %s", c.seed, r)
		}
	}
}
