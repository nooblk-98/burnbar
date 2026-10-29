//go:build !windows

package usage

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
