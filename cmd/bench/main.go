// Command bench measures a real-mode cluster on this machine: throughput and latency for 3 and 5
// nodes at several client counts, what fsync costs, and how long writes stall when the leader
// crashes. Nodes run in this process but talk over real loopback HTTP and write real files.
//
//	go run ./cmd/bench -json web/data/bench.json
//
// The numbers describe the machine they ran on; the JSON records which.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pBariskan/Raft/client"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/server"
)

type Throughput struct {
	Nodes     int     `json:"nodes"`
	Clients   int     `json:"clients"`
	Fsync     bool    `json:"fsync"`
	Ops       int     `json:"ops"`
	OpsPerSec float64 `json:"ops_per_sec"`
	P50ms     float64 `json:"p50_ms"`
	P90ms     float64 `json:"p90_ms"`
	P99ms     float64 `json:"p99_ms"`
	MaxMs     float64 `json:"max_ms"`
}

type Failover struct {
	Nodes    int       `json:"nodes"`
	StallMs  []float64 `json:"stall_ms"` // per trial: leader killed → next write answered
	MedianMs float64   `json:"median_ms"`
	MinMs    float64   `json:"min_ms"`
	MaxMs    float64   `json:"max_ms"`
}

type Report struct {
	GeneratedAt string `json:"generated_at"`
	Machine     struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
		CPUs int    `json:"cpus"`
		Go   string `json:"go"`
		Note string `json:"note,omitempty"`
	} `json:"machine"`
	Setup struct {
		TickMs        float64 `json:"tick_ms"`
		ElectionTicks int     `json:"election_ticks"` // the timeout is random in [this, 2×this) ticks
		Op            string  `json:"op"`
		ValueBytes    int     `json:"value_bytes"`
		Seconds       float64 `json:"seconds_per_run"`
	} `json:"setup"`
	Throughput []Throughput `json:"throughput"`
	Failover   []Failover   `json:"failover"`
}

const (
	tick          = 10 * time.Millisecond
	electionTicks = 10
	valueBytes    = 16
)

func main() {
	out := flag.String("json", "", "write the report here")
	secs := flag.Float64("seconds", 3, "measured seconds per throughput run")
	trials := flag.Int("trials", 7, "leader crashes per failover measurement")
	note := flag.String("note", "", "describe the machine (stored in the report)")
	quick := flag.Bool("quick", false, "fewer runs, for trying it out")
	flag.Parse()
	log.SetFlags(0)

	var rep Report
	rep.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	rep.Machine.OS, rep.Machine.Arch, rep.Machine.CPUs, rep.Machine.Go = runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version()
	rep.Machine.Note = *note
	rep.Setup.TickMs = float64(tick) / float64(time.Millisecond)
	rep.Setup.ElectionTicks = electionTicks
	rep.Setup.Op, rep.Setup.ValueBytes, rep.Setup.Seconds = "PUT", valueBytes, *secs

	clientCounts := []int{1, 8, 32, 128}
	sizes := []int{3, 5}
	if *quick {
		clientCounts, sizes, *trials = []int{1, 16}, []int{3}, 3
	}
	dur := time.Duration(*secs * float64(time.Second))
	for _, n := range sizes {
		for _, c := range clientCounts {
			rep.Throughput = append(rep.Throughput, throughput(n, c, true, dur))
		}
	}
	for _, c := range []int{1, 32} {
		rep.Throughput = append(rep.Throughput, throughput(3, c, false, dur))
	}
	for _, n := range sizes {
		rep.Failover = append(rep.Failover, failover(n, *trials))
	}

	if *out != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *out)
	}
}

type cluster struct {
	servers []string
	nodes   map[raft.NodeID]*server.Server
	dir     string
}

func startCluster(n int, fsync bool) *cluster {
	dir, err := os.MkdirTemp("", "raft-bench-")
	if err != nil {
		log.Fatal(err)
	}
	c := &cluster{nodes: map[raft.NodeID]*server.Server{}, dir: dir}
	addrs := map[raft.NodeID]string{}
	lns := map[raft.NodeID]net.Listener{}
	for i := 1; i <= n; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
		lns[raft.NodeID(i)], addrs[raft.NodeID(i)] = ln, ln.Addr().String()
		c.servers = append(c.servers, ln.Addr().String())
	}
	for id, ln := range lns {
		s, err := server.Start(server.Config{
			ID: id, Addrs: addrs, Dir: fmt.Sprintf("%s/node%d", dir, id),
			Tick: tick, ElectionTick: electionTicks, NoSync: !fsync,
			Listener: ln, Logf: func(string, ...any) {},
		})
		if err != nil {
			log.Fatal(err)
		}
		c.nodes[id] = s
	}
	return c
}

