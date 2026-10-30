package transport

import (
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/pBariskan/Raft/raft"
)

func TestCoalesceKeepsLastAppendAndEverythingElse(t *testing.T) {
	q := []raft.Message{
		{Type: raft.MsgApp, To: 2, Index: 1},
		{Type: raft.MsgVoteResp, To: 2},
		{Type: raft.MsgApp, To: 2, Index: 3},
		{Type: raft.MsgAppResp, To: 2},
	}
	got := coalesce(q)
	want := []raft.Message{q[1], q[2], q[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coalesce = %+v, want %+v", got, want)
	}
}

func TestMessagesArrive(t *testing.T) {
	var mu sync.Mutex
	var got []raft.Message
	srv := httptest.NewServer(Handler(func(b []raft.Message) {
		mu.Lock()
		got = append(got, b...)
		mu.Unlock()
	}))
	defer srv.Close()

	tr := New(1, map[raft.NodeID]string{1: "http://unused", 2: srv.URL})
	defer tr.Stop()
	sent := raft.Message{Type: raft.MsgApp, From: 1, To: 2, Term: 3, Entries: []raft.Entry{{Term: 3, Index: 1, Data: []byte("x")}}}
	tr.Send([]raft.Message{sent, {Type: raft.MsgVote, From: 1, To: 9}}) // 9 is unknown: ignored

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !reflect.DeepEqual(got[0], sent) {
		t.Fatalf("received %+v, want [%+v]", got, sent)
	}
}

func TestDeadPeerDoesNotBlockSend(t *testing.T) {
	tr := New(1, map[raft.NodeID]string{2: "http://127.0.0.1:1"}) // nothing listens there
	defer tr.Stop()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5*maxQueue; i++ {
			tr.Send([]raft.Message{{Type: raft.MsgVote, From: 1, To: 2}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked on a dead peer")
	}
}
