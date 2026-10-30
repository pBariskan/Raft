package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pBariskan/Raft/raft"
)

func ent(term, idx uint64, data string) raft.Entry {
	e := raft.Entry{Term: term, Index: idx}
	if data != "" {
		e.Data = []byte(data)
	}
	return e
}

func reopen(t *testing.T, dir string) (*Disk, raft.HardState, []raft.Entry) {
	t.Helper()
	d, hs, es, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, hs, es
}

func TestFreshDirectoryIsEmpty(t *testing.T) {
	_, hs, es := reopen(t, t.TempDir())
	if hs != (raft.HardState{}) || len(es) != 0 {
		t.Fatalf("got %+v %+v", hs, es)
	}
}

func TestSaveAndReopen(t *testing.T) {
	dir := t.TempDir()
	d, _, _ := reopen(t, dir)
	hs := raft.HardState{Term: 3, Vote: 2, Commit: 2}
	es := []raft.Entry{ent(1, 1, ""), ent(2, 2, "a"), ent(3, 3, "b")}
	if err := d.Save(&hs, es); err != nil {
		t.Fatal(err)
	}
	d.Close()
	_, hs2, es2 := reopen(t, dir)
	if hs2 != hs || !reflect.DeepEqual(es2, es) {
		t.Fatalf("reopened %+v %+v", hs2, es2)
	}
}

func TestReplacingEntriesTruncatesTheTail(t *testing.T) {
	dir := t.TempDir()
	d, _, _ := reopen(t, dir)
	d.Save(nil, []raft.Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(1, 3, "c"), ent(1, 4, "d")})
	d.Save(nil, []raft.Entry{ent(2, 3, "x")})
	d.Save(nil, []raft.Entry{ent(2, 4, "y")})
	d.Close()
	_, _, es := reopen(t, dir)
	want := []raft.Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(2, 3, "x"), ent(2, 4, "y")}
	if !reflect.DeepEqual(es, want) {
		t.Fatalf("got %+v, want %+v", es, want)
	}
}

func TestTornTailIsCutOff(t *testing.T) {
	dir := t.TempDir()
	d, _, _ := reopen(t, dir)
	d.Save(&raft.HardState{Term: 1, Commit: 2}, []raft.Entry{ent(1, 1, "a"), ent(1, 2, "b")})
	d.Close()
	// Simulate a crash in the middle of writing a third record.
	f, _ := os.OpenFile(filepath.Join(dir, walName), os.O_APPEND|os.O_WRONLY, 0)
	f.Write([]byte{40, 0, 0, 0, 1, 2, 3, 4, '{', '"'})
	f.Close()
	d2, hs, es := reopen(t, dir)
	if len(es) != 2 || hs.Commit != 2 {
		t.Fatalf("after torn write: %d entries, %+v", len(es), hs)
	}
	// And the file is usable again.
	if err := d2.Save(nil, []raft.Entry{ent(1, 3, "c")}); err != nil {
		t.Fatal(err)
	}
	d2.Close()
	if _, _, es := reopen(t, dir); len(es) != 3 {
		t.Fatalf("after repair and append: %d entries", len(es))
	}
}

func TestCorruptRecordStopsTheLog(t *testing.T) {
	dir := t.TempDir()
	d, _, _ := reopen(t, dir)
	d.Save(nil, []raft.Entry{ent(1, 1, "a"), ent(1, 2, "b"), ent(1, 3, "c")})
	d.Close()
	b, _ := os.ReadFile(filepath.Join(dir, walName))
	b[len(b)-3] ^= 0xff // flip bits inside the last record
	os.WriteFile(filepath.Join(dir, walName), b, 0o644)
	if _, _, es := reopen(t, dir); len(es) != 2 {
		t.Fatalf("got %d entries, want the 2 intact ones", len(es))
	}
}

func TestCommitIsCappedByTheLogOnDisk(t *testing.T) {
	dir := t.TempDir()
	d, _, _ := reopen(t, dir)
	d.Save(&raft.HardState{Term: 1, Commit: 5}, []raft.Entry{ent(1, 1, "a")})
	d.Close()
	if _, hs, _ := reopen(t, dir); hs.Commit != 1 {
		t.Fatalf("commit = %d, want 1", hs.Commit)
	}
}
