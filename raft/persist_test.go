package raft

import (
	"reflect"
	"testing"
)

func TestReadyReportsHardStateOnlyWhenChanged(t *testing.T) {
	n := newTestNode(t, 1, five...)
	if rd := n.Ready(); rd.HardState != nil {
		t.Fatalf("fresh node reported hard state %+v", rd.HardState)
	}
	tickN(n, testElection)
	rd := n.Ready()
	if rd.HardState == nil || *rd.HardState != (HardState{Term: 1, Vote: 1}) {
		t.Fatalf("after campaigning got %+v, want term 1, vote 1", rd.HardState)
	}
	if rd := n.Ready(); rd.HardState != nil {
		t.Fatal("unchanged hard state reported twice")
	}
}

func TestReadyReportsNewEntriesToPersist(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	if got := n.Ready().Entries; !reflect.DeepEqual(got, []Entry{ent(1, 1, "")}) {
		t.Fatalf("new leader asked to persist %+v, want its no-op", got)
	}
	if err := n.Propose([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if got := n.Ready().Entries; !reflect.DeepEqual(got, []Entry{ent(1, 2, "x")}) {
		t.Fatalf("after Propose asked to persist %+v, want just x", got)
	}
	if got := n.Ready().Entries; len(got) != 0 {
		t.Fatalf("entries reported twice: %+v", got)
	}
}

func TestReadyEntriesStartAtTheFirstReplacedIndex(t *testing.T) {
	n := newTestNode(t, 3, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 3, Term: 1, Entries: []Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(1, 3, "c")}})
	n.Ready()
	n.Step(Message{Type: MsgApp, From: 2, To: 3, Term: 2, Index: 1, LogTerm: 1, Entries: []Entry{ent(2, 2, "z")}})
	rd := n.Ready()
	if !reflect.DeepEqual(rd.Entries, []Entry{ent(2, 2, "z")}) {
		t.Fatalf("got %+v, want [z at 2]: storage must replace everything from index 2", rd.Entries)
	}
}

func TestRestartRestoresTermVoteLogAndCommit(t *testing.T) {
	log := []Entry{ent(1, 1, ""), ent(1, 2, "a"), ent(2, 3, "b")}
	n, err := New(Config{ID: 2, Peers: five, ElectionTick: testElection, HeartbeatTick: 1, Rand: fixedRand(0),
		HardState: HardState{Term: 5, Vote: 3, Commit: 2}, Entries: log})
	if err != nil {
		t.Fatal(err)
	}
	s := n.Status()
	if s.State != StateFollower || s.Term != 5 || s.Vote != 3 || s.Commit != 2 || s.LastIndex != 3 {
		t.Fatalf("restored status %+v", s)
	}
	if !reflect.DeepEqual(n.Log(), log) {
		t.Fatalf("restored log %+v", n.Log())
	}
	// Committed entries are handed out again so the state machine can be rebuilt.
	rd := n.Ready()
	if !reflect.DeepEqual(rd.CommittedEntries, log[:2]) {
		t.Fatalf("replayed %+v, want the first two entries", rd.CommittedEntries)
	}
	if rd.HardState != nil || len(rd.Entries) != 0 {
		t.Fatalf("restored state reported as new: %+v", rd)
	}
	// It already voted for 3 in term 5, so it must refuse 4 in the same term.
	n.Step(Message{Type: MsgVote, From: 4, To: 2, Term: 5, Index: 9, LogTerm: 9})
	if r := msgsOfType(n.Ready().Messages, MsgVoteResp); len(r) != 1 || !r[0].Reject {
		t.Fatalf("restarted node voted twice in term 5: %+v", r)
	}
}

func TestRestoreValidation(t *testing.T) {
	bad := []Config{
		{HardState: HardState{Term: 1, Commit: 3}, Entries: []Entry{ent(1, 1, "")}},     // commit past the log
		{HardState: HardState{Term: 1}, Entries: []Entry{ent(1, 2, "")}},                // log not starting at 1
		{HardState: HardState{Term: 1}, Entries: []Entry{ent(2, 1, "")}},                // entry newer than term
		{HardState: HardState{Term: 2}, Entries: []Entry{ent(2, 1, ""), ent(1, 2, "")}}, // terms going down
	}
	for i, c := range bad {
		c.ID, c.Peers, c.ElectionTick, c.HeartbeatTick, c.Rand = 1, five, 10, 1, fixedRand(0)
		if _, err := New(c); err == nil {
			t.Errorf("restore %d: expected an error", i)
		}
	}
}
