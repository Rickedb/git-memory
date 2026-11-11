//go:build !windows

package gm

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

// detach makes cmd outlive the hook process that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// shellOutput runs a shell command line with input on stdin and returns its stdout.
func shellOutput(ctx context.Context, command, input string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	return string(out), err
}
