package check

import (
	"testing"
	"time"

	"github.com/pBariskan/Raft/kv"
)

func put(c uint64, k, v string, call, ret int64) Operation {
	return Operation{Client: c, Op: kv.OpPut, Key: k, Value: v, Call: call, Return: ret, Done: true}
}

func get(c uint64, k, v string, found bool, call, ret int64) Operation {
	return Operation{Client: c, Op: kv.OpGet, Key: k, Call: call, Return: ret, Output: kv.Result{Value: v, Found: found}, Done: true}
}

func TestHistories(t *testing.T) {
	cases := []struct {
		name string
		ops  []Operation
		want Verdict
	}{
		{"read sees the write", []Operation{put(1, "x", "1", 0, 10), get(2, "x", "1", true, 20, 30)}, Linearizable},
		{"stale read after a finished write", []Operation{put(1, "x", "1", 0, 10), get(2, "x", "", false, 20, 30)}, NotLinearizable},
		{"concurrent read may see either", []Operation{put(1, "x", "1", 0, 30), get(2, "x", "", false, 10, 20)}, Linearizable},
		{"value nobody wrote", []Operation{put(1, "x", "1", 0, 10), get(2, "x", "2", true, 20, 30)}, NotLinearizable},
		{"read goes back in time", []Operation{
			put(1, "x", "1", 0, 10), put(1, "x", "2", 20, 30),
			get(2, "x", "2", true, 40, 50), get(3, "x", "1", true, 60, 70)}, NotLinearizable},
		{"unanswered write may have happened", []Operation{
			{Client: 1, Op: kv.OpPut, Key: "x", Value: "1", Call: 0},
			get(2, "x", "1", true, 50, 60)}, Linearizable},
		{"keys are independent", []Operation{put(1, "x", "1", 0, 10), put(1, "y", "2", 0, 10), get(2, "y", "2", true, 20, 30), get(2, "x", "1", true, 20, 30)}, Linearizable},
	}
	for _, c := range cases {
		if got := History(c.ops, time.Second); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAppendApplied(t *testing.T) {
	app := func(c uint64, v string, call, ret int64) Operation {
		return Operation{Client: c, Op: kv.OpAppend, Key: "x", Value: v, Call: call, Return: ret, Done: true}
	}
	ok := []Operation{app(1, "a", 0, 10), app(2, "b", 0, 10), get(3, "x", "ba", true, 20, 30)}
	if History(ok, time.Second) != Linearizable {
		t.Fatal("concurrent appends in either order should be allowed")
	}
	twice := []Operation{app(1, "a", 0, 10), get(3, "x", "aa", true, 20, 30)}
	if History(twice, time.Second) != NotLinearizable {
		t.Fatal("an append applied twice should be caught")
	}
}
