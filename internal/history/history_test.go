package history

import (
	"testing"
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"
)

func TestProviderHistoryIsolation(t *testing.T) {
	dir := t.TempDir()
	claude, codex := LoadProvider(dir, usage.Claude), LoadProvider(dir, usage.Codex)
	claude.Add(usage.Usage{FiveHour: &usage.Window{Percent: 62}, UpdatedAt: time.Now()})
	codex.Add(usage.Usage{Provider: usage.Codex, FiveHour: &usage.Window{Percent: 38}, UpdatedAt: time.Now()})
	for _, tc := range []struct {
		provider usage.Provider
		want     float64
	}{{usage.Claude, 62}, {usage.Codex, 38}} {
		got := LoadProvider(dir, tc.provider).Since(time.Now().Add(-time.Hour))
		if len(got) != 1 || got[0].S != tc.want || got[0].W != -1 {
			t.Fatalf("%s history: %+v", tc.provider, got)
		}
	}
	// Existing callers and installations still read Claude's history.jsonl.
	if got := Load(dir).Since(time.Now().Add(-time.Hour)); len(got) != 1 || got[0].S != 62 {
		t.Fatalf("legacy history: %+v", got)
	}
}
