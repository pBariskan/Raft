// Package storage persists Raft state in real mode: a write-ahead log and the hard state
// (current term and vote), synced to disk before the node replies.
package storage
