// Package kv is the replicated key-value state machine (PUT, GET, DELETE) that runs on top of
// Raft, with client sessions so a retried request is applied only once.
package kv
