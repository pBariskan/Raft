package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/transport"
)

// The client API.
//
//	POST /kv        {"client":7,"seq":1,"op":"PUT","key":"x","value":"5"}  → Reply
//	GET  /kv/{key}  read one key                     (curl -L follows the redirect to the leader)
//	PUT  /kv/{key}  write one key; the body is the value
//	DELETE /kv/{key}
//	GET  /status    this node's role, term, leader and log position
//
// POST /kv is what real clients use: the client picks a unique ID and numbers its requests, and
// resends the same number when it retries, so a retried write is applied once. A non-leader
// answers 421 with the leader's address. The /kv/{key} shortcuts are for trying the cluster by
// hand: each is a one-off request with a fresh client ID, and a non-leader redirects to the
// leader.

// Request is the body of POST /kv.
type Request struct {
	Client uint64 `json:"client"`
	Seq    uint64 `json:"seq"`
	Op     string `json:"op"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
}

// Reply is the answer to a client request. Err is empty on success.
type Reply struct {
	Value  string `json:"value,omitempty"`
	Found  bool   `json:"found,omitempty"`
	Err    string `json:"error,omitempty"`
	Leader string `json:"leader,omitempty"` // with ErrNotLeader: where the leader is, if known
}

// Errors a Reply can carry that a client acts on.
const (
	ErrNotLeader = "not leader"
	ErrTimeout   = "timeout"
)

// RequestTimeout is how long the server waits for a command to be committed before giving up
// on it. The client then retries, possibly elsewhere.
const RequestTimeout = 2 * time.Second

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST "+transport.Path, transport.Handler(func(b []raft.Message) {
		select {
		case s.inbox <- b:
		default: // overloaded or stopped: drop, Raft resends what matters
		}
	}))
	mux.HandleFunc("POST /kv", s.handleCommand)
	mux.HandleFunc("GET /kv/{key}", s.handleKey)
	mux.HandleFunc("PUT /kv/{key}", s.handleKey)
	mux.HandleFunc("DELETE /kv/{key}", s.handleKey)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.Status()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	return mux
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, Reply{Err: err.Error()})
		return
	}
	op, err := kv.ParseOp(req.Op)
	if err != nil || req.Client == 0 || req.Seq == 0 {
		writeJSON(w, http.StatusBadRequest, Reply{Err: "need client > 0, seq > 0 and op GET, PUT, DELETE or APPEND"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
	defer cancel()
	rep := s.Do(ctx, kv.Command{Client: req.Client, Seq: req.Seq, Op: op, Key: req.Key, Value: req.Value})
	writeJSON(w, statusCode(rep), rep)
}

func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	cmd := kv.Command{
		Client: 1<<63 | uint64(s.cfg.ID)<<40 | atomic.AddUint64(&s.convenience, 1),
		Seq:    1,
		Key:    r.PathValue("key"),
	}
	switch r.Method {
	case http.MethodGet:
		cmd.Op = kv.OpGet
	case http.MethodPut:
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, Reply{Err: err.Error()})
			return
		}
		cmd.Op, cmd.Value = kv.OpPut, string(b)
	case http.MethodDelete:
		cmd.Op = kv.OpDelete
	}
	ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
	defer cancel()
	rep := s.Do(ctx, cmd)
	if rep.Err == ErrNotLeader && rep.Leader != "" {
		http.Redirect(w, r, "http://"+rep.Leader+r.URL.Path, http.StatusTemporaryRedirect)
		return
	}
	writeJSON(w, statusCode(rep), rep)
}

func statusCode(r Reply) int {
	switch r.Err {
	case "":
		return http.StatusOK
	case ErrNotLeader:
		return http.StatusMisdirectedRequest
	case ErrTimeout:
		return http.StatusGatewayTimeout
	}
	return http.StatusServiceUnavailable
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// ParseAddrs reads a cluster description like "1=127.0.0.1:7001,2=127.0.0.1:7002".
func ParseAddrs(s string) (map[raft.NodeID]string, error) {
	out := map[raft.NodeID]string{}
	for _, part := range strings.Split(s, ",") {
		id, addr, ok := strings.Cut(strings.TrimSpace(part), "=")
		n, err := strconv.ParseUint(id, 10, 64)
		if !ok || err != nil || n == 0 || addr == "" {
			return nil, fmt.Errorf("server: bad cluster entry %q (want id=host:port)", part)
		}
		if _, dup := out[raft.NodeID(n)]; dup {
			return nil, fmt.Errorf("server: node %d listed twice", n)
		}
		out[raft.NodeID(n)] = addr
	}
	return out, nil
}
