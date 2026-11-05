//go:build !windows && (!darwin || !cgo)

package app

import "fmt"

func Run(demo bool, mode, version string) error {
	return fmt.Errorf("the desktop app requires Windows or a macOS build with cgo; -once and -snapshot remain available")
}
