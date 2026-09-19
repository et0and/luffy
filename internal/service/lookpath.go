package service

import "os/exec"

// defaultLookPath locates an executable on PATH. Tests replace lookPath.
func defaultLookPath(name string) (string, error) {
	return exec.LookPath(name)
}
