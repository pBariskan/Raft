package raft

import (
	"errors"
	"reflect"
	"testing"
)

// makeLeader returns node 1 as leader of term 1 in a five-node cluster, with its no-op appended
// and its first heartbeats drained.
func makeLeader(t *testing.T) *Node {
	t.Helper()
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	if n.Status().State != StateLeader {
		t.Fatal("setup: not leader")
	}
	n.Ready()
	return n
}

func ent(term, index uint64, data string) Entry {
	e := Entry{Term: term, Index: index}
	if data != "" {
		e.Data = []byte(data)
	}
	return e
}

func terms(es []Entry) []uint64 {
	out := []uint64{}
	for _, e := range es {
		out = append(out, e.Term)
	}
	return out
}

func lastApp(t *testing.T, ms []Message, to NodeID) Message {
	t.Helper()
	var found *Message
	for i := range ms {
		if ms[i].Type == MsgApp && ms[i].To == to {
			found = &ms[i]
		}
	}
	if found == nil {
		t.Fatalf("no MsgApp to %d in %+v", to, ms)
	}
	return *found
}

func TestNewLeaderAppendsNoop(t *testing.T) {
	n := makeCandidate(t)
	n.Step(Message{Type: MsgVoteResp, From: 2, To: 1, Term: 1})
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 1})
	if got, want := n.Log(), []Entry{ent(1, 1, "")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %+v, want just the no-op %+v", got, want)
	}
	m := lastApp(t, n.Ready().Messages, 2)
	if m.Index != 0 || m.LogTerm != 0 || len(m.Entries) != 1 || m.Entries[0].Term != 1 || m.Commit != 0 {
		t.Fatalf("first append = %+v, want the no-op after index 0", m)
	}
}

func TestProposeOnlyOnLeader(t *testing.T) {
	f := newTestNode(t, 1, five...)
	if err := f.Propose([]byte("x")); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("follower Propose err = %v, want ErrNotLeader", err)
	}
	n := makeLeader(t)
	if err := n.Propose([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if got := n.Log(); len(got) != 2 || got[1].Term != 1 || got[1].Index != 2 || string(got[1].Data) != "x" {
		t.Fatalf("log = %+v, want no-op then x at index 2", got)
	}
	m := lastApp(t, n.Ready().Messages, 3)
	if len(m.Entries) == 0 || string(m.Entries[len(m.Entries)-1].Data) != "x" {
		t.Fatalf("append to 3 = %+v, want it to carry x", m)
	}
}

func TestCommitNeedsMajority(t *testing.T) {
	n := makeLeader(t)
	n.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Index: 1})
	if c := n.Status().Commit; c != 0 {
		t.Fatalf("commit = %d with 2 of 5 copies, want 0", c)
	}
	if rd := n.Ready(); len(rd.CommittedEntries) != 0 {
		t.Fatalf("committed %+v too early", rd.CommittedEntries)
	}
	n.Step(Message{Type: MsgAppResp, From: 3, To: 1, Term: 1, Index: 1})
	if c := n.Status().Commit; c != 1 {
		t.Fatalf("commit = %d with 3 of 5 copies, want 1", c)
	}
	if rd := n.Ready(); len(rd.CommittedEntries) != 1 || rd.CommittedEntries[0].Index != 1 {
		t.Fatalf("committed entries = %+v, want the no-op", rd.CommittedEntries)
	}
	if rd := n.Ready(); len(rd.CommittedEntries) != 0 {
		t.Fatal("committed entries were delivered twice")
	}
}

