package gm

// Tests of install.sh: it runs against a fake GitHub releases page served over HTTP, from a file and piped into sh
// (the `curl … | sh` way), with a fake home directory.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func installScript(t *testing.T) string {
	t.Helper()
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	b, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// releases serves a releases page that holds the built binary as the Linux asset for this machine, under both
// /latest/download and /download/v0.0.1, with a matching SHA256SUMS. It returns the page's URL and its folder.
func (e *env) releases() (url, root string) {
	e.t.Helper()
	asset := "git-memory_linux_" + runtime.GOARCH
	bin, err := os.ReadFile(binPath)
	if err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	root = e.mkdir("releases")
	for _, sub := range []string{"latest/download", "download/v0.0.1"} {
		e.write(filepath.Join(root, filepath.FromSlash(sub), asset), string(bin))
		e.write(filepath.Join(root, filepath.FromSlash(sub), "SHA256SUMS"), hex.EncodeToString(sum[:])+"  "+asset+"\n")
		check(os.Chmod(filepath.Join(root, filepath.FromSlash(sub), asset), 0o755))
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(root)))
	e.t.Cleanup(srv.Close)
	return srv.URL, root
}

// install runs install.sh the way the one-liner does (piped into sh) and returns its output and exit code.
func (e *env) install(url string, args ...string) (string, string, int) {
	e.t.Helper()
	e.extra = append(e.extra, "GIT_MEMORY_BASE_URL="+url)
	return e.runIn(e.mkdir("cwd"), installScript(e.t), "sh", append([]string{"-s", "--"}, args...)...)
}

func linuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("install.sh installs the Linux build")
	}
}

func TestInstallShDownloadsVerifiesInstallsAndAddsToPath(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	url, _ := e.releases()
	home := filepath.Dir(e.home)
	dir := filepath.Join(home, ".local", "bin")
	e.extra = []string{"SHELL=/bin/bash"}

	stdout, errOut, code := e.install(url)
	e.eq(code, 0, errOut)
	e.has(stdout, "Checksum verified.")
	e.has(stdout, "Installed: git-memory 0.0.1 ("+dir+"/git-memory)")
	e.has(stdout, "Added "+dir+" to your PATH in "+filepath.Join(home, ".bashrc"))
	e.has(stdout, "git-memory agent init")

	// the binary is there, executable, and is the one that was released
	st, err := os.Stat(filepath.Join(dir, "git-memory"))
	e.ok(err == nil && st.Mode().Perm()&0o111 != 0, "git-memory is not an executable in "+dir)
	out, _, c := e.run(home, filepath.Join(dir, "git-memory"), "agent", "version")
	e.eq(c, 0)
	e.eq(strings.TrimSpace(out), "git-memory 0.0.1")
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".git-memory.new.*"))
	e.eq(len(leftovers), 0, "temporary files")

	// the folder is on the PATH of new terminals, spelled with $HOME, once
	rc := e.read(filepath.Join(home, ".bashrc"))
	e.has(rc, "# >>> git-memory >>>")
	e.has(rc, `export PATH="$HOME/.local/bin:$PATH"`)

	// running it again upgrades in place and does not add the folder a second time
	stdout, errOut, code = e.install(url)
	e.eq(code, 0, errOut)
	e.has(stdout, "Upgraded: git-memory 0.0.1 -> git-memory 0.0.1")
	e.has(stdout, dir+" is already set up in your PATH in "+filepath.Join(home, ".bashrc"), "the second run says it is already set up, not 'added'")
	e.lacks(stdout, "Added "+dir)
	e.eq(strings.Count(e.read(filepath.Join(home, ".bashrc")), "# >>> git-memory >>>"), 1, "the PATH block must be added once")
}

func TestInstallShFollowsTheUsersShell(t *testing.T) {
	linuxOnly(t)
	for shell, rcName := range map[string]string{"/bin/zsh": ".zshrc", "/bin/dash": ".profile", "/usr/bin/ksh": ".profile"} {
		e := newEnv(t)
		url, _ := e.releases()
		home := filepath.Dir(e.home)
		e.extra = []string{"SHELL=" + shell, "ZDOTDIR="}
		_, errOut, code := e.install(url)
		e.eq(code, 0, errOut)
		e.has(e.read(filepath.Join(home, rcName)), "# >>> git-memory >>>", shell+" should write "+rcName)
	}

	// fish keeps its PATH in a universal variable, set with fish_add_path
	if _, err := exec.LookPath("fish"); err == nil {
		e := newEnv(t)
		url, _ := e.releases()
		home := filepath.Dir(e.home)
		e.extra = []string{"SHELL=/usr/bin/fish"}
		stdout, errOut, code := e.install(url)
		e.eq(code, 0, errOut)
		e.has(stdout, "to your fish PATH")
		// fish stores universal variables escaped on disk, so ask fish what a new shell sees
		seen, _, _ := e.run(home, "fish", "-c", "printf '%s\\n' $fish_user_paths")
		e.has(seen, filepath.Join(home, ".local", "bin"), "fish_user_paths in a new fish")
	}
}

