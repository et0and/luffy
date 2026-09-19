//go:build windows

package proc

import "os/exec"

// detach is a no-op on Windows; luffy targets Unix routers.
func detach(c *exec.Cmd) {}
