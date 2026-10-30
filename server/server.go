// Package server runs one Raft node as a real process: a wall-clock ticker, disk storage, HTTP
// between nodes, and an HTTP API for clients.
//
// One goroutine owns the Raft node, the disk and the key-value store; everything else talks to
// it through channels, so the pure state machine in package raft never sees a lock. Each time
// round its loop it handles whatever arrived (a tick, messages from peers, client requests),
// then does what Raft's Ready asks, in order: save to disk and sync, then send messages, then
// apply committed commands and answer the clients waiting for them.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/storage"
	"github.com/pBariskan/Raft/transport"
)

// Config describes one node.
type Config struct {
	ID raft.NodeID
	// Addrs maps every node in the cluster, this one included, to its host:port. Peers talk to
	// each other and clients talk to the cluster on the same address.
	Addrs map[raft.NodeID]string
	Dir   string // where the log and hard state live

	Tick          time.Duration // wall-clock length of one Raft tick (default 10ms)
	ElectionTick  int           // default 10: an election timeout of 100–200ms with a 10ms tick
	HeartbeatTick int           // default 1
	NoSync        bool          // skip fsync (tests and benchmarks only)
	Seed          uint64        // seeds the election timer; 0 picks one at random

	// Listener, if set, is used instead of listening on Addrs[ID] (tests pass one bound to
	// port 0 so they never collide).
	Listener net.Listener
	Logf     func(format string, args ...any) // default log.Printf
}

// Server is one running node.
type Server struct {
	cfg  Config
	rn   *raft.Node
	disk *storage.Disk
	tr   *transport.Transport
	http *http.Server

	store   *kv.Store
	waiting map[key]*waiter

	inbox    chan []raft.Message
	requests chan *pending
	status   chan chan Status
	stop     chan struct{}
	done     chan struct{}
	failed   error

	convenience uint64 // counter for client IDs of one-off /kv/{key} requests
	stopOnce    sync.Once
}

type key struct{ client, seq uint64 }

// A waiter is a client request proposed in some term, waiting to be applied.
type waiter struct {
	term    uint64
	answers []chan Reply
}

type pending struct {
	cmd   kv.Command
	reply chan Reply
}

// Status is what /status reports.
type Status struct {
	ID        raft.NodeID `json:"id"`
	State     string      `json:"state"`
	Term      uint64      `json:"term"`
	Leader    raft.NodeID `json:"leader"`
	Commit    uint64      `json:"commit"`
	LastIndex uint64      `json:"last_index"`
	Applied   uint64      `json:"applied"`
}

// Start opens the node's storage, restores its log, and starts serving.
func Start(cfg Config) (*Server, error) {
	if cfg.Tick == 0 {
		cfg.Tick = 10 * time.Millisecond
	}
	if cfg.ElectionTick == 0 {
		cfg.ElectionTick = 10
	}
	if cfg.HeartbeatTick == 0 {
		cfg.HeartbeatTick = 1
	}
	if cfg.Seed == 0 {
		cfg.Seed = rand.Uint64()
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if _, ok := cfg.Addrs[cfg.ID]; !ok {
		return nil, fmt.Errorf("server: node %d has no address", cfg.ID)
	}
	disk, hs, entries, err := storage.Open(cfg.Dir, storage.Options{NoSync: cfg.NoSync})
	if err != nil {
		return nil, err
	}
	var peers []raft.NodeID
	for id := range cfg.Addrs {
		peers = append(peers, id)
	}
	rn, err := raft.New(raft.Config{
		ID:            cfg.ID,
		Peers:         peers,
		ElectionTick:  cfg.ElectionTick,
		HeartbeatTick: cfg.HeartbeatTick,
		Rand:          rand.New(rand.NewPCG(cfg.Seed, uint64(cfg.ID))),
		HardState:     hs,
		Entries:       entries,
	})
	if err != nil {
		disk.Close()
		return nil, err
	}
	urls := map[raft.NodeID]string{}
	for id, a := range cfg.Addrs {
		urls[id] = "http://" + a
	}
	ln := cfg.Listener
	if ln == nil {
		if ln, err = net.Listen("tcp", cfg.Addrs[cfg.ID]); err != nil {
			disk.Close()
			return nil, err
		}
	}
	s := &Server{
		cfg:      cfg,
		rn:       rn,
		disk:     disk,
		tr:       transport.New(cfg.ID, urls),
		store:    kv.NewStore(),
		waiting:  map[key]*waiter{},
		inbox:    make(chan []raft.Message, 256),
		requests: make(chan *pending, 256),
		status:   make(chan chan Status),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	s.http = &http.Server{Handler: s.routes()}
	go s.http.Serve(ln)
	go s.loop()
	cfg.Logf("node %d: up on %s with %d log entries, term %d", cfg.ID, ln.Addr(), len(entries), hs.Term)
	return s, nil
}

// Stop halts the node at once, the way a crash would: open connections are cut, nothing is
// handed over. What was synced to disk is what the node finds when it starts again.
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		s.http.Close()
		close(s.stop)
		<-s.done
		s.tr.Stop()
		s.disk.Close()
	})
}

// Done is closed when the node has stopped, by Stop or because its disk failed.
func (s *Server) Done() <-chan struct{} { return s.done }

// Err returns why the node stopped on its own (a disk error), or nil.
func (s *Server) Err() error {
	select {
	case <-s.done:
		return s.failed
	default:
		return nil
	}
}

