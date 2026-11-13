// The per-project launcher the hook files call.
package command

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// installLauncher writes .git/git-memory/gm. On Unix it is a tiny sh script that execs the installed binary, so upgrading
// the binary needs no re-init and agents launched without your shell's PATH still find it. Windows has no sh, so the
// executable itself is copied there.
func installLauncher(dest string) {
	exe, err := os.Executable()
	core.Check(err)
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	core.MkdirAll(filepath.Dir(dest))
	if runtime.GOOS == "windows" {
		if sameFile(exe, dest) {
			return // already running from the installed copy
		}
		copyExecutable(exe, dest)
		return
	}
	core.WriteFile(dest, "#!/bin/sh\nexec "+shQuote(exe)+" \"$@\"\n")
	core.Check(os.Chmod(dest, 0o755))
}

func sameFile(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}

func copyExecutable(src, dest string) {
	in, err := os.Open(src)
	core.Check(err)
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	core.Check(err)
	_, err = io.Copy(out, in)
	out.Close()
	core.Check(err)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		if !core.Exists(dest) { // a running copy cannot be replaced on Windows; keep it
			core.Check(err)
		}
	}
}
