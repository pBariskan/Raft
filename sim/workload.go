package sim

import (
	"math/rand/v2"

	"github.com/pBariskan/Raft/kv"
)

// Workload drives a set of clients with random GET/PUT/APPEND/DELETE requests on a few keys. Few keys
// means many requests touch the same key, which is where consistency bugs show.
type Workload struct {
	c       *Cluster
	rng     *rand.Rand
	clients []uint64
	keys    []string
	rate    int // a free client starts a new request with probability 1/rate per tick
	next    int // counter for unique PUT values
}

// NewWorkload adds n clients to the cluster. Its randomness comes from seed, separate from the
// network's, so the same seed gives the same requests.
func NewWorkload(c *Cluster, seed uint64, clients int, keys []string, rate int) *Workload {
	w := &Workload{c: c, rng: rand.New(rand.NewPCG(seed, 0x776b6c64)), keys: keys, rate: max(1, rate)}
	for i := 0; i < clients; i++ {
		w.clients = append(w.clients, c.NewClient())
	}
	return w
}

// Step gives each idle client a chance to start a request. Call it once per tick.
func (w *Workload) Step() {
	for _, id := range w.clients {
		if w.c.ClientBusy(id) || w.rng.IntN(w.rate) != 0 {
			continue
		}
		key := w.keys[w.rng.IntN(len(w.keys))]
		w.next++
		switch r := w.rng.IntN(20); {
		case r < 8:
			_ = w.c.Submit(id, kv.OpGet, key, "")
		case r < 12:
			_ = w.c.Submit(id, kv.OpPut, key, itoa(w.next))
		case r < 19:
			_ = w.c.Submit(id, kv.OpAppend, key, itoa(w.next)+".")
		default:
			_ = w.c.Submit(id, kv.OpDelete, key, "")
		}
	}
}

// Idle reports whether no client is waiting for an answer.
func (w *Workload) Idle() bool {
	for _, id := range w.clients {
		if w.c.ClientBusy(id) {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
