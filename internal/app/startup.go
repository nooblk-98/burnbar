package app

import (
	"errors"
	"os"

	"github.com/nooblk-98/burnbar/internal/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func startupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

func writeStartup(k registry.Key, on bool, exe string) error {
	if on {
		return k.SetStringValue(runValue, `"`+exe+`"`)
	}
	err := k.DeleteValue(runValue)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	return err
}

func setStartup(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return writeStartup(k, on, exe)
}

// configureStartup migrates the legacy entry and preserves an existing preference.
func configureStartup(cfg *config.Config, keyPath, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_, _, currentErr := k.GetStringValue(runValue)
	if currentErr != nil && !errors.Is(currentErr, windows.ERROR_FILE_NOT_FOUND) {
		return currentErr
	}
	_, _, legacyErr := k.GetStringValue(legacyRunValue)
	if legacyErr != nil && !errors.Is(legacyErr, windows.ERROR_FILE_NOT_FOUND) {
		return legacyErr
	}
	on := currentErr == nil || legacyErr == nil
	if cfg.StartWithWindows != nil {
		on = *cfg.StartWithWindows
	}
	if err := writeStartup(k, on, exe); err != nil {
		return err
	}
	// Only remove the old name after the requested replacement succeeded.
	if legacyErr == nil {
		if err := k.DeleteValue(legacyRunValue); err != nil {
			return err
		}
	}
	cfg.StartWithWindows = &on
	return nil
}

func (a *app) initializeStartup() error {
	if a.demo {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := configureStartup(&a.cfg, runKey, exe); err != nil {
		return err
	}
	a.cfg.Save()
	return nil
}
