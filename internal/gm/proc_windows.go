//go:build windows

package gm

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// detach makes cmd outlive the hook process that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup | createNoWindow}
}

// shellOutput runs a command line through cmd.exe with input on stdin and returns its stdout.
func shellOutput(ctx context.Context, command, input string) (string, error) {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command, CreationFlags: createNoWindow}
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	return string(out), err
}
