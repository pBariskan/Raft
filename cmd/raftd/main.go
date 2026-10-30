// Command raftd runs one node of a real-mode cluster.
//
//	raftd -id 1 -cluster 1=127.0.0.1:7001,2=127.0.0.1:7002,3=127.0.0.1:7003
//
// Every node gets the same -cluster list. Try it by hand with curl:
//
//	curl -L -X PUT 127.0.0.1:7001/kv/x -d hello
//	curl -L 127.0.0.1:7002/kv/x
//	curl 127.0.0.1:7003/status
//
// cmd/cluster starts a whole cluster on one machine.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pBariskan/Raft/raft"
	"github.com/pBariskan/Raft/server"
)

func main() {
	id := flag.Uint64("id", 0, "this node's ID (must appear in -cluster)")
	cluster := flag.String("cluster", "", "every node as id=host:port, comma separated")
	dir := flag.String("dir", "", "data directory (default data/node<id>)")
	tick := flag.Duration("tick", 10*time.Millisecond, "length of one Raft tick")
	election := flag.Int("election", 10, "election timeout in ticks (randomized up to twice this)")
	noSync := flag.Bool("nosync", false, "skip fsync (unsafe: for measuring only)")
	flag.Parse()

	addrs, err := server.ParseAddrs(*cluster)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}
	if *dir == "" {
		*dir = fmt.Sprintf("data/node%d", *id)
	}
	log.SetPrefix(fmt.Sprintf("[node %d] ", *id))
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	s, err := server.Start(server.Config{
		ID: raft.NodeID(*id), Addrs: addrs, Dir: *dir,
		Tick: *tick, ElectionTick: *election, NoSync: *noSync,
	})
	if err != nil {
		log.Fatal(err)
	}
	go reportChanges(s)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		s.Stop()
	case <-s.Done():
		log.Fatal(s.Err())
	}
}

// reportChanges logs every change of role, term or leader, so a terminal shows elections happen.
func reportChanges(s *server.Server) {
	var last server.Status
	for range time.Tick(20 * time.Millisecond) {
		st, err := s.Status()
		if err != nil {
			return
		}
		if st.State != last.State || st.Term != last.Term || st.Leader != last.Leader {
			log.Printf("%s in term %d (leader: %d)", st.State, st.Term, st.Leader)
		}
		last = st
	}
}
