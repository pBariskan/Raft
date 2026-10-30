// Package check decides whether what clients saw is consistent with a single, correct copy of the
// key-value store (linearizability), using the Porcupine checker.
package check

import (
	"fmt"
	"math"
	"time"

	"github.com/anishathalye/porcupine"

	"github.com/pBariskan/Raft/kv"
)

// Operation is one client request as the client saw it: when it was sent (Call), when the answer
// arrived (Return) and what the answer was. An operation that never got an answer has Done false.
type Operation struct {
	Client uint64
	Op     kv.Op
	Key    string
	Value  string
	Call   int64
	Return int64
	Output kv.Result
	Done   bool
}

func (o Operation) String() string {
	switch o.Op {
	case kv.OpPut:
		return fmt.Sprintf("client %d: PUT %s=%s [%d, %d]", o.Client, o.Key, o.Value, o.Call, o.Return)
	case kv.OpDelete:
		return fmt.Sprintf("client %d: DELETE %s [%d, %d]", o.Client, o.Key, o.Call, o.Return)
	case kv.OpAppend:
		return fmt.Sprintf("client %d: APPEND %s+=%s [%d, %d]", o.Client, o.Key, o.Value, o.Call, o.Return)
	}
	return fmt.Sprintf("client %d: GET %s -> %q (found %v) [%d, %d]", o.Client, o.Key, o.Output.Value, o.Output.Found, o.Call, o.Return)
}

// Verdict is the outcome of a check.
type Verdict int

const (
	Linearizable Verdict = iota
	NotLinearizable
	Unknown // the checker ran out of time
)

func (v Verdict) String() string {
	return [...]string{"linearizable", "NOT linearizable", "unknown (timed out)"}[v]
}

type keyState struct {
	value   string
	present bool
}

var model = porcupine.Model{
	// Keys are independent: a history is linearizable if each key's history is.
	Partition: func(history []porcupine.Operation) [][]porcupine.Operation {
		byKey := map[string][]porcupine.Operation{}
		var order []string
		for _, op := range history {
			k := op.Input.(Operation).Key
			if _, ok := byKey[k]; !ok {
				order = append(order, k)
			}
			byKey[k] = append(byKey[k], op)
		}
		out := make([][]porcupine.Operation, 0, len(order))
		for _, k := range order {
			out = append(out, byKey[k])
		}
		return out
	},
	Init: func() interface{} { return keyState{} },
	Step: func(state, input, output interface{}) (bool, interface{}) {
		st, in := state.(keyState), input.(Operation)
		switch in.Op {
		case kv.OpPut:
			return true, keyState{value: in.Value, present: true}
		case kv.OpDelete:
			return true, keyState{}
		case kv.OpAppend:
			return true, keyState{value: st.value + in.Value, present: true}
		}
		out := output.(kv.Result)
		return out.Found == st.present && (!st.present || out.Value == st.value), st
	},
	DescribeOperation: func(input, output interface{}) string { return input.(Operation).String() },
}

// History checks a client history. Unanswered writes may or may not have taken effect, so they
// are kept with an open-ended return time; unanswered reads tell us nothing and are dropped.
func History(ops []Operation, timeout time.Duration) Verdict {
	var hist []porcupine.Operation
	for _, op := range ops {
		ret := op.Return
		if !op.Done {
			if op.Op == kv.OpGet {
				continue
			}
			ret = math.MaxInt64
		}
		hist = append(hist, porcupine.Operation{
			ClientId: int(op.Client),
			Input:    op,
			Call:     op.Call,
			Output:   op.Output,
			Return:   ret,
		})
	}
	switch porcupine.CheckOperationsTimeout(model, hist, timeout) {
	case porcupine.Ok:
		return Linearizable
	case porcupine.Illegal:
		return NotLinearizable
	}
	return Unknown
}
