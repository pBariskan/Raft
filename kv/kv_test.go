package kv

import (
	"testing"

	"github.com/pBariskan/Raft/raft"
)

func entry(idx uint64, c Command) raft.Entry {
	return raft.Entry{Term: 1, Index: idx, Data: c.Encode()}
}

func TestCommandRoundTrip(t *testing.T) {
	c := Command{Client: 7, Seq: 3, Op: OpPut, Key: "x", Value: "5"}
	got, err := Decode(c.Encode())
	if err != nil || got != c {
		t.Fatalf("got %+v, %v; want %+v", got, err, c)
	}
	if _, err := Decode([]byte("not json")); err == nil {
		t.Fatal("decoded garbage")
	}
}

func TestPutGetDelete(t *testing.T) {
	s := NewStore()
	steps := []struct {
		cmd  Command
		want Result
	}{
		{Command{Client: 1, Seq: 1, Op: OpGet, Key: "x"}, Result{}},
		{Command{Client: 1, Seq: 2, Op: OpPut, Key: "x", Value: "5"}, Result{}},
		{Command{Client: 1, Seq: 3, Op: OpGet, Key: "x"}, Result{Value: "5", Found: true}},
		{Command{Client: 1, Seq: 4, Op: OpDelete, Key: "x"}, Result{}},
		{Command{Client: 1, Seq: 5, Op: OpGet, Key: "x"}, Result{}},
	}
	for i, st := range steps {
		a := s.Apply(entry(uint64(i+1), st.cmd))
		if !a.Client || a.Result != st.want || a.Duplicate {
			t.Fatalf("step %d %+v: got %+v, want %+v", i, st.cmd, a, st.want)
		}
	}
}

func TestNoopAndForeignEntriesAreSkipped(t *testing.T) {
	s := NewStore()
	if a := s.Apply(raft.Entry{Term: 1, Index: 1}); a.Client {
		t.Fatalf("no-op treated as a client command: %+v", a)
	}
	if s.Applied() != 1 {
		t.Fatalf("applied = %d, want 1", s.Applied())
	}
}

func TestRetriedCommandAppliesOnce(t *testing.T) {
	s := NewStore()
	put := Command{Client: 1, Seq: 1, Op: OpPut, Key: "n", Value: "a"}
	s.Apply(entry(1, put))
	s.Apply(entry(2, Command{Client: 2, Seq: 1, Op: OpPut, Key: "n", Value: "b"}))
	// Client 1's retry of seq 1 was committed a second time by a new leader.
	a := s.Apply(entry(3, put))
	if !a.Duplicate {
		t.Fatalf("second copy of client 1 seq 1 not flagged as duplicate: %+v", a)
	}
	if v, _ := s.Get("n"); v != "b" {
		t.Fatalf("n = %q, want b: the duplicate put was applied again", v)
	}
}

func TestDuplicateGetReturnsTheOriginalAnswer(t *testing.T) {
	s := NewStore()
	s.Apply(entry(1, Command{Client: 2, Seq: 1, Op: OpPut, Key: "k", Value: "old"}))
	get := Command{Client: 1, Seq: 1, Op: OpGet, Key: "k"}
	first := s.Apply(entry(2, get))
	s.Apply(entry(3, Command{Client: 2, Seq: 2, Op: OpPut, Key: "k", Value: "new"}))
	again := s.Apply(entry(4, get))
	if again.Result != first.Result || !again.Duplicate {
		t.Fatalf("retried get answered %+v, want the original %+v", again, first)
	}
	if r, ok := s.Cached(1, 1); !ok || r != first.Result {
		t.Fatalf("Cached(1,1) = %+v, %v", r, ok)
	}
}

func TestOutOfOrderAppliesPanic(t *testing.T) {
	s := NewStore()
	defer func() {
		if recover() == nil {
			t.Fatal("applying index 2 before 1 did not panic")
		}
	}()
	s.Apply(entry(2, Command{Client: 1, Seq: 1, Op: OpGet, Key: "x"}))
}