// Status asks the node for its current state.
func (s *Server) Status() (Status, error) {
	ch := make(chan Status, 1)
	select {
	case s.status <- ch:
		return <-ch, nil
	case <-s.done:
		return Status{}, errStopped
	}
}

var errStopped = errors.New("server: stopped")

func (s *Server) loop() {
	defer close(s.done)
	ticker := time.NewTicker(s.cfg.Tick)
	defer ticker.Stop()
	if err := s.ready(); err != nil { // a restarted node applies its committed entries again
		s.fail(err)
		return
	}
	for {
		select {
		case <-s.stop:
			s.answerAll(Reply{Err: "node stopped"})
			return
		case <-ticker.C:
			s.rn.Tick()
		case b := <-s.inbox:
			s.step(b)
		case p := <-s.requests:
			s.request(p)
		case ch := <-s.status:
			ch <- s.snapshot()
		}
		// Take whatever else is already waiting before going to disk, so many requests share
		// one fsync (group commit).
	drain:
		for i := 0; i < 1024; i++ {
			select {
			case b := <-s.inbox:
				s.step(b)
			case p := <-s.requests:
				s.request(p)
			default:
				break drain
			}
		}
		if err := s.ready(); err != nil {
			s.fail(err)
			return
		}
	}
}

func (s *Server) fail(err error) {
	// A node that can't write its disk must stop: carrying on would mean acknowledging
	// entries and votes it has not stored.
	s.failed = err
	s.cfg.Logf("node %d: stopping: %v", s.cfg.ID, err)
	s.answerAll(Reply{Err: "node failed"})
	s.http.Close()
}

func (s *Server) step(batch []raft.Message) {
	for _, m := range batch {
		s.rn.Step(m)
	}
}

// ready does what Raft asks, in the order that keeps its promises: store, then send, then apply.
func (s *Server) ready() error {
	rd := s.rn.Ready()
	if err := s.disk.Save(rd.HardState, rd.Entries); err != nil {
		return err
	}
	s.tr.Send(rd.Messages)
	for _, e := range rd.CommittedEntries {
		s.applied(s.store.Apply(e))
	}
	st := s.rn.Status()
	for k, w := range s.waiting {
		if st.State != raft.StateLeader || st.Term != w.term {
			// Leadership moved on. The entry may still be committed by the next leader, or may
			// be erased; the client retries there and duplicate detection sorts it out.
			s.answer(k, s.notLeader(st.Lead))
		}
	}
	return nil
}

func (s *Server) applied(a kv.Applied) {
	if !a.Client {
		return
	}
	s.answer(key{a.Cmd.Client, a.Cmd.Seq}, Reply{Value: a.Result.Value, Found: a.Result.Found})
}

func (s *Server) answer(k key, r Reply) {
	w, ok := s.waiting[k]
	if !ok {
		return
	}
	delete(s.waiting, k)
	for _, ch := range w.answers {
		ch <- r // buffered, never blocks
	}
}

func (s *Server) answerAll(r Reply) {
	for k := range s.waiting {
		s.answer(k, r)
	}
}

func (s *Server) notLeader(lead raft.NodeID) Reply {
	r := Reply{Err: ErrNotLeader}
	if lead != raft.None {
		r.Leader = s.cfg.Addrs[lead]
	}
	return r
}

// request handles a client command the same way the simulator's servers do.
func (s *Server) request(p *pending) {
	cmd := p.cmd
	st := s.rn.Status()
	if st.State != raft.StateLeader {
		p.reply <- s.notLeader(st.Lead)
		return
	}
	if res, ok := s.store.Cached(cmd.Client, cmd.Seq); ok {
		p.reply <- Reply{Value: res.Value, Found: res.Found} // a retry of something already applied
		return
	}
	if s.store.LastSeq(cmd.Client) > cmd.Seq {
		p.reply <- Reply{Err: "stale request: the client has moved on"}
		return
	}
	k := key{cmd.Client, cmd.Seq}
	if w, ok := s.waiting[k]; ok && w.term == st.Term {
		w.answers = append(w.answers, p.reply) // already in this term's log
		return
	}
	if err := s.rn.Propose(cmd.Encode()); err != nil {
		p.reply <- s.notLeader(st.Lead)
		return
	}
	s.waiting[k] = &waiter{term: st.Term, answers: []chan Reply{p.reply}}
}

func (s *Server) snapshot() Status {
	st := s.rn.Status()
	return Status{
		ID:        st.ID,
		State:     strings.ToLower(st.State.String()),
		Term:      st.Term,
		Leader:    st.Lead,
		Commit:    st.Commit,
		LastIndex: st.LastIndex,
		Applied:   s.store.Applied(),
	}
}

// Do submits a command and waits for its answer (or for ctx to end).
func (s *Server) Do(ctx context.Context, cmd kv.Command) Reply {
	p := &pending{cmd: cmd, reply: make(chan Reply, 1)}
	select {
	case s.requests <- p:
	case <-s.done:
		return Reply{Err: "node stopped"}
	case <-ctx.Done():
		return Reply{Err: ErrTimeout}
	}
	select {
	case r := <-p.reply:
		return r
	case <-s.done:
		return Reply{Err: "node stopped"}
	case <-ctx.Done():
		return Reply{Err: ErrTimeout}
	}
}
