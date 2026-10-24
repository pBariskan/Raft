package raft

import (
	"math/rand/v2"
	"testing"
)

// fixedRand always returns the same offset, so election timeouts are exact in tests.
type fixedRand int

func (r fixedRand) IntN(n int) int { return int(r) % n }

const testElection = 10

func newTestNode(t *testing.T, id NodeID, peers ...NodeID) *Node {
	t.Helper()
	n, err := New(Config{ID: id, Peers: peers, ElectionTick: testElection, HeartbeatTick: 1, Rand: fixedRand(0)})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var five = []NodeID{1, 2, 3, 4, 5}

func tickN(n *Node, k int) {
	for i := 0; i < k; i++ {
		n.Tick()
	}
}

func msgsOfType(ms []Message, typ MessageType) []Message {
	var out []Message
	for _, m := range ms {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

// makeCandidate runs node 1 out of patience, so it is a candidate in term 1.
func makeCandidate(t *testing.T) *Node {
	t.Helper()
	n := newTestNode(t, 1, five...)
	tickN(n, testElection)
	n.Ready()
	return n
}

func TestConfigValidation(t *testing.T) {
	bad := []Config{
		{ID: 0, Peers: five, ElectionTick: 10, HeartbeatTick: 1, Rand: fixedRand(0)},
		{ID: 9, Peers: five, ElectionTick: 10, HeartbeatTick: 1, Rand: fixedRand(0)},
		{ID: 1, Peers: five, ElectionTick: 1, HeartbeatTick: 1, Rand: fixedRand(0)},
		{ID: 1, Peers: five, ElectionTick: 10, HeartbeatTick: 0, Rand: fixedRand(0)},
		{ID: 1, Peers: five, ElectionTick: 10, HeartbeatTick: 1},
		{ID: 1, Peers: []NodeID{1, 2, 2}, ElectionTick: 10, HeartbeatTick: 1, Rand: fixedRand(0)},
	}
	for i, c := range bad {
		if _, err := New(c); err == nil {
			t.Errorf("config %d: expected an error", i)
		}
	}
}

func TestStartsAsFollowerInTermZero(t *testing.T) {
	s := newTestNode(t, 1, five...).Status()
	if s.State != StateFollower || s.Term != 0 || s.Vote != None || s.Lead != None {
		t.Fatalf("got %+v", s)
	}
}

func TestFollowerCampaignsAfterElectionTimeout(t *testing.T) {
	n := newTestNode(t, 1, five...)
	tickN(n, testElection-1)
	if n.Status().State != StateFollower {
		t.Fatal("campaigned before the timeout")
	}
	if len(n.Ready().Messages) != 0 {
		t.Fatal("follower sent messages before the timeout")
	}
	n.Tick()
	s := n.Status()
	if s.State != StateCandidate || s.Term != 1 || s.Vote != 1 {
		t.Fatalf("after timeout got %+v, want candidate, term 1, voted for itself", s)
	}
	votes := msgsOfType(n.Ready().Messages, MsgVote)
	if len(votes) != 4 {
		t.Fatalf("got %d vote requests, want 4", len(votes))
	}
	for _, m := range votes {
		if m.From != 1 || m.Term != 1 || m.To == 1 {
			t.Fatalf("bad vote request %+v", m)
		}
	}
}

func TestElectionTimeoutIsRandomizedInRange(t *testing.T) {
	seen := map[int]bool{}
	for seed := uint64(0); seed < 200; seed++ {
		n, _ := New(Config{ID: 1, Peers: five, ElectionTick: testElection, HeartbeatTick: 1, Rand: rand.New(rand.NewPCG(seed, 0))})
		ticks := 0
		for n.Status().State == StateFollower {
			n.Tick()
			ticks++
		}
		if ticks < testElection || ticks >= 2*testElection {
			t.Fatalf("seed %d: campaigned after %d ticks, want [%d, %d)", seed, ticks, testElection, 2*testElection)
		}
		seen[ticks] = true
	}
	if len(seen) < testElection/2 {
		t.Fatalf("timeouts not spread out: only %d distinct values", len(seen))
	}
}

func TestCandidateBecomesLeaderWithMajority(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	if n.Status().State != StateCandidate {
		t.Fatal("leader with only 2 of 5 votes")
	}
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1}) // duplicate vote must not count twice
	if n.Status().State != StateCandidate {
		t.Fatal("a duplicated vote was counted twice")
	}
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	s := n.Status()
	if s.State != StateLeader || s.Lead != 1 {
		t.Fatalf("got %+v, want leader with 3 of 5 votes", s)
	}
	hb := msgsOfType(n.Ready().Messages, MsgApp)
	if len(hb) != 4 {
		t.Fatalf("new leader sent %d heartbeats, want 4 right away", len(hb))
	}
}

func TestRejectionsDoNotCount(t *testing.T) {
	n := makeCandidate(t)
	for _, from := range []NodeID{2, 3, 4} {
		n.Step(Message{Type: MsgVoteResp, From: from, To: 1, Term: 1, Reject: true})
	}
	if n.Status().State == StateLeader {
		t.Fatal("became leader from rejections")
	}
}

func TestVoteFromOldTermIgnored(t *testing.T) {
	n := makeCandidate(t)
	tickN(n, testElection) // second election: term 2
	n.Ready()
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	if n.Status().State == StateLeader {
		t.Fatal("votes from term 1 elected a term-2 candidate")
	}
}

func TestOneVotePerTerm(t *testing.T) {
	n := newTestNode(t, 1, five...)
	n.Step(Message{Type: MsgVote, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVote, From: 3, To: 1, Term: 1})
	n.Step(Message{Type: MsgVote, From: 2, To: 1, Term: 1}) // retry from the same candidate
	resp := msgsOfType(n.Ready().Messages, MsgVoteResp)
	if len(resp) != 3 {
		t.Fatalf("got %d responses, want 3", len(resp))
	}
	want := []struct {
		to     NodeID
		reject bool
	}{{2, false}, {3, true}, {2, false}}
	for i, w := range want {
		if resp[i].To != w.to || resp[i].Reject != w.reject || resp[i].Term != 1 {
			t.Fatalf("response %d = %+v, want to=%d reject=%v term=1", i, resp[i], w.to, w.reject)
		}
	}
	if v := n.Status().Vote; v != 2 {
		t.Fatalf("vote = %d, want 2", v)
	}
}

func TestCandidateRejectsOtherCandidatesInSameTerm(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVote, From: 2, To: 1, Term: 1})
	resp := msgsOfType(n.Ready().Messages, MsgVoteResp)
	if len(resp) != 1 || !resp[0].Reject {
		t.Fatalf("candidate gave away its vote: %+v", resp)
	}
}

