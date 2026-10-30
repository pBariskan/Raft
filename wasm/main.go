//go:build js && wasm

// Command wasm is the bridge between the browser and the simulator. Built with
// GOOS=js GOARCH=wasm, it puts one object on the page:
//
//	raftEngine.create(optionsJSON)   → "" or an error message
//	raftEngine.do(commandJSON)       → {"result": …} or {"error": "…"}
//	raftEngine.snapshot(logTail)     → the state as JSON, and the events since the last snapshot
//
// Options, commands and the snapshot are play.Options, play.Command and play.Snapshot. Every
// call goes through JSON so the page never holds Go values.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/pBariskan/Raft/play"
)

var session *play.Session

func main() {
	js.Global().Set("raftEngine", js.ValueOf(map[string]any{
		"create":   js.FuncOf(create),
		"do":       js.FuncOf(do),
		"snapshot": js.FuncOf(snapshot),
		"version":  "1",
	}))
	select {} // keep the Go side alive for callbacks
}

func create(_ js.Value, args []js.Value) any {
	var opt play.Options
	if len(args) > 0 {
		if err := json.Unmarshal([]byte(args[0].String()), &opt); err != nil {
			return err.Error()
		}
	}
	s, err := play.New(opt)
	if err != nil {
		return err.Error()
	}
	session = s
	return ""
}

func do(_ js.Value, args []js.Value) any {
	out := map[string]any{}
	var cmd play.Command
	switch {
	case session == nil:
		out["error"] = "call create first"
	case len(args) == 0:
		out["error"] = "missing command"
	default:
		if err := json.Unmarshal([]byte(args[0].String()), &cmd); err != nil {
			out["error"] = err.Error()
		} else if r, err := session.Do(cmd); err != nil {
			out["error"] = err.Error()
		} else {
			out["result"] = r
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func snapshot(_ js.Value, args []js.Value) any {
	if session == nil {
		return "null"
	}
	tail := 0
	if len(args) > 0 {
		tail = args[0].Int()
	}
	b, _ := json.Marshal(session.Snapshot(tail))
	return string(b)
}
