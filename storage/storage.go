// Package storage persists a Raft node's hard state and log on disk in real mode.
//
// The log is a write-ahead file of length-prefixed, checksummed records, one per entry. Replacing
// entries (when a follower's log is repaired) truncates the file at the first replaced entry and
// appends. A crash can leave a half-written record at the end; Open detects it by its checksum
// and cuts it off. The hard state is written to a temporary file, synced and renamed into place,
// so it is always either the old or the new version.
package storage

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"

	"github.com/pBariskan/Raft/raft"
)

const (
	walName   = "wal"
	stateName = "hardstate.json"
)

// Disk is one node's storage directory. It is not safe for concurrent use.
type Disk struct {
	dir     string
	wal     *os.File
	offsets []int64 // offsets[i] is where the record for entry i+1 starts
	size    int64
	hs      raft.HardState
	noSync  bool
}

// Options tune a Disk.
type Options struct {
	// NoSync skips fsync. Only for tests and for measuring what fsync costs; a node running
	// like this can lose acknowledged data in a power failure.
	NoSync bool
}

// Open opens (or creates) the storage in dir and returns it with the saved hard state and log.
func Open(dir string, opt Options) (*Disk, raft.HardState, []raft.Entry, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, raft.HardState{}, nil, err
	}
	d := &Disk{dir: dir, noSync: opt.NoSync}
	if b, err := os.ReadFile(filepath.Join(dir, stateName)); err == nil {
		if err := json.Unmarshal(b, &d.hs); err != nil {
			return nil, raft.HardState{}, nil, fmt.Errorf("storage: bad hard state: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, raft.HardState{}, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, walName), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, raft.HardState{}, nil, err
	}
	d.wal = f
	entries, err := d.load()
	if err != nil {
		f.Close()
		return nil, raft.HardState{}, nil, err
	}
	if d.hs.Commit > uint64(len(entries)) {
		// The hard state was synced but the log tail it points to was not: never acknowledged
		// as stored, so only the commit hint is lost; the leader will resend.
		d.hs.Commit = uint64(len(entries))
	}
	return d, d.hs, entries, nil
}

// load reads every intact record and cuts off a torn tail.
func (d *Disk) load() ([]raft.Entry, error) {
	if _, err := d.wal.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(d.wal)
	var entries []raft.Entry
	var off int64
	for {
		var head [8]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			break // clean end, or a torn header
		}
		n := binary.LittleEndian.Uint32(head[:4])
		sum := binary.LittleEndian.Uint32(head[4:])
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil || crc32.ChecksumIEEE(body) != sum {
			break // torn or corrupt record: everything from here on was never synced
		}
		var e raft.Entry
		if err := json.Unmarshal(body, &e); err != nil || e.Index != uint64(len(entries)+1) {
			break
		}
		d.offsets = append(d.offsets, off)
		off += int64(8 + n)
		entries = append(entries, e)
	}
	d.size = off
	if err := d.wal.Truncate(off); err != nil {
		return nil, err
	}
	_, err := d.wal.Seek(off, io.SeekStart)
	return entries, err
}

// Save writes what a raft.Ready asks to persist and syncs it. It must return before the Ready's
// messages are sent.
func (d *Disk) Save(hs *raft.HardState, entries []raft.Entry) error {
	if len(entries) > 0 {
		first := entries[0].Index
		if first > uint64(len(d.offsets))+1 {
			return fmt.Errorf("storage: gap: have %d entries, asked to write from %d", len(d.offsets), first)
		}
		if first <= uint64(len(d.offsets)) {
			d.size = d.offsets[first-1]
			d.offsets = d.offsets[:first-1]
			if err := d.wal.Truncate(d.size); err != nil {
				return err
			}
			if _, err := d.wal.Seek(d.size, io.SeekStart); err != nil {
				return err
			}
		}
		var buf []byte
		for _, e := range entries {
			body, err := json.Marshal(e)
			if err != nil {
				return err
			}
			var head [8]byte
			binary.LittleEndian.PutUint32(head[:4], uint32(len(body)))
			binary.LittleEndian.PutUint32(head[4:], crc32.ChecksumIEEE(body))
			d.offsets = append(d.offsets, d.size+int64(len(buf)))
			buf = append(append(buf, head[:]...), body...)
		}
		if _, err := d.wal.Write(buf); err != nil {
			return err
		}
		d.size += int64(len(buf))
		if !d.noSync {
			if err := d.wal.Sync(); err != nil {
				return err
			}
		}
	}
	if hs != nil && *hs != d.hs {
		if err := d.writeHardState(*hs); err != nil {
			return err
		}
		d.hs = *hs
	}
	return nil
}

func (d *Disk) writeHardState(hs raft.HardState) error {
	b, _ := json.Marshal(hs)
	tmp := filepath.Join(d.dir, stateName+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if !d.noSync {
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(d.dir, stateName)); err != nil {
		return err
	}
	if d.noSync {
		return nil
	}
	dir, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync() // make the rename itself durable
}

// Close closes the log file.
func (d *Disk) Close() error { return d.wal.Close() }