func TestStaleVoteRequestRejected(t *testing.T) {
	n := makeCandidate(t) // term 1
	tickN(n, testElection)
	n.Ready() // term 2
	n.Step(Message{Type: MsgVote, From: 3, To: 1, Term: 1})
	resp := msgsOfType(n.Ready().Messages, MsgVoteResp)
	if len(resp) != 1 || !resp[0].Reject || resp[0].Term != 2 {
		t.Fatalf("got %+v, want a rejection carrying term 2", resp)
	}
}

func TestHigherTermVoteRequestTurnsLeaderIntoFollowerAndGetsVote(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	n.Ready()
	// The leader's log holds its term-1 no-op, so the candidate's log must be at least that long.
	n.Step(Message{Type: MsgVote, From: 4, To: 1, Term: 5, Index: 1, LogTerm: 1})
	s := n.Status()
	if s.State != StateFollower || s.Term != 5 || s.Vote != 4 || s.Lead != None {
		t.Fatalf("got %+v, want follower in term 5 that voted for 4", s)
	}
}

func TestCandidateStepsDownOnHeartbeatFromSameTerm(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgApp, From: 3, To: 1, Term: 1})
	s := n.Status()
	if s.State != StateFollower || s.Term != 1 || s.Lead != 3 {
		t.Fatalf("got %+v, want follower of 3 in term 1", s)
	}
}

func TestStaleLeaderLearnsItWasReplaced(t *testing.T) {
	// A follower in term 3 answers an old leader's heartbeat with its own term...
	f := newTestNode(t, 2, five...)
	f.Step(Message{Type: MsgApp, From: 4, To: 2, Term: 3})
	f.Ready()
	f.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1})
	resp := msgsOfType(f.Ready().Messages, MsgAppResp)
	if len(resp) != 1 || resp[0].Term != 3 || !resp[0].Reject {
		t.Fatalf("got %+v, want a rejection carrying term 3", resp)
	}
	if f.Status().Lead != 4 {
		t.Fatal("follower switched to the stale leader")
	}
	// ...and the old leader steps down when it reads that reply.
	l := makeCandidate(t)
	l.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	l.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	l.Step(resp[0])
	if s := l.Status(); s.State != StateFollower || s.Term != 3 {
		t.Fatalf("old leader got %+v, want follower in term 3", s)
	}
}

func TestHeartbeatsKeepFollowerFromCampaigning(t *testing.T) {
	n := newTestNode(t, 2, five...)
	for i := 0; i < 10*testElection; i++ {
		n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1})
		n.Tick()
	}
	if s := n.Status(); s.State != StateFollower || s.Lead != 1 {
		t.Fatalf("got %+v, want a calm follower of 1", s)
	}
}

func TestGrantingAVoteResetsTheTimer(t *testing.T) {
	n := newTestNode(t, 2, five...)
	tickN(n, testElection-1)
	n.Step(Message{Type: MsgVote, From: 1, To: 2, Term: 1})
	tickN(n, testElection-1)
	if n.Status().State != StateFollower {
		t.Fatal("follower that just voted started its own election too soon")
	}
}

func TestLeaderSendsHeartbeatsAndNeverCampaigns(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	n.Ready()
	for i := 0; i < 5*testElection; i++ {
		n.Tick()
		if hb := msgsOfType(n.Ready().Messages, MsgApp); len(hb) != 4 {
			t.Fatalf("tick %d: %d heartbeats, want 4", i, len(hb))
		}
	}
	if s := n.Status(); s.State != StateLeader || s.Term != 1 {
		t.Fatalf("got %+v, want leader still in term 1", s)
	}
}

func TestSingleNodeElectsItself(t *testing.T) {
	n := newTestNode(t, 1, 1)
	tickN(n, testElection)
	if s := n.Status(); s.State != StateLeader || s.Term != 1 {
		t.Fatalf("got %+v, want leader in term 1", s)
	}
}

func TestMessagesFromStrangersIgnored(t *testing.T) {
	n := newTestNode(t, 1, five...)
	n.Step(Message{Type: MsgVote, From: 42, To: 1, Term: 7})
	if s := n.Status(); s.Term != 0 || len(n.Ready().Messages) != 0 {
		t.Fatalf("reacted to a non-member: %+v", s)
	}
}
