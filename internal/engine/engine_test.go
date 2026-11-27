package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sulthonuladib/e7-refresh/internal/config"
)

// An unlimited budget (0) must never stop a run by itself: only a stop
// request, an error or a positive budget ends it.
func TestOverBudget(t *testing.T) {
	cases := []struct {
		budget   int
		refresh  int
		expected bool
	}{
		{0, 0, false},
		{0, 29, false},
		{0, 100000, false},
		{3, 0, false},
		{3, 1, true},
		{300, 99, false},
		{300, 100, true},
	}
	for _, c := range cases {
		e := New(nil)
		e.stats.Running = true
		e.stats.RefreshCount = c.refresh
		got := e.overBudget(config.Config{Budget: c.budget})
		if got != c.expected {
			t.Errorf("budget %d after %d refreshes: overBudget = %v, want %v",
				c.budget, c.refresh, got, c.expected)
		}
	}
}

// The summary has to say who ended the run, so a stop never looks like the
// engine giving up on its own.
func TestFinishRecordsStopReason(t *testing.T) {
	e := New(nil)
	e.stats.Running = true
	e.stats.RefreshCount = 29
	if err := e.StopFrom("F10"); err != nil {
		t.Fatal(err)
	}
	e.finish(nil)
	status := e.Status()
	if !strings.Contains(status.LastResult, "stopped by F10") {
		t.Fatalf("summary %q does not name the stop key", status.LastResult)
	}
	if status.LastError != "" {
		t.Fatalf("a requested stop is not an error, got %q", status.LastError)
	}
	if status.Spent != 87 {
		t.Fatalf("spent = %d, want 3 skystones per refresh (87)", status.Spent)
	}
}

func TestFinishRecordsBudget(t *testing.T) {
	e := New(nil)
	e.stats.Running = true
	e.stats.RefreshCount = 1
	e.budgetReached = true
	e.finish(nil)
	if got := e.Status().LastResult; !strings.Contains(got, "budget reached") {
		t.Fatalf("summary %q does not mention the budget", got)
	}
}

// The history row used to be written before FinishedAt existed, which printed
// a -2562047h duration. It is written from a defer inside the run, so an open
// run has to get an end time from writeHistory itself.
func TestHistoryDurationIsPositive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	e := New(nil)
	e.stats.Running = true
	e.stats.StartedAt = time.Now().Add(-90 * time.Second)
	e.stats.RefreshCount = 3
	e.stats.Counts = map[string]int{"cov": 2}
	e.writeHistory("mouse", config.Config{Items: map[string]bool{"cov": true}})

	files, err := filepath.Glob(filepath.Join(config.HistoryDir(), "*.csv"))
	if err != nil || len(files) != 1 {
		t.Fatalf("history files = %v (%v), want one", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 2 {
		t.Fatalf("history has no row: %q", string(raw))
	}
	fields := strings.Split(lines[len(lines)-1], ",")
	if len(fields) < 2 || strings.HasPrefix(fields[1], "-") {
		t.Fatalf("duration %q in %q is negative", fields[1], lines[len(lines)-1])
	}
	if fields[1] != "1m30s" {
		t.Fatalf("duration = %q, want 1m30s", fields[1])
	}
}
