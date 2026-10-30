// Package client talks to a real-mode cluster over HTTP.
//
// A client sends one request at a time, numbered 1, 2, 3, … It starts at any server, follows
// "not leader" answers to the leader they name, and on a timeout or a dead server tries the next
// one, always resending the same number, so the cluster applies the request once however many
// copies arrive.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/server"
)

// Client is one logical client. It is safe for use by one goroutine at a time.
type Client struct {
	id      uint64
	seq     uint64
	servers []string // host:port of every server
	target  int      // index into servers of who we think leads
	http    *http.Client
	mu      sync.Mutex
}

// New returns a client with the given ID, which must be unique among the cluster's clients.
func New(id uint64, servers []string) *Client {
	return &Client{
		id:      id,
		servers: servers,
		http:    &http.Client{Timeout: server.RequestTimeout + time.Second},
	}
}

// Do runs one operation and returns its result, retrying until it succeeds or ctx ends.
func (c *Client) Do(ctx context.Context, op kv.Op, key, value string) (kv.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	body, _ := json.Marshal(server.Request{Client: c.id, Seq: c.seq, Op: op.String(), Key: key, Value: value})
	tries := 0
	for {
		if err := ctx.Err(); err != nil {
			return kv.Result{}, err
		}
		rep, err := c.send(ctx, c.servers[c.target], body)
		switch {
		case err == nil && rep.Err == "":
			return kv.Result{Value: rep.Value, Found: rep.Found}, nil
		case err == nil && rep.Err == server.ErrNotLeader && rep.Leader != "":
			c.target = c.index(rep.Leader)
		default:
			// Down, cut off, mid-election, or timed out: try the next server.
			c.target = (c.target + 1) % len(c.servers)
		}
		tries++
		if tries%len(c.servers) == 0 {
			// Gone round every server: probably an election in progress. Give it a moment.
			select {
			case <-ctx.Done():
				return kv.Result{}, ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
}

func (c *Client) send(ctx context.Context, addr string, body []byte) (server.Reply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/kv", bytes.NewReader(body))
	if err != nil {
		return server.Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return server.Reply{}, err
	}
	defer resp.Body.Close()
	var rep server.Reply
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		return server.Reply{}, errors.New("client: bad reply")
	}
	return rep, nil
}

func (c *Client) index(addr string) int {
	for i, s := range c.servers {
		if s == addr {
			return i
		}
	}
	return c.target
}
