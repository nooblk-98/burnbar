package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStartupPreference(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		want           *bool
	}{
		{name: "new profile defaults on", want: boolValue(true)},
		{name: "legacy profile needs registry migration", contents: `{ "providers": "claude" }`},
		{name: "explicit opt out", contents: `{ "startWithWindows": false }`, want: boolValue(false)},
		{name: "explicit opt in", contents: `{ "startWithWindows": true }`, want: boolValue(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APPDATA", t.TempDir())
			if tc.contents != "" {
				if err := os.WriteFile(filepath.Join(Dir(), "config.json"), []byte(tc.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := Load().StartWithWindows
			if tc.want == nil {
				if got != nil {
					t.Fatal("legacy preference must be resolved from the registry")
				}
			} else if got == nil || *got != *tc.want {
				t.Fatalf("startup preference=%v, want %v", got, *tc.want)
			}
		})
	}
}

func TestStartupOptOutSurvivesSaveAndReload(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfg := Default()
	cfg.StartWithWindows = boolValue(false)
	cfg.Save()
	got := Load()
	if got.StartWithWindows == nil || *got.StartWithWindows {
		t.Fatal("saving and loading must not re-enable startup")
	}
}

func boolValue(v bool) *bool { return &v }
