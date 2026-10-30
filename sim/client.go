package sim

import (
	"fmt"

	"github.com/pBariskan/Raft/check"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
)

// Clients live outside the cluster's network: every running server is reachable from every
// client (partitions only cut server-to-server links), but client traffic is delayed and dropped
// like any other message.

type reqKey struct{ client, seq uint64 }

type request struct {
	cmd kv.Command
	to  raft.NodeID
}

type response struct {
	from      raft.NodeID // the server answering
	client    uint64
	seq       uint64
	result    kv.Result
	notLeader bool
	hint      raft.NodeID // who the server believes leads, when notLeader
}

type client struct {
	id      uint64
	seq     uint64      // number of the request in progress (or of the last one)
	busy    bool        // a request is in progress
	cmd     kv.Command  // the request in progress
	op      int         // its position in the cluster's history
	target  raft.NodeID // the server we are talking to
	sentAt  int
	retries int
}

// clientTimeout is how long a client waits for an answer before trying another server.
func (c *Cluster) clientTimeout() int { return 3 * c.cfg.ElectionTick }

// NewClient adds a client and returns its ID (1, 2, 3, …).
func (c *Cluster) NewClient() uint64 {
	id := uint64(len(c.clients) + 1)
	c.clients = append(c.clients, &client{id: id, target: c.ids[int(id-1)%len(c.ids)]})
	return id
}

// ClientBusy reports whether a client is still waiting for an answer.
func (c *Cluster) ClientBusy(id uint64) bool { return c.clients[id-1].busy }

// Submit starts a request on an idle client. The client sends it, follows leader hints, and
// retries on a timeout until it gets an answer. Every request is recorded in History.
func (c *Cluster) Submit(clientID uint64, op kv.Op, key, value string) error {
	cl := c.clients[clientID-1]
	if cl.busy {
		return fmt.Errorf("sim: client %d is busy", clientID)
	}
	cl.seq++
	cl.busy, cl.retries = true, 0
	cl.cmd = kv.Command{Client: cl.id, Seq: cl.seq, Op: op, Key: key, Value: value}
	cl.op = len(c.history)
	c.history = append(c.history, check.Operation{Client: cl.id, Op: op, Key: key, Value: value, Call: int64(c.now)})
	c.emit(Event{Kind: EvCall, Client: cl.id, Node: cl.target, Text: describeCmd(cl.cmd)})
	c.sendRequest(cl)
	return nil
}

// History returns every client request so far, answered or not.
func (c *Cluster) History() []check.Operation {
	return append([]check.Operation(nil), c.history...)
}

func (c *Cluster) sendRequest(cl *client) {
	cl.sentAt = c.now
	c.sendClientTraffic(envelope{req: &request{cmd: cl.cmd, to: cl.target}})
}

func (c *Cluster) sendClientTraffic(e envelope) {
	c.seq++
	lost := c.net.Float64() < c.dropRate
	delay := c.cfg.MinDelay + c.net.IntN(c.cfg.MaxDelay-c.cfg.MinDelay+1)
	e.sentAt, e.deliverAt, e.seq = c.now, c.now+delay, c.seq
	if lost {
		c.lost(e)
		return
	}
	c.inflight = append(c.inflight, e)
}

func (c *Cluster) tickClients() {
	for _, cl := range c.clients {
		if cl.busy && c.now-cl.sentAt >= c.clientTimeout() {
			// No answer: the server may be down, cut off, or the request lost. Try the next one.
			cl.retries++
			cl.target = c.ids[(int(cl.target))%len(c.ids)]
			c.emit(Event{Kind: EvRetry, Client: cl.id, Node: cl.target})
			c.sendRequest(cl)
		}
	}
}

func (c *Cluster) deliverClientTraffic(e envelope) {
	if e.req != nil {
		c.serverReceived(e.req)
		return
	}
	r := e.resp
	cl := c.clients[r.client-1]
	if !cl.busy || r.seq != cl.seq {
		return // an answer to something the client already finished
	}
	if r.notLeader {
		if r.hint != raft.None {
			cl.target = r.hint
		} else {
			cl.target = c.ids[(int(cl.target))%len(c.ids)]
		}
		c.emit(Event{Kind: EvRedirect, Client: cl.id, Node: cl.target})
		c.sendRequest(cl)
		return
	}
	cl.busy = false
	op := &c.history[cl.op]
	op.Done, op.Return, op.Output = true, int64(c.now), r.result
	c.emit(Event{Kind: EvReturn, Client: cl.id, Node: r.from, Text: describeCmd(cl.cmd) + " → " + describeResult(r.result)})
}

func (c *Cluster) serverReceived(req *request) {
	n := c.nodes[req.to]
	if !n.up {
		c.dropped(envelope{req: req, sentAt: c.now, deliverAt: c.now})
		return
	}
	cmd := req.cmd
	reply := func(r response) {
		r.from, r.client, r.seq = req.to, cmd.Client, cmd.Seq
		c.sendClientTraffic(envelope{resp: &r})
	}
	st := n.rn.Status()
	if st.State != raft.StateLeader {
		reply(response{notLeader: true, hint: st.Lead})
		return
	}
	if c.cfg.Mistakes.LocalReads && cmd.Op == kv.OpGet {
		v, ok := n.store.Get(cmd.Key) // what this server believes, without asking anyone
		reply(response{result: kv.Result{Value: v, Found: ok}})
		return
	}
	if res, ok := n.store.Cached(cmd.Client, cmd.Seq); ok {
		reply(response{result: res}) // a retry of something already applied
		return
	}
	if n.store.LastSeq(cmd.Client) > cmd.Seq {
		return // the client has moved on; this is an old copy
	}
	key := reqKey{cmd.Client, cmd.Seq}
	if term, ok := n.waiting[key]; ok && term == st.Term {
		return // already in this term's log; the answer comes when it is applied
	}
	if err := n.rn.Propose(cmd.Encode()); err != nil {
		reply(response{notLeader: true, hint: st.Lead})
		return
	}
	n.waiting[key] = st.Term
	c.after(req.to)
}

// serverApplied answers a waiting client once its command has been applied on the server that
// proposed it.
func (c *Cluster) serverApplied(id raft.NodeID, a kv.Applied) {
	if !a.Client {
		return
	}
	if a.Duplicate {
		c.duplicates++
		c.emit(Event{Kind: EvDuplicate, Node: id, Client: a.Cmd.Client, Index: a.Index, Text: describeCmd(a.Cmd)})
	}
	n := c.nodes[id]
	key := reqKey{a.Cmd.Client, a.Cmd.Seq}
	if _, ok := n.waiting[key]; !ok {
		return
	}
	delete(n.waiting, key)
	c.sendClientTraffic(envelope{resp: &response{from: id, client: a.Cmd.Client, seq: a.Cmd.Seq, result: a.Result}})
}

// Duplicates counts how many times a server skipped a client command because it had already
// been applied (the retry path of duplicate detection).
func (c *Cluster) Duplicates() int { return c.duplicates }

// Store returns a node's key-value store (read-only use).
func (c *Cluster) Store(id raft.NodeID) *kv.Store { return c.nodes[id].store }
