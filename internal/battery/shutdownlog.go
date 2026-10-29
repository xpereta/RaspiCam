package battery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// MaxShutdownLogEntries is how many events the shutdown log keeps.
const MaxShutdownLogEntries = 10

// ShutdownLog keeps the last MaxShutdownLogEntries shutdown events as JSON
// lines in one file. It is safe for concurrent use within one process.
type ShutdownLog struct {
	path string

	// Logf reports skipped corrupt lines. NewShutdownLog sets it to
	// log.Printf; set it to nil to discard the messages.
	Logf func(format string, args ...any)

	mu sync.Mutex
}

// NewShutdownLog returns a log stored at path (for example
// "/var/lib/raspicam/shutdowns.jsonl"). The file is created on first Append.
func NewShutdownLog(path string) *ShutdownLog {
	return &ShutdownLog{path: path, Logf: log.Printf}
}

// Path returns the file path of the log.
func (l *ShutdownLog) Path() string { return l.path }

// Append adds ev, keeps the newest MaxShutdownLogEntries events and rewrites
// the file atomically (temp file, fsync, rename, fsync of the directory).
// Corrupt existing lines are skipped and reported through Logf.
func (l *ShutdownLog) Append(ev Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	events, err := l.read()
	if err != nil {
		return err
	}
	events = append(events, ev)
	if n := len(events); n > MaxShutdownLogEntries {
		events = events[n-MaxShutdownLogEntries:]
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("shutdown log: encode event: %w", err)
		}
	}
	return writeFileAtomic(l.path, buf.Bytes())
}

// List returns the logged events, newest first. A missing file returns an
// empty list and no error.
func (l *ShutdownLog) List() ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	events, err := l.read()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

// read returns the valid events in file order (oldest first).
func (l *ShutdownLog) read() ([]Event, error) {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shutdown log: read %s: %w", l.path, err)
	}
	events, skipped := parseShutdownLog(data)
	if skipped > 0 && l.Logf != nil {
		l.Logf("shutdown log: skipped %d corrupt line(s) in %s", skipped, l.path)
	}
	return events, nil
}

// parseShutdownLog decodes JSON lines, skipping blank lines. It returns the
// events in file order and the number of corrupt lines skipped.
func parseShutdownLog(data []byte) ([]Event, int) {
	var events []Event
	skipped := 0
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			skipped++
			continue
		}
		events = append(events, ev)
	}
	return events, skipped
}

// writeFileAtomic replaces path with data via path+".tmp", fsyncing the file
// before the rename and the directory after it.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("shutdown log: create %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("shutdown log: write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("shutdown log: fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("shutdown log: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("shutdown log: rename %s: %w", tmp, err)
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("shutdown log: open dir %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("shutdown log: fsync dir %s: %w", dir, err)
	}
	return nil
}
