//go:build !windows

package gm

import (
	"fmt"
	"os"
	"syscall"
)

// execGit replaces this process with git, so stdin, the terminal, signals and the exit code are exactly git's own.
func execGit(gitPath string, args []string) int {
	err := syscall.Exec(gitPath, append([]string{"git"}, args...), os.Environ())
	fmt.Fprintf(os.Stderr, "git-memory: cannot run git: %v\n", err)
	return 127
}
