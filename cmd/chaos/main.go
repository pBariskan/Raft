// Command chaos runs many seeded chaos scenarios against the simulated cluster and reports any
// safety or linearizability violation with the seed that reproduces it.
//
//	go run ./cmd/chaos -seeds 1000
//	go run ./cmd/chaos -mistake commit-old-terms -seeds 2000 -stop
//	go run ./cmd/chaos -from 4242 -seeds 1 -v      # replay one seed
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pBariskan/Raft/sim"
)

// Summary is the JSON report.
type Summary struct {
	Mistake    string            `json:"mistake"`
	Seeds      int               `json:"seeds"`
	FirstSeed  uint64            `json:"first_seed"`
	Ticks      int               `json:"ticks"`
	Crashes    int               `json:"crashes"`
	Partitions int               `json:"partitions"`
	Leaders    int               `json:"leaders"`
	Requests   int               `json:"requests"`
	Answered   int               `json:"answered"`
	Committed  int               `json:"committed"`
	Duplicates int               `json:"duplicates"`
	Failed     int               `json:"failed_seeds"`
	Failures   []sim.ChaosResult `json:"failures"` // the first -keep of them
	ByProperty map[string]int    `json:"failures_by_property"`
	Seconds    float64           `json:"seconds"`
	GoVersion  string            `json:"go_version"`
}

func main() {
	seeds := flag.Int("seeds", 300, "number of seeds to run")
	from := flag.Uint64("from", 1, "first seed")
	ticks := flag.Int("ticks", 3000, "ticks of chaos per seed")
	mistake := flag.String("mistake", "none", "classic mistake to switch on: "+names())
	stop := flag.Bool("stop", false, "stop at the first failing seed")
	out := flag.String("json", "", "write a JSON summary to this file")
	verbose := flag.Bool("v", false, "print every seed's result")
	keep := flag.Int("keep", -1, "failing seeds to keep in the JSON report (-1: all)")
	flag.Parse()

	apply, ok := sim.MistakeSwitches[*mistake]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown mistake %q; choose one of: %s\n", *mistake, names())
		os.Exit(2)
	}
	var m sim.Mistakes
	apply(&m)

	start := time.Now()
	results := make([]sim.ChaosResult, *seeds)
	var mu sync.Mutex
	failed := false
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				r := sim.RunChaos(sim.ChaosConfig{Seed: *from + uint64(i), Ticks: *ticks, Mistakes: m})
				mu.Lock()
				results[i] = r
				if r.Failed() {
					failed = true
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < *seeds; i++ {
		mu.Lock()
		f := failed
		mu.Unlock()
		if f && *stop {
			break
		}
		next <- i
	}
	close(next)
	wg.Wait()

	sum := Summary{Mistake: *mistake, FirstSeed: *from, ByProperty: map[string]int{}, GoVersion: runtime.Version()}
	for _, r := range results {
		if r.Seed == 0 {
			continue // not run (stopped early)
		}
		sum.Seeds++
		sum.Ticks += r.Ticks
		sum.Crashes += r.Crashes
		sum.Partitions += r.Partitions
		sum.Leaders += r.Leaders
		sum.Requests += r.Requests
		sum.Answered += r.Answered
		sum.Committed += r.Committed
		sum.Duplicates += r.Duplicates
		if r.Failed() {
			sum.Failures = append(sum.Failures, r)
			sum.ByProperty[r.Violation]++
		}
		if *verbose {
			fmt.Println(r)
		}
	}
	sort.Slice(sum.Failures, func(i, j int) bool { return sum.Failures[i].Seed < sum.Failures[j].Seed })
	sum.Seconds = time.Since(start).Seconds()

	fmt.Printf("%d seeds (%d..%d), mistake %q: %d ticks, %d crashes, %d partitions, %d leaders, %d/%d requests answered, %d entries committed, %d duplicates skipped, %.1fs\n",
		sum.Seeds, *from, *from+uint64(sum.Seeds)-1, *mistake, sum.Ticks, sum.Crashes, sum.Partitions, sum.Leaders,
		sum.Answered, sum.Requests, sum.Committed, sum.Duplicates, sum.Seconds)
	if len(sum.Failures) == 0 {
		fmt.Println("no violations")
	} else {
		fmt.Printf("%d failing seeds: %v\n", len(sum.Failures), sum.ByProperty)
		f := sum.Failures[0]
		fmt.Printf("first: %s\nreplay: go run ./cmd/chaos -mistake %s -from %d -seeds 1 -v\n", f, *mistake, f.Seed)
	}
	sum.Failed = len(sum.Failures)
	if *out != "" {
		if *keep >= 0 && len(sum.Failures) > *keep {
			sum.Failures = sum.Failures[:*keep]
		}
		b, _ := json.MarshalIndent(sum, "", "  ")
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if sum.Failed > 0 && *mistake == "none" {
		os.Exit(1)
	}
}

func names() string {
	var n []string
	for k := range sim.MistakeSwitches {
		n = append(n, k)
	}
	sort.Strings(n)
	return strings.Join(n, ", ")
}