func (c *cluster) stop() {
	for _, s := range c.nodes {
		s.Stop()
	}
	os.RemoveAll(c.dir)
}

func (c *cluster) leader() raft.NodeID {
	for {
		for id, s := range c.nodes {
			if st, err := s.Status(); err == nil && st.State == "leader" {
				return id
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func value(r *rand.Rand) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, valueBytes)
	for i := range b {
		b[i] = letters[r.IntN(len(letters))]
	}
	return string(b)
}

func throughput(nodes, clients int, fsync bool, dur time.Duration) Throughput {
	c := startCluster(nodes, fsync)
	defer c.stop()
	c.leader()
	warm := client.New(1_000_000, c.servers)
	warm.Do(context.Background(), kv.OpPut, "warmup", "x")

	var (
		mu        sync.Mutex
		latencies []time.Duration
		measuring atomic.Bool
		wg        sync.WaitGroup
	)
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl := client.New(uint64(i+1), c.servers)
			r := rand.New(rand.NewPCG(uint64(i), 1))
			var mine []time.Duration
			for ctx.Err() == nil {
				began := time.Now()
				if _, err := cl.Do(ctx, kv.OpPut, fmt.Sprintf("k%d", r.IntN(1000)), value(r)); err != nil {
					break
				}
				if measuring.Load() {
					mine = append(mine, time.Since(began))
				}
			}
			mu.Lock()
			latencies = append(latencies, mine...)
			mu.Unlock()
		}(i)
	}
	time.Sleep(300 * time.Millisecond) // let connections and batching settle
	measuring.Store(true)
	time.Sleep(dur)
	measuring.Store(false)
	cancel()
	wg.Wait()

	slices.Sort(latencies)
	t := Throughput{Nodes: nodes, Clients: clients, Fsync: fsync, Ops: len(latencies)}
	t.OpsPerSec = round(float64(len(latencies)) / dur.Seconds())
	t.P50ms, t.P90ms, t.P99ms = pct(latencies, 0.50), pct(latencies, 0.90), pct(latencies, 0.99)
	if len(latencies) > 0 {
		t.MaxMs = ms(latencies[len(latencies)-1])
	}
	log.Printf("%d nodes, %3d clients, fsync %-5v  %8.0f ops/s   p50 %6.2fms  p90 %6.2fms  p99 %6.2fms",
		nodes, clients, fsync, t.OpsPerSec, t.P50ms, t.P90ms, t.P99ms)
	return t
}

// failover kills the leader while one client writes in a loop, and measures how long the client
// waits from the kill until a write is answered again: the time to notice the leader is gone, hold
// an election, and for the client to find the new leader.
func failover(nodes, trials int) Failover {
	f := Failover{Nodes: nodes}
	for trial := 0; trial < trials; trial++ {
		c := startCluster(nodes, true)
		c.leader()
		cl := client.New(1, c.servers)
		ctx := context.Background()
		for i := 0; i < 20; i++ { // the client now knows the leader
			cl.Do(ctx, kv.OpPut, "k", "v")
		}
		lead := c.leader()
		killed := time.Now()
		c.nodes[lead].Stop()
		delete(c.nodes, lead)
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := cl.Do(wctx, kv.OpPut, "k", "after")
		cancel()
		if err != nil {
			log.Fatalf("failover trial %d: no write within 10s: %v", trial, err)
		}
		f.StallMs = append(f.StallMs, ms(time.Since(killed)))
		c.stop()
	}
	s := slices.Clone(f.StallMs)
	slices.Sort(s)
	f.MinMs, f.MaxMs, f.MedianMs = s[0], s[len(s)-1], s[len(s)/2]
	parts := make([]string, len(f.StallMs))
	for i, v := range f.StallMs {
		parts[i] = fmt.Sprintf("%.0f", v)
	}
	log.Printf("%d nodes, leader crash: writes stalled %s ms (median %.0f)", nodes, strings.Join(parts, ", "), f.MedianMs)
	return f
}

func pct(d []time.Duration, p float64) float64 {
	if len(d) == 0 {
		return 0
	}
	return ms(d[min(len(d)-1, int(p*float64(len(d))))])
}

func ms(d time.Duration) float64 { return round(float64(d) / float64(time.Millisecond)) }

func round(x float64) float64 {
	if x >= 100 {
		return float64(int64(x + 0.5))
	}
	return float64(int64(x*100+0.5)) / 100
}
