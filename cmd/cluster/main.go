// Command cluster runs a real-mode cluster on this machine: one raftd process per node, each with
// its own data directory, and a prompt to crash and restart them.
//
//	go run ./cmd/cluster -n 5
//
//	> status          every node's role, term and log position
//	> kill 2          SIGKILL node 2 (a crash: no clean shutdown)
//	> start 2         start node 2 again from its data directory
//	> put x hello     write through the cluster
//	> get x
//	> quit
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pBariskan/Raft/client"
	"github.com/pBariskan/Raft/kv"
	"github.com/pBariskan/Raft/server"
)

type node struct {
	id   int
	addr string
	cmd  *exec.Cmd
}

func main() {
	n := flag.Int("n", 3, "number of nodes")
	port := flag.Int("port", 7001, "first port; node i listens on port+i-1")
	dir := flag.String("dir", "data", "parent directory for the nodes' data")
	bin := flag.String("bin", "", "raftd binary (default: build ./cmd/raftd into -dir)")
	fresh := flag.Bool("fresh", false, "delete the nodes' data before starting")
	flag.Parse()

	if *fresh {
		for i := 1; i <= *n; i++ {
			os.RemoveAll(filepath.Join(*dir, fmt.Sprintf("node%d", i)))
		}
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fail(err)
	}
	if *bin == "" {
		*bin = filepath.Join(*dir, "raftd")
		build := exec.Command("go", "build", "-o", *bin, "./cmd/raftd")
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			fail(fmt.Errorf("building raftd (run from the repository root, or pass -bin): %w", err))
		}
	}

	var parts, servers []string
	nodes := map[int]*node{}
	for i := 1; i <= *n; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", *port+i-1)
		parts = append(parts, fmt.Sprintf("%d=%s", i, addr))
		servers = append(servers, addr)
		nodes[i] = &node{id: i, addr: addr}
	}
	spec := strings.Join(parts, ",")
	start := func(nd *node) {
		if nd.cmd != nil {
			fmt.Printf("node %d is already running\n", nd.id)
			return
		}
		nd.cmd = exec.Command(*bin, "-id", strconv.Itoa(nd.id), "-cluster", spec,
			"-dir", filepath.Join(*dir, fmt.Sprintf("node%d", nd.id)))
		nd.cmd.Stdout, nd.cmd.Stderr = os.Stdout, os.Stderr
		if err := nd.cmd.Start(); err != nil {
			fmt.Println(err)
			nd.cmd = nil
		}
	}
	kill := func(nd *node) {
		if nd.cmd == nil {
			fmt.Printf("node %d is not running\n", nd.id)
			return
		}
		nd.cmd.Process.Kill()
		nd.cmd.Wait()
		nd.cmd = nil
	}
	for i := 1; i <= *n; i++ {
		start(nodes[i])
	}
	defer func() {
		for _, nd := range nodes {
			if nd.cmd != nil {
				kill(nd)
			}
		}
	}()

	fmt.Printf("cluster of %d on %s\ncommands: status, kill N, start N, put K V, get K, quit\n", *n, spec)
	cl := client.New(uint64(time.Now().UnixNano()), servers) // a fresh client ID each run
	in := bufio.NewScanner(os.Stdin)
	for fmt.Print("> "); in.Scan(); fmt.Print("> ") {
		f := strings.Fields(in.Text())
		if len(f) == 0 {
			continue
		}
		pick := func() *node {
			if len(f) < 2 {
				return nil
			}
			i, _ := strconv.Atoi(f[1])
			return nodes[i]
		}
		switch f[0] {
		case "status", "s":
			for i := 1; i <= *n; i++ {
				fmt.Println(status(nodes[i]))
			}
		case "kill", "k":
			if nd := pick(); nd != nil {
				kill(nd)
			}
		case "start":
			if nd := pick(); nd != nil {
				start(nd)
			}
		case "put", "get":
			if len(f) < 2 || (f[0] == "put" && len(f) < 3) {
				fmt.Println("usage: put KEY VALUE | get KEY")
				continue
			}
			op, val := kv.OpGet, ""
			if f[0] == "put" {
				op, val = kv.OpPut, strings.Join(f[2:], " ")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			began := time.Now()
			r, err := cl.Do(ctx, op, f[1], val)
			cancel()
			switch {
			case err != nil:
				fmt.Println("no answer within 5s (is a majority up?)")
			case op == kv.OpGet && !r.Found:
				fmt.Printf("(not found)  %v\n", time.Since(began).Round(time.Microsecond))
			case op == kv.OpGet:
				fmt.Printf("%s  %v\n", r.Value, time.Since(began).Round(time.Microsecond))
			default:
				fmt.Printf("ok  %v\n", time.Since(began).Round(time.Microsecond))
			}
		case "quit", "q", "exit":
			return
		default:
			fmt.Println("commands: status, kill N, start N, put K V, get K, quit")
		}
	}
}

func status(nd *node) string {
	if nd.cmd == nil {
		return fmt.Sprintf("node %d  down", nd.id)
	}
	c := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get("http://" + nd.addr + "/status")
	if err != nil {
		return fmt.Sprintf("node %d  not answering", nd.id)
	}
	defer resp.Body.Close()
	var st server.Status
	b, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(b, &st) != nil {
		return fmt.Sprintf("node %d  bad status", nd.id)
	}
	return fmt.Sprintf("node %d  %-9s term %-4d leader %d  log %d  committed %d  applied %d",
		nd.id, st.State, st.Term, st.Leader, st.LastIndex, st.Commit, st.Applied)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
