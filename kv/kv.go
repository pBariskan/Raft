// Package kv is the replicated key-value state machine (PUT, GET, DELETE) that runs on top of
// Raft, with client sessions so a retried request is applied only once.
//
// Every operation, reads included, goes through the Raft log. Each server applies the same
// committed commands in the same order, so every server's store holds the same data.
package kv

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/pBariskan/Raft/raft"
)

// Op is a key-value operation.
type Op uint8

const (
	OpGet Op = iota
	OpPut
	OpDelete
)

func (o Op) String() string {
	switch o {
	case OpGet:
		return "GET"
	case OpPut:
		return "PUT"
	case OpDelete:
		return "DELETE"
	}
	return fmt.Sprintf("Op(%d)", uint8(o))
}

// Command is one client request as stored in the log. Client and Seq identify it: a client sends
// one request at a time and numbers them 1, 2, 3, …, reusing the number when it retries.
type Command struct {
	Client uint64 `json:"c"`
	Seq    uint64 `json:"s"`
	Op     Op     `json:"o"`
	Key    string `json:"k"`
	Value  string `json:"v,omitempty"`
}

func (c Command) String() string {
	if c.Op == OpPut {
		return fmt.Sprintf("%s %s=%s (client %d #%d)", c.Op, c.Key, c.Value, c.Client, c.Seq)
	}
	return fmt.Sprintf("%s %s (client %d #%d)", c.Op, c.Key, c.Client, c.Seq)
}

// Encode turns a command into log entry data.
func (c Command) Encode() []byte {
	b, err := json.Marshal(c)
	if err != nil {
		panic(err) // a struct of plain fields always marshals
	}
	return b
}

// Decode parses log entry data written by Encode.
func Decode(b []byte) (Command, error) {
	var c Command
	if err := json.Unmarshal(b, &c); err != nil {
		return Command{}, fmt.Errorf("kv: bad command: %w", err)
	}
	return c, nil
}

// Result is what a client gets back. For GET, Value and Found; PUT and DELETE return nothing.
type Result struct {
	Value string `json:"v,omitempty"`
	Found bool   `json:"f,omitempty"`
}

// Applied describes what applying one log entry did.
type Applied struct {
	Index     uint64
	Client    bool // false for no-ops and anything that isn't a client command
	Cmd       Command
	Result    Result
	Duplicate bool // the command was already applied; Result is the original answer
}

type session struct {
	lastSeq    uint64
	lastResult Result
}

// Store is the state machine. It is not safe for concurrent use.
type Store struct {
	data     map[string]string
	sessions map[uint64]session
	applied  uint64
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{data: map[string]string{}, sessions: map[uint64]session{}}
}

// Apply runs the next committed entry. Entries must be applied in log order with no gaps.
func (s *Store) Apply(e raft.Entry) Applied {
	if e.Index != s.applied+1 {
		panic(fmt.Sprintf("kv: applying entry %d after %d", e.Index, s.applied))
	}
	s.applied = e.Index
	out := Applied{Index: e.Index}
	if e.Data == nil {
		return out // a leader's no-op
	}
	cmd, err := Decode(e.Data)
	if err != nil {
		return out
	}
	out.Client, out.Cmd = true, cmd

	// Duplicate detection: a retried request can be committed more than once (the first leader
	// committed it but the client never heard back). Apply it only the first time.
	ses := s.sessions[cmd.Client]
	if cmd.Seq <= ses.lastSeq {
		out.Duplicate = true
		if cmd.Seq == ses.lastSeq {
			out.Result = ses.lastResult
		}
		return out
	}

	switch cmd.Op {
	case OpGet:
		v, ok := s.data[cmd.Key]
		out.Result = Result{Value: v, Found: ok}
	case OpPut:
		s.data[cmd.Key] = cmd.Value
	case OpDelete:
		delete(s.data, cmd.Key)
	}
	s.sessions[cmd.Client] = session{lastSeq: cmd.Seq, lastResult: out.Result}
	return out
}

// Cached returns the answer to a client's request if it was already applied as that client's
// latest request.
func (s *Store) Cached(client, seq uint64) (Result, bool) {
	ses, ok := s.sessions[client]
	if !ok || ses.lastSeq != seq {
		return Result{}, false
	}
	return ses.lastResult, true
}

// LastSeq returns the highest request number applied for a client.
func (s *Store) LastSeq(client uint64) uint64 { return s.sessions[client].lastSeq }

// Applied returns the index of the last applied entry.
func (s *Store) Applied() uint64 { return s.applied }

// Get reads a key directly from this server's copy (for display; clients read through the log).
func (s *Store) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// Keys returns the stored keys in sorted order.
func (s *Store) Keys() []string {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
