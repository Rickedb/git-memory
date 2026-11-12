//go:build windows

package gm

import (
	"fmt"
	"os"
	"os/exec"
)

// execGit runs git with this process's stdin, stdout and stderr and returns its exit code (Windows cannot exec in place).
func execGit(gitPath string, args []string) int {
	cmd := exec.Command(gitPath, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "git-memory: cannot run git: %v\n", err)
		return 127
	}
	return 0
}