// TestLeaderCommitsOnlyCurrentTermByCounting is Figure 8 of the Raft paper: an entry from an older
// term that sits on a majority is not committed until an entry of the leader's own term is.
func TestLeaderCommitsOnlyCurrentTermByCounting(t *testing.T) {
	n := newTestNode(t, 1, five...)
	n.Step(Message{Type: MsgApp, From: 2, To: 1, Term: 2, Entries: []Entry{ent(2, 1, "b")}})
	n.Ready()
	tickN(n, testElection) // term 3 candidate
	n.Step(Message{Type: MsgVoteResp, From: 3, To: 1, Term: 3})
	n.Step(Message{Type: MsgVoteResp, From: 4, To: 1, Term: 3})
	if s := n.Status(); s.State != StateLeader || s.Term != 3 {
		t.Fatalf("setup: %+v", s)
	}
	n.Ready()
	n.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 3, Index: 1})
	n.Step(Message{Type: MsgAppResp, From: 3, To: 1, Term: 3, Index: 1})
	if c := n.Status().Commit; c != 0 {
		t.Fatalf("committed the term-2 entry by counting copies (commit = %d)", c)
	}
	n.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 3, Index: 2})
	n.Step(Message{Type: MsgAppResp, From: 3, To: 1, Term: 3, Index: 2})
	if c := n.Status().Commit; c != 2 {
		t.Fatalf("commit = %d once the term-3 no-op is on a majority, want 2", c)
	}
	if got := terms(n.Ready().CommittedEntries); !reflect.DeepEqual(got, []uint64{2, 3}) {
		t.Fatalf("committed terms = %v, want [2 3]", got)
	}
}

func TestFollowerRejectsAppendThatDoesNotLineUp(t *testing.T) {
	n := newTestNode(t, 2, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Entries: []Entry{ent(1, 1, "a")}})
	n.Ready()

	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Index: 2, LogTerm: 1, Entries: []Entry{ent(1, 3, "c")}})
	r := n.Ready().Messages
	if len(r) != 1 || r[0].Type != MsgAppResp || !r[0].Reject || r[0].Index != 2 || r[0].Hint != 1 {
		t.Fatalf("missing prev entry: got %+v, want rejection of index 2 with hint 1", r)
	}

	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 2, Index: 1, LogTerm: 2, Entries: []Entry{ent(2, 2, "b")}})
	r = n.Ready().Messages
	if len(r) != 1 || !r[0].Reject {
		t.Fatalf("prev entry with the wrong term: got %+v, want a rejection", r)
	}
	if len(n.Log()) != 1 {
		t.Fatalf("log changed by a rejected append: %+v", n.Log())
	}
}

func TestFollowerReplacesConflictingEntries(t *testing.T) {
	n := newTestNode(t, 3, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 3, Term: 1, Entries: []Entry{ent(1, 1, "a"), ent(1, 2, "b")}})
	n.Step(Message{Type: MsgApp, From: 2, To: 3, Term: 2, Index: 2, LogTerm: 1, Entries: []Entry{ent(2, 3, "x"), ent(2, 4, "y")}})
	n.Ready()
	n.Step(Message{Type: MsgApp, From: 4, To: 3, Term: 3, Index: 2, LogTerm: 1, Entries: []Entry{ent(3, 3, "c")}})
	want := []Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(3, 3, "c")}
	if got := n.Log(); !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %+v, want %+v", got, want)
	}
	r := n.Ready().Messages
	if len(r) != 1 || r[0].Reject || r[0].Index != 3 {
		t.Fatalf("got %+v, want success up to index 3", r)
	}
}

func TestLateDuplicateAppendDoesNotTruncate(t *testing.T) {
	n := newTestNode(t, 2, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Entries: []Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(1, 3, "c")}})
	n.Ready()
	// An older copy of the first append arrives late, after the newer one.
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Entries: []Entry{ent(1, 1, "a")}})
	if got := len(n.Log()); got != 3 {
		t.Fatalf("log has %d entries after a late duplicate, want 3", got)
	}
	r := n.Ready().Messages
	if len(r) != 1 || r[0].Reject || r[0].Index != 1 {
		t.Fatalf("got %+v, want success for what this message covered (index 1)", r)
	}
}

func TestFollowerCommitIsCappedByWhatTheMessageVerified(t *testing.T) {
	n := newTestNode(t, 2, five...)
	// The follower has a stale entry 2 from term 1 that the leader of term 2 will replace.
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Entries: []Entry{ent(1, 1, "a"), ent(1, 2, "old")}})
	n.Ready()
	n.Step(Message{Type: MsgApp, From: 3, To: 2, Term: 2, Entries: []Entry{ent(1, 1, "a")}, Commit: 5})
	if c := n.Status().Commit; c != 1 {
		t.Fatalf("commit = %d, want 1: entry 2 was not checked against the leader's log", c)
	}
}

