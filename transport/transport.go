// Package transport carries Raft messages between real processes over HTTP.
//
// Each node POSTs batches of messages as JSON to a peer's /raft endpoint. One sender per peer
// sends one batch at a time; whatever Raft produces while a batch is in flight waits and goes in
// the next one. Delivery is best effort: a failed POST is dropped, because Raft already repeats
// what matters (heartbeats resend unacknowledged entries, candidates retry elections).
package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pBariskan/Raft/raft"
)

// Path is where a node accepts Raft messages.
const Path = "/raft"

// maxQueue bounds how many messages wait for a slow or dead peer; older ones are dropped.
const maxQueue = 1024

// Transport sends one node's outgoing messages. Send never blocks.
type Transport struct {
	peers  map[raft.NodeID]*peer
	client *http.Client
	stop   chan struct{}
	wg     sync.WaitGroup
}

type peer struct {
	url   string
	mu    sync.Mutex
	queue []raft.Message
	wake  chan struct{}
}

// New starts a sender for every peer other than self. urls maps each node to its base URL,
// for example "http://127.0.0.1:7001".
func New(self raft.NodeID, urls map[raft.NodeID]string) *Transport {
	t := &Transport{
		peers: map[raft.NodeID]*peer{},
		client: &http.Client{
			Timeout:   time.Second,
			Transport: &http.Transport{MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
		},
		stop: make(chan struct{}),
	}
	for id, u := range urls {
		if id == self {
			continue
		}
		p := &peer{url: u + Path, wake: make(chan struct{}, 1)}
		t.peers[id] = p
		t.wg.Add(1)
		go t.run(p)
	}
	return t
}

// Send queues messages for their peers.
func (t *Transport) Send(msgs []raft.Message) {
	for _, m := range msgs {
		p, ok := t.peers[m.To]
		if !ok {
			continue
		}
		p.mu.Lock()
		if len(p.queue) >= maxQueue {
			p.queue = p.queue[1:]
		}
		p.queue = append(p.queue, m)
		p.mu.Unlock()
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
}

// Stop ends every sender. Queued messages are dropped.
func (t *Transport) Stop() {
	close(t.stop)
	t.wg.Wait()
	t.client.CloseIdleConnections()
}

func (t *Transport) run(p *peer) {
	defer t.wg.Done()
	for {
		select {
		case <-t.stop:
			return
		case <-p.wake:
		}
		p.mu.Lock()
		batch := coalesce(p.queue)
		p.queue = nil
		p.mu.Unlock()
		if len(batch) == 0 {
			continue
		}
		body, err := json.Marshal(batch)
		if err != nil {
			continue
		}
		resp, err := t.client.Post(p.url, "application/json", bytes.NewReader(body))
		if err != nil {
			// The peer is down or unreachable. Wait a little so a dead peer doesn't cost a busy
			// loop; heartbeats will carry the same information again.
			select {
			case <-t.stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// coalesce keeps every message except AppendEntries that a later AppendEntries in the same batch
// replaces. The leader's latest append to a peer starts at that peer's current next index and
// carries the newest commit index, so the earlier ones add nothing.
func coalesce(q []raft.Message) []raft.Message {
	last := -1
	for i, m := range q {
		if m.Type == raft.MsgApp {
			last = i
		}
	}
	out := q[:0:0]
	for i, m := range q {
		if m.Type == raft.MsgApp && i != last {
			continue
		}
		out = append(out, m)
	}
	return out
}

// Handler accepts message batches from peers and hands each batch to deliver.
func Handler(deliver func([]raft.Message)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var batch []raft.Message
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&batch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		deliver(batch)
		w.WriteHeader(http.StatusNoContent)
	})
}
