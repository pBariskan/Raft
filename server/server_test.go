package server_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pBariskan/Raft/client"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/server"
)

// Real-mode tests run real processes' code on real time, sockets and disks, so their exact
// schedule is not reproducible. They check outcomes (the data, who leads eventually), never
// timings, and give every wait a generous deadline.

type cluster struct {
	t     *testing.T
	addrs map[raft.NodeID]string
	dirs  map[raft.NodeID]string
	nodes map[raft.NodeID]*server.Server
}

func newCluster(t *testing.T, n int) *cluster {
	t.Helper()
	c := &cluster{t: t, addrs: map[raft.NodeID]string{}, dirs: map[raft.NodeID]string{}, nodes: map[raft.NodeID]*server.Server{}}
	lns := map[raft.NodeID]net.Listener{}
	for i := 1; i <= n; i++ {
		id := raft.NodeID(i)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		lns[id], c.addrs[id], c.dirs[id] = ln, ln.Addr().String(), t.TempDir()
	}
	for id, ln := range lns {
		c.start(id, ln)
	}
	t.Cleanup(func() {
		for _, s := range c.nodes {
			s.Stop()
		}
	})
	return c
}

func (c *cluster) start(id raft.NodeID, ln net.Listener) {
	c.t.Helper()
	s, err := server.Start(server.Config{
		ID: id, Addrs: c.addrs, Dir: c.dirs[id], Seed: uint64(id),
		Tick: 5 * time.Millisecond, Listener: ln, Logf: c.t.Logf,
	})
	if err != nil {
		c.t.Fatal(err)
	}
	c.nodes[id] = s
}

func (c *cluster) crash(id raft.NodeID) {
	c.nodes[id].Stop()
	delete(c.nodes, id)
}

// restart boots a crashed node again on the same address and directory.
func (c *cluster) restart(id raft.NodeID) {
	c.t.Helper()
	var ln net.Listener
	var err error
	for i := 0; i < 50; i++ { // the port can take a moment to free up
		if ln, err = net.Listen("tcp", c.addrs[id]); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		c.t.Fatal(err)
	}
	c.start(id, ln)
}

// leader waits until exactly one running node says it leads and the others agree.
func (c *cluster) leader() raft.NodeID {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var lead raft.NodeID
		agree := true
		for id, s := range c.nodes {
			st, err := s.Status()
			if err != nil {
				agree = false
				break
			}
			if st.State == "leader" {
				if lead != raft.None {
					agree = false
				}
				lead = id
			}
			if st.Leader != 0 && lead != 0 && st.Leader != lead {
				agree = false
			}
		}
		if agree && lead != raft.None {
			return lead
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatal("no stable leader within 10s")
	return raft.None
}

func (c *cluster) client(id uint64) *client.Client {
	var servers []string
	for i := 1; i <= len(c.addrs); i++ {
		servers = append(servers, c.addrs[raft.NodeID(i)])
	}
	return client.New(id, servers)
}

func do(t *testing.T, cl *client.Client, op kv.Op, k string, v ...string) kv.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := cl.Do(ctx, op, k, strings.Join(v, ""))
	if err != nil {
		t.Fatalf("%s %s: %v", op, k, err)
	}
	return r
}

func TestPutGetAppend(t *testing.T) {
	c := newCluster(t, 3)
	cl := c.client(1)
	do(t, cl, kv.OpPut, "x", "1")
	do(t, cl, kv.OpAppend, "x", "2")
	if r := do(t, cl, kv.OpGet, "x"); !r.Found || r.Value != "12" {
		t.Fatalf("GET x = %+v, want 12", r)
	}
	do(t, cl, kv.OpDelete, "x")
	if r := do(t, cl, kv.OpGet, "x"); r.Found {
		t.Fatalf("GET x after DELETE = %+v", r)
	}
}

func TestLeaderCrashAndRestartFromDisk(t *testing.T) {
	c := newCluster(t, 3)
	cl := c.client(1)
	for i := 0; i < 20; i++ {
		do(t, cl, kv.OpAppend, "log", "a")
	}
	old := c.leader()
	c.crash(old)
	for i := 0; i < 20; i++ {
		do(t, cl, kv.OpAppend, "log", "b")
	}
	if c.leader() == old {
		t.Fatal("the crashed node still leads")
	}
	c.restart(old)
	want := strings.Repeat("a", 20) + strings.Repeat("b", 20)
	if r := do(t, cl, kv.OpGet, "log"); r.Value != want {
		t.Fatalf("GET log = %q, want %q", r.Value, want)
	}
	// The restarted node catches up from its own disk plus what the leader sends it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		lead, _ := c.nodes[c.leader()].Status()
		st, _ := c.nodes[old].Status()
		if st.Applied == lead.Applied && st.Applied > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarted node applied %d entries, leader %d", st.Applied, lead.Applied)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWholeClusterRestart(t *testing.T) {
	c := newCluster(t, 3)
	cl := c.client(1)
	do(t, cl, kv.OpPut, "k", "kept")
	for id := range c.addrs {
		c.crash(id)
	}
	for id := range c.addrs {
		c.restart(id)
	}
	if r := do(t, c.client(2), kv.OpGet, "k"); r.Value != "kept" {
		t.Fatalf("after a full restart GET k = %+v", r)
	}
}

func TestNoProgressWithoutMajority(t *testing.T) {
	c := newCluster(t, 3)
	cl := c.client(1)
	do(t, cl, kv.OpPut, "k", "1")
	lead := c.leader()
	for id := range c.addrs {
		if id != lead {
			c.crash(id)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := cl.Do(ctx, kv.OpPut, "k", "2"); err == nil {
		t.Fatal("a write succeeded with two of three servers down")
	}
}

func TestRetryIsAppliedOnce(t *testing.T) {
	c := newCluster(t, 3)
	lead := c.addrs[c.leader()]
	body := `{"client":42,"seq":1,"op":"APPEND","key":"d","value":"x"}`
	for i := 0; i < 3; i++ { // the same request three times, as a client retrying would
		resp, err := http.Post("http://"+lead+"/kv", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d: status %d", i, resp.StatusCode)
		}
	}
	if r := do(t, c.client(1), kv.OpGet, "d"); r.Value != "x" {
		t.Fatalf("GET d = %q, want x (applied once)", r.Value)
	}
}

func TestShortcutsRedirectToLeader(t *testing.T) {
	c := newCluster(t, 3)
	lead := c.leader()
	follower := lead%3 + 1
	req, _ := http.NewRequest(http.MethodPut, "http://"+c.addrs[follower]+"/kv/hello", strings.NewReader("world"))
	resp, err := http.DefaultClient.Do(req) // follows the 307 to the leader, body included
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT via follower: status %d", resp.StatusCode)
	}
	resp, err = http.Get("http://" + c.addrs[follower] + "/kv/hello")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"value":"world"`) {
		t.Fatalf("GET via follower: %s", b)
	}
}

func TestParseAddrs(t *testing.T) {
	got, err := server.ParseAddrs("1=127.0.0.1:7001, 2=127.0.0.1:7002")
	if err != nil || len(got) != 2 || got[2] != "127.0.0.1:7002" {
		t.Fatalf("ParseAddrs = %v, %v", got, err)
	}
	for _, bad := range []string{"", "1", "0=a:1", "x=a:1", "1=a:1,1=b:2"} {
		if _, err := server.ParseAddrs(bad); err == nil {
			t.Errorf("ParseAddrs(%q) should fail", bad)
		}
	}
}