func TestCommittedEntriesDeliveredOnceInOrder(t *testing.T) {
	n := newTestNode(t, 2, five...)
	es := []Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(1, 3, "c")}
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Entries: es, Commit: 2})
	if got := n.Ready().CommittedEntries; !reflect.DeepEqual(got, es[:2]) {
		t.Fatalf("got %+v, want %+v", got, es[:2])
	}
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Index: 3, LogTerm: 1, Commit: 3})
	if got := n.Ready().CommittedEntries; !reflect.DeepEqual(got, es[2:]) {
		t.Fatalf("got %+v, want %+v", got, es[2:])
	}
	// A heartbeat with an older commit index must not move the commit index back.
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 1, Index: 3, LogTerm: 1, Commit: 1})
	if c := n.Status().Commit; c != 3 {
		t.Fatalf("commit went back to %d", c)
	}
}

func TestLeaderBacksUpAfterRejection(t *testing.T) {
	n := makeLeader(t)
	for _, d := range []string{"x", "y"} {
		if err := n.Propose([]byte(d)); err != nil {
			t.Fatal(err)
		}
	}
	n.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Index: 3})
	n.Ready()
	n.Tick()
	if m := lastApp(t, n.Ready().Messages, 2); m.Index != 3 || len(m.Entries) != 0 {
		t.Fatalf("heartbeat to an up-to-date follower = %+v, want prev 3 and no entries", m)
	}
	// Node 3 never acknowledged anything and rejects; its hint says its log ends at 0.
	n.Step(Message{Type: MsgAppResp, From: 3, To: 1, Term: 1, Index: 0, Reject: true, Hint: 0})
	n.Ready()
	n.Tick()
	if m := lastApp(t, n.Ready().Messages, 3); m.Index != 0 || len(m.Entries) != 3 {
		t.Fatalf("append to 3 = %+v, want everything from index 1", m)
	}
	// A stale rejection (for a prev index the leader is no longer sending) is ignored.
	n.Step(Message{Type: MsgAppResp, From: 2, To: 1, Term: 1, Index: 1, Reject: true, Hint: 0})
	n.Ready()
	n.Tick()
	if m := lastApp(t, n.Ready().Messages, 2); m.Index != 3 {
		t.Fatalf("stale rejection moved node 2 back: %+v", m)
	}
}

func TestVoteRequiresUpToDateLog(t *testing.T) {
	n := newTestNode(t, 2, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 2, Entries: []Entry{ent(1, 1, "a"), ent(2, 2, "b")}})
	n.Ready()
	cases := []struct {
		term, lastIndex, lastTerm uint64
		grant                     bool
	}{
		{3, 5, 1, false}, // longer, but its last entry is from an older term
		{4, 1, 2, false}, // same last term, shorter
		{5, 2, 2, true},  // same last term, same length
		{6, 1, 3, true},  // newer last term wins even if shorter
	}
	for _, c := range cases {
		n.Step(Message{Type: MsgVote, From: 3, To: 2, Term: c.term, Index: c.lastIndex, LogTerm: c.lastTerm})
		r := msgsOfType(n.Ready().Messages, MsgVoteResp)
		if len(r) != 1 || r[0].Reject == c.grant {
			t.Fatalf("candidate last=(%d, term %d) in term %d: got %+v, want grant=%v", c.lastIndex, c.lastTerm, c.term, r, c.grant)
		}
	}
}

func TestCandidateAdvertisesItsLastEntry(t *testing.T) {
	n := newTestNode(t, 2, five...)
	n.Step(Message{Type: MsgApp, From: 1, To: 2, Term: 2, Entries: []Entry{ent(1, 1, "a"), ent(2, 2, "b")}})
	n.Ready()
	tickN(n, testElection)
	for _, m := range msgsOfType(n.Ready().Messages, MsgVote) {
		if m.Index != 2 || m.LogTerm != 2 {
			t.Fatalf("vote request %+v, want last index 2, last term 2", m)
		}
	}
}
