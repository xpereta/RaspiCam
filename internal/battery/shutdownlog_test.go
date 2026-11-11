package battery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLog(t *testing.T) (*ShutdownLog, *[]string) {
	t.Helper()
	l := NewShutdownLog(filepath.Join(t.TempDir(), "shutdowns.jsonl"))
	var msgs []string
	l.Logf = func(format string, args ...any) { msgs = append(msgs, fmt.Sprintf(format, args...)) }
	return l, &msgs
}

func logEvent(i int) Event {
	return Event{
		Time:       time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		Reason:     ReasonLowBattery,
		Threshold:  ReasonSOC,
		Volts:      3.3,
		SOC:        float64(i),
		HasBattery: true,
	}
}

func TestShutdownLogMissingFile(t *testing.T) {
	l, _ := newTestLog(t)
	got, err := l.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestShutdownLogKeepsLastTenNewestFirst(t *testing.T) {
	l, _ := newTestLog(t)
	for i := 0; i < 11; i++ {
		if err := l.Append(logEvent(i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	got, err := l.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != MaxShutdownLogEntries {
		t.Fatalf("len = %d, want %d", len(got), MaxShutdownLogEntries)
	}
	for i, ev := range got {
		want := logEvent(10 - i)
		if !ev.Time.Equal(want.Time) || ev.SOC != want.SOC {
			t.Errorf("event %d = %+v, want %+v", i, ev, want)
		}
	}
	if _, err := os.Stat(l.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind: %v", err)
	}
}

func TestShutdownLogSnakeCaseJSON(t *testing.T) {
	l, _ := newTestLog(t)
	ev := logEvent(1)
	ev.Client = "1.2.3.4:5"
	ev.DryRun = true
	ev.Synced = true
	if err := l.Append(ev); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"time"`, `"reason":"low_battery"`, `"threshold":"soc"`, `"volts"`, `"soc"`, `"has_battery":true`, `"synced":true`, `"client":"1.2.3.4:5"`, `"dry_run":true`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("file %s missing %s", b, key)
		}
	}
}

func TestShutdownLogSkipsCorruptLines(t *testing.T) {
	l, msgs := newTestLog(t)
	if err := l.Append(logEvent(1)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json\n\n{\"reason\":\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := l.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].SOC != 1 {
		t.Fatalf("List = %+v, want one event", got)
	}
	if len(*msgs) != 1 || !strings.Contains((*msgs)[0], "skipped 2 corrupt") {
		t.Errorf("log messages = %q", *msgs)
	}

	if err := l.Append(logEvent(2)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(b), "\n"); lines != 2 {
		t.Errorf("rewritten file has %d lines, want 2 (corrupt dropped):\n%s", lines, b)
	}
}

func TestShutdownLogAtomicRewriteReplacesStaleTmp(t *testing.T) {
	l, _ := newTestLog(t)
	if err := os.WriteFile(l.Path()+".tmp", []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(logEvent(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind: %v", err)
	}
	got, err := l.List()
	if err != nil || len(got) != 1 {
		t.Fatalf("List = %v, %v", got, err)
	}
}

func TestShutdownLogAppendErrorUnwritableDir(t *testing.T) {
	l := NewShutdownLog(filepath.Join(t.TempDir(), "missing", "shutdowns.jsonl"))
	if err := l.Append(logEvent(1)); err == nil {
		t.Fatal("Append into a missing directory succeeded")
	}
}
