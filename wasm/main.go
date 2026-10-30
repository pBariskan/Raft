//go:build js && wasm

// Command wasm is the bridge between the browser and the simulator. Built with
// GOOS=js GOARCH=wasm, it puts one object on the page:
//
//	raftEngine.create(optionsJSON)       → {"id": n} or {"error": "…"}
//	raftEngine.do(id, commandJSON)       → {"result": …} or {"error": "…"}
//	raftEngine.snapshot(id, logTail)     → the state as JSON, and the events since the last snapshot
//	raftEngine.peek(id, logTail)         → the same, leaving the events for the next snapshot
//	raftEngine.destroy(id)
//
// Several sessions can live side by side (a page can show more than one cluster).
// Options, commands and the snapshot are play.Options, play.Command and play.Snapshot. Every
// call goes through JSON so the page never holds Go values.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/pBariskan/Raft/play"
)

var (
	sessions = map[int]*play.Session{}
	nextID   = 1
)

func main() {
	js.Global().Set("raftEngine", js.ValueOf(map[string]any{
		"create":   js.FuncOf(create),
		"do":       js.FuncOf(do),
		"snapshot": js.FuncOf(snapshot),
		"peek":     js.FuncOf(peek),
		"destroy":  js.FuncOf(destroy),
		"version":  "2",
	}))
	select {} // keep the Go side alive for callbacks
}

func reply(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func create(_ js.Value, args []js.Value) any {
	var opt play.Options
	if len(args) > 0 {
		if err := json.Unmarshal([]byte(args[0].String()), &opt); err != nil {
			return reply(map[string]any{"error": err.Error()})
		}
	}
	s, err := play.New(opt)
	if err != nil {
		return reply(map[string]any{"error": err.Error()})
	}
	id := nextID
	nextID++
	sessions[id] = s
	return reply(map[string]any{"id": id})
}

func session(args []js.Value) *play.Session {
	if len(args) == 0 {
		return nil
	}
	return sessions[args[0].Int()]
}

func do(_ js.Value, args []js.Value) any {
	s := session(args)
	if s == nil || len(args) < 2 {
		return reply(map[string]any{"error": "no such session"})
	}
	var cmd play.Command
	if err := json.Unmarshal([]byte(args[1].String()), &cmd); err != nil {
		return reply(map[string]any{"error": err.Error()})
	}
	r, err := s.Do(cmd)
	if err != nil {
		return reply(map[string]any{"error": err.Error()})
	}
	return reply(map[string]any{"result": r})
}

func snapshot(_ js.Value, args []js.Value) any {
	s := session(args)
	if s == nil {
		return "null"
	}
	tail := 0
	if len(args) > 1 {
		tail = args[1].Int()
	}
	b, _ := json.Marshal(s.Snapshot(tail))
	return string(b)
}

func peek(_ js.Value, args []js.Value) any {
	s := session(args)
	if s == nil {
		return "null"
	}
	tail := 0
	if len(args) > 1 {
		tail = args[1].Int()
	}
	b, _ := json.Marshal(s.Peek(tail))
	return string(b)
}

func destroy(_ js.Value, args []js.Value) any {
	if len(args) > 0 {
		delete(sessions, args[0].Int())
	}
	return nil
}
