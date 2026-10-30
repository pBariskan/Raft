// Package raft is a pure, deterministic implementation of the Raft consensus algorithm.
//
// It never touches the outside world: no goroutines, no clock, no I/O and no global randomness.
// The caller drives it with Tick (time passing), Step (an incoming message) and Propose (a new
// command), and receives back what to send, what to persist and which entries are committed.
// The same code therefore runs in the simulator, over a real network and in the browser.
package raft
