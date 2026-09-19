//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so it keeps running when luffy
// exits, the same way start-stop-daemon --background did in the original
// init script.
func detach(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
}
