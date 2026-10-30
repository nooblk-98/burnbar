package app

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nooblk-98/burnbar/internal/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestConfigureStartup(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		legacyProfile, current bool
		legacyEntry, optOut    bool
		want                   bool
	}{
		{name: "new profile defaults on", want: true},
		{name: "legacy opt out", legacyProfile: true},
		{name: "legacy opt in", legacyProfile: true, current: true, want: true},
		{name: "renamed startup entry", legacyProfile: true, legacyEntry: true, want: true},
		{name: "explicit opt out wins", current: true, legacyEntry: true, optOut: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No test opens or modifies the real Windows Run key.
			keyPath := fmt.Sprintf(`Software\BurnBarStartupTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
			k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			t.Cleanup(func() {
				if err := registry.DeleteKey(registry.CURRENT_USER, keyPath); err != nil {
					t.Errorf("remove isolated test key: %v", err)
				}
			})
			for name, enabled := range map[string]bool{runValue: tc.current, legacyRunValue: tc.legacyEntry} {
				if enabled {
					if err := k.SetStringValue(name, `"C:\Old Folder\burnbar.exe"`); err != nil {
						t.Fatal(err)
					}
				}
			}
			cfg := config.Default()
			if tc.legacyProfile {
				cfg.StartWithWindows = nil
			}
			if tc.optOut {
				off := false
				cfg.StartWithWindows = &off
			}
			const executable = `C:\Program Files\BurnBar\burnbar.exe`
			if err := configureStartup(&cfg, keyPath, executable); err != nil {
				t.Fatal(err)
			}
			if cfg.StartWithWindows == nil || *cfg.StartWithWindows != tc.want {
				t.Fatal("resolved preference did not match the existing choice")
			}
			command, _, err := k.GetStringValue(runValue)
			if tc.want {
				if err != nil || command != `"`+executable+`"` {
					t.Fatalf("startup command=%q, error=%v", command, err)
				}
			} else if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				t.Fatalf("startup entry should be absent: %v", err)
			}
			if _, _, err := k.GetStringValue(legacyRunValue); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				t.Fatalf("legacy entry should be absent: %v", err)
			}
		})
	}
}

func TestDemoDoesNotConfigureStartup(t *testing.T) {
	a := &app{demo: true, cfg: config.Default()}
	a.cfg.StartWithWindows = nil
	if err := a.initializeStartup(); err != nil {
		t.Fatal(err)
	}
	if a.cfg.StartWithWindows != nil {
		t.Fatal("demo mode must not migrate or save the startup preference")
	}
}