func TestInstallShLeavesThePathAloneWhenAskedOrWhenItIsThere(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	url, _ := e.releases()
	home := filepath.Dir(e.home)
	dir := filepath.Join(home, "tools")
	e.extra = []string{"SHELL=/bin/bash"}

	stdout, errOut, code := e.install(url, "--dir", dir, "--no-path")
	e.eq(code, 0, errOut)
	e.has(stdout, "Add "+dir+" to your PATH")
	for _, f := range []string{".bashrc", ".profile", ".zshrc"} {
		e.ok(!e.exists(filepath.Join(home, f)), "--no-path touched "+f)
	}
	e.ok(e.exists(filepath.Join(dir, "git-memory")), "--dir was not honoured")

	// the folder is already on the PATH: nothing to add
	e.extra = []string{"SHELL=/bin/bash", "PATH=" + dir + ":" + os.Getenv("PATH")}
	stdout, errOut, code = e.install(url, "--dir", dir)
	e.eq(code, 0, errOut)
	e.has(stdout, dir+" is already on your PATH.")
	e.ok(!e.exists(filepath.Join(home, ".bashrc")), "the PATH was already right: no startup file needed")
}

func TestInstallShPicksTheRequestedVersion(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	url, _ := e.releases()
	for _, v := range []string{"0.0.1", "v0.0.1"} {
		stdout, errOut, code := e.install(url, "--dir", e.mkdir("bin"), "--no-path", "--version", v)
		e.eq(code, 0, v+": "+errOut)
		e.has(stdout, "/download/v0.0.1")
	}
	// a release that does not exist is an error, and installs nothing
	dir := e.mkdir("bin")
	_, errOut, code := e.install(url, "--dir", dir, "--no-path", "--version", "9.9.9")
	e.eq(code, 1)
	e.has(errOut, "could not download")
	e.ok(!e.exists(filepath.Join(dir, "git-memory")), "a failed install left a binary")
}

func TestInstallShRefusesADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	url, root := e.releases()
	asset := "git-memory_linux_" + runtime.GOARCH
	dir := e.mkdir("bin")

	// the binary was tampered with after SHA256SUMS was made
	e.write(filepath.Join(root, "latest", "download", asset), "#!/bin/sh\necho not git-memory\n")
	_, errOut, code := e.install(url, "--dir", dir, "--no-path")
	e.eq(code, 1)
	e.has(errOut, "checksum mismatch")
	e.has(errOut, "not installing")
	e.ok(!e.exists(filepath.Join(dir, "git-memory")), "a binary that failed verification was installed")

	// SHA256SUMS that does not list this asset at all
	e.write(filepath.Join(root, "latest", "download", "SHA256SUMS"), strings.Repeat("0", 64)+"  git-memory_linux_other\n")
	_, errOut, code = e.install(url, "--dir", dir, "--no-path")
	e.eq(code, 1)
	e.has(errOut, "SHA256SUMS has no entry for "+asset)
	e.ok(!e.exists(filepath.Join(dir, "git-memory")), "installed without a checksum")
}

func TestInstallShExplainsPlatformsItCannotInstall(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	url, _ := e.releases()
	fake := e.mkdir("fakebin") // a uname that reports another machine
	e.write(filepath.Join(fake, "uname"), "#!/bin/sh\nif [ \"$1\" = -s ]; then echo \"$FAKE_OS\"; else echo \"$FAKE_ARCH\"; fi\n")
	check(os.Chmod(filepath.Join(fake, "uname"), 0o755))

	for _, c := range []struct{ os, arch, want string }{
		{"Darwin", "arm64", "no macOS build in the releases yet"},
		{"FreeBSD", "amd64", "unsupported operating system: FreeBSD"},
		{"Linux", "riscv64", "unsupported CPU architecture: riscv64"},
	} {
		e.extra = []string{"PATH=" + fake + ":" + os.Getenv("PATH"), "FAKE_OS=" + c.os, "FAKE_ARCH=" + c.arch}
		_, errOut, code := e.install(url, "--dir", e.mkdir("bin"), "--no-path")
		e.eq(code, 1, c.os+" "+c.arch)
		e.has(errOut, c.want)
	}
	// other spellings of the same machines are understood
	for _, arch := range []string{"x86_64", "amd64", "aarch64", "arm64"} {
		e.extra = []string{"PATH=" + fake + ":" + os.Getenv("PATH"), "FAKE_OS=Linux", "FAKE_ARCH=" + arch}
		stdout, _, _ := e.install(url, "--dir", e.mkdir("bin"), "--no-path")
		want := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[arch]
		e.has(stdout, "git-memory_linux_"+want, arch)
	}
}

func TestInstallShHelpAndBadOptionsWorkWhenPiped(t *testing.T) {
	linuxOnly(t)
	e := newEnv(t)
	stdout, _, code := e.runIn(e.mkdir("cwd"), installScript(t), "sh", "-s", "--", "--help")
	e.eq(code, 0)
	e.has(stdout, "--version <v>")
	e.has(stdout, "--no-path")
	_, errOut, code := e.runIn(e.mkdir("cwd"), installScript(t), "sh", "-s", "--", "--nope")
	e.eq(code, 1)
	e.has(errOut, "unknown option: --nope")
	_, errOut, code = e.runIn(e.mkdir("cwd"), installScript(t), "sh", "-s", "--", "--version")
	e.eq(code, 1)
	e.has(errOut, "--version needs a value")
}

// The installer and the release agree on what the assets are called.
func TestInstallShAndTheMakefileAgreeOnAssetNames(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := os.ReadFile(filepath.Join(root, "install.ps1"))
	sh := installScript(t)
	e := newEnv(t)
	e.has(string(mk), "release/git-memory_$${os}_$${arch}$$ext", "the Makefile names the assets")
	e.has(sh, `ASSET="git-memory_${os}_${arch}"`)
	e.has(string(ps), `git-memory_windows_$arch.exe`)
	for _, target := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
		e.has(string(mk), target, "the Makefile builds every platform the installers can pick")
	}
}
