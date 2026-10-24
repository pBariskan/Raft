// Package sim is a deterministic simulator for a Raft cluster: a virtual clock, a simulated
// network (latency, loss, reordering, partitions) and crash/restart with a simulated disk.
// One seed produces one exact, replayable run.
package sim
