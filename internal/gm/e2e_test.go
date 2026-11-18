package gm

// End-to-end tests: the real binary against real git repos in temp dirs. TestMain builds it once; every test then
// drives it exactly as an agent or a person would (git-memory <git command>, agent init, hooks on stdin).
//
// Every test gets its own "machine": a home directory (so a ~/.git-memory that does not exist yet) and its own projects.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

var binPath string // the built git-memory

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gm-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name := "git-memory"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath = filepath.Join(tmp, name)
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	build := exec.Command("go", "build", "-o", binPath, "./cmd/git-memory")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the binary: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// ---- harness ----------------------------------------------------------------------------------------------------

type env struct {
	t     *testing.T
	tmp   string   // every directory of the test lives here
	home  string   // the memory repository of the project most recently made by newRepo: <project>/.git-memory
	user  string   // the home directory of the "machine" currently running commands
	extra []string // more environment for every command
	n     int
}

func realpath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, tmp: realpath(t.TempDir())}
	e.user = e.mkdir("user")
	return e
}

func (e *env) mkdir(prefix string) string {
	e.n++
	d := filepath.Join(e.tmp, fmt.Sprintf("%s-%d", prefix, e.n))
	if err := os.MkdirAll(d, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return d
}

// run executes a command and returns stdout, stderr and the exit code.
func (e *env) run(dir, name string, args ...string) (string, string, int) {
	return e.runIn(dir, "", name, args...)
}

func (e *env) runIn(dir, input, name string, args ...string) (string, string, int) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	userHome := e.user
	cmd.Env = append(os.Environ(), "HOME="+userHome, "USERPROFILE="+userHome, "XDG_CONFIG_HOME="+filepath.Join(userHome, ".config"), "GIT_CONFIG_NOSYSTEM=1", "CLAUDE_CONFIG_DIR=",
		"GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	cmd.Env = append(cmd.Env, e.extra...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			e.t.Fatalf("%s %v: %v", name, args, err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// sh runs a command that must succeed.
func (e *env) sh(dir, name string, args ...string) string {
	e.t.Helper()
	out, errOut, code := e.run(dir, name, args...)
	if code != 0 {
		e.t.Fatalf("%s %s: exit %d\n%s%s", name, strings.Join(args, " "), code, errOut, out)
	}
	return out
}

func (e *env) git(dir string, args ...string) string { e.t.Helper(); return e.sh(dir, "git", args...) }

func (e *env) status(dir string) string {
	e.t.Helper()
	return strings.TrimSpace(e.git(dir, "status", "--porcelain"))
}

// gm runs `git-memory <args>` from dir: a git command, routed to the memory repository.
func (e *env) gm(dir string, args ...string) string {
	e.t.Helper()
	return e.sh(dir, binPath, args...)
}

// gmRaw runs git-memory and returns stdout, stderr and the exit code (it may fail).
func (e *env) gmRaw(dir string, args ...string) (string, string, int) {
	return e.run(dir, binPath, args...)
}

// agentInit wires a project to the memory repository (what a user runs once per project): every agent unless args say
// which (args may be flags only).
func (e *env) agentInit(dir string, args ...string) string {
	e.t.Helper()
	named := false
	for _, a := range args {
		named = named || !strings.HasPrefix(a, "-")
	}
	if !named {
		args = append([]string{"all"}, args...)
	}
	return e.sh(dir, binPath, append([]string{"agent", "init"}, args...)...)
}

func launcher(dir string) string { return filepath.Join(dir, ".git", "git-memory", core.WrapperName()) }

func (e *env) hookRaw(dir, agent, event string, payload map[string]any) string {
	e.t.Helper()
	p := map[string]any{"cwd": dir}
	for k, v := range payload {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	out, errOut, code := e.runIn(dir, string(raw), launcher(dir), "agent", "hook", agent, event)
	if code != 0 {
		e.t.Fatalf("hook %s %s: exit %d\n%s", agent, event, code, errOut)
	}
	return out
}

// hook fires a hook through the project's launcher and returns what it printed (nil when it printed nothing).
func (e *env) hook(dir, agent, event string, payload map[string]any) map[string]any {
	e.t.Helper()
	out := e.hookRaw(dir, agent, event, payload)
	if strings.TrimSpace(out) == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		e.t.Fatalf("hook printed invalid JSON: %q", out)
	}
	return m
}

// newRepo makes a project and wires it. Its folder in the memory repository is named after the directory.
func (e *env) newProject(name string) string {
	e.t.Helper()
	dir := e.newRepo(name)
	e.agentInit(dir)
	return dir
}

// newRepo is a project whose user ran `git-memory init` in it: it has its own .git-memory.
func (e *env) newRepo(name string) string {
	e.t.Helper()
	dir := e.newRepoNoMemory(name)
	e.sh(dir, binPath, "init", "-q")
	e.home = filepath.Join(dir, ".git-memory")
	return dir
}

func (e *env) newRepoNoMemory(name string) string {
	e.t.Helper()
	dir := e.mkdir("gm-" + name)
	e.git(dir, "init", "-q", "-b", "main")
	e.git(dir, "config", "user.email", "t@example.com")
	e.git(dir, "config", "user.name", "T")
	e.write(filepath.Join(dir, "README.md"), "# demo\n")
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "init")
	return dir
}

func (e *env) bare() string {
	d := e.mkdir("gm-bare")
	e.git(d, "init", "-q", "--bare", "-b", "main")
	return d
}

func (e *env) write(p, content string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(p string) string {
	e.t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *env) readJSON(p string) map[string]any {
	e.t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(e.read(p)), &m); err != nil {
		e.t.Fatalf("%s: %v", p, err)
	}
	return m
}

// withMachine runs fn as another machine: another home directory and its own projects.
func (e *env) withMachine(fn func()) {
	prevHome, prevUser := e.home, e.user
	e.user = e.mkdir("machine")
	defer func() { e.home, e.user = prevHome, prevUser }()
	fn()
}

// doctor runs `git-memory doctor --report` in dir and returns its rows: label → detail.
func (e *env) doctor(dir string) (map[string]string, string) {
	e.t.Helper()
	out := e.sh(dir, binPath, "doctor", "--report")
	rows := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if m := regexp.MustCompile(`^[✓!✗·] ([^:]+): (.*)$`).FindStringSubmatch(l); m != nil {
			rows[m[1]] = m[2]
		}
	}
	return rows, out
}

// project is the project's folder in the memory repository, as `doctor` reports it.
func (e *env) project(dir string) string {
	e.t.Helper()
	rows, out := e.doctor(dir)
	if rows["Project folder"] == "" {
		e.t.Fatalf("doctor printed no project folder:\n%s", out)
	}
	return projectPrefix(rows["Project folder"])
}

// projectPrefix turns the doctor's "Project folder" row into what goes in front of a page path in the memory repository:
// nothing at the repository's top, else the root folder and a slash.
func projectPrefix(row string) string {
	folder, _, _ := strings.Cut(row, " (")
	if folder == "." || folder == "" {
		return ""
	}
	return folder + "/"
}

// pages lists the files the memory repository tracks, as `git-memory ls-files` does.
func (e *env) tracked(dir string) string { return e.gm(dir, "ls-files") }

// pageOf finds the tracked page whose path contains sub.
func (e *env) pageOf(dir, sub string) string {
	e.t.Helper()
	for _, f := range strings.Split(e.tracked(dir), "\n") {
		if strings.Contains(f, sub) {
			return f
		}
	}
	e.t.Fatalf("no tracked page contains %q in:\n%s", sub, e.tracked(dir))
	return ""
}

// show prints a page from the memory repository.
func (e *env) show(dir, sub string) string { return e.gm(dir, "show", "HEAD:"+e.pageOf(dir, sub)) }

// findSession finds the session page whose file name contains sub. Session pages are local files of the memory
// repository, kept out of git, so they are read from disk. It returns the page's path in the repository ("" if none).
func (e *env) findSession(sub string) string {
	var found string
	_ = filepath.WalkDir(e.home, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(d.Name(), sub) && strings.Contains(filepath.ToSlash(p), "/sessions/") {
			rel, _ := filepath.Rel(e.home, p)
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	return found
}

// sessionPage is a session page's path in the memory repository and its text.
func (e *env) sessionPage(sub string) (string, string) {
	e.t.Helper()
	rel := e.findSession(sub)
	if rel == "" {
		e.t.Fatalf("no session page contains %q under %s", sub, e.home)
	}
	return rel, e.read(filepath.Join(e.home, filepath.FromSlash(rel)))
}

// commitPage writes a page into the memory repository and commits it, the way a person (or an agent following a slash
// command) would: plain files and plain git.
func (e *env) commitPage(dir, repoPath, content string) {
	e.t.Helper()
	e.write(filepath.Join(e.home, filepath.FromSlash(repoPath)), content)
	e.gm(dir, "add", "-A", "--", repoPath)
	e.gm(dir, "commit", "-q", "-m", "memory: "+repoPath, "--", repoPath)
}

func (e *env) skipOnWindows() {
	if runtime.GOOS == "windows" {
		e.t.Skip("hook launcher and shell behavior on Windows is not verified yet")
	}
}

// ---- assertions -------------------------------------------------------------------------------------------------

func (e *env) match(s, pattern string, msg ...string) {
	e.t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		e.t.Errorf("expected /%s/ %s in:\n%s", pattern, strings.Join(msg, " "), s)
	}
}

func (e *env) has(s, sub string, msg ...string) {
	e.t.Helper()
	if !strings.Contains(s, sub) {
		e.t.Errorf("expected %q %s in:\n%s", sub, strings.Join(msg, " "), s)
	}
}

func (e *env) lacks(s, sub string, msg ...string) {
	e.t.Helper()
	if strings.Contains(s, sub) {
		e.t.Errorf("did not expect %q %s in:\n%s", sub, strings.Join(msg, " "), s)
	}
}

func (e *env) eq(got, want any, msg ...string) {
	e.t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		e.t.Errorf("%s: got %v, want %v", strings.Join(msg, " "), got, want)
	}
}

func (e *env) ok(cond bool, msg string) {
	e.t.Helper()
	if !cond {
		e.t.Error(msg)
	}
}

func (e *env) exists(p string) bool { _, err := os.Stat(p); return err == nil }

// at digs into decoded JSON: strings index objects, ints index arrays.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func ctxOf(out map[string]any) string {
	if s, ok := at(out, "hookSpecificOutput", "additionalContext").(string); ok {
		return s
	}
	s, _ := at(out, "additionalContext").(string)
	return s
}

// ---- routing: git-memory <git command> is git, in the memory repository -----------------------------------------

func TestEveryCommandIsGitInTheMemoryRepository(t *testing.T) {
	e := newEnv(t)
	elsewhere := e.newRepoNoMemory("elsewhere") // a project: its own repository must not be touched
	e.home = filepath.Join(elsewhere, ".git-memory")

	// nothing makes a memory repository but init: not a git-memory command, not a hook
	_, errOut0, code0 := e.gmRaw(elsewhere, "status")
	e.eq(code0, 1)
	e.has(errOut0, "no memory repository at "+e.home+": run `git-memory init`")
	e.ok(!e.exists(e.home), "a command created the memory repository")
	e.has(e.gm(elsewhere, "init"), "Initialized empty Git repository")
	e.has(e.gm(elsewhere, "status"), "No commits yet")
	e.eq(e.status(elsewhere), "", "the .git-memory folder stays out of the project's git status")
	e.ok(!e.exists(filepath.Join(elsewhere, ".gitignore")), ".gitignore touched")
	e.eq(strings.TrimSpace(e.git(e.home, "symbolic-ref", "--short", "HEAD")), "main")

	e.write(filepath.Join(e.home, "note.md"), "# a note\n")
	e.gm(elsewhere, "add", ".")
	e.gm(elsewhere, "commit", "-q", "-m", "first note")

	// the answers are git's own, byte for byte, because it IS git
	viaGM := e.gm(elsewhere, "log", "--format=%an|%s")
	viaGit := e.git(e.home, "log", "--format=%an|%s")
	e.eq(viaGM, viaGit)
	e.has(viaGM, "first note")
	e.eq(strings.TrimSpace(e.gm(elsewhere, "ls-files")), ".gitignore\nnote.md") // the .gitignore that keeps session pages out
	e.eq(strings.TrimSpace(e.gm(elsewhere, "rev-parse", "--show-toplevel")), e.home)
	e.has(e.gm(elsewhere, "grep", "-n", "note"), "note.md:1:# a note")
	e.has(e.gm(elsewhere, "show", "HEAD:note.md"), "# a note")

	// the project's own history is untouched
	e.eq(strings.TrimSpace(e.git(elsewhere, "log", "--format=%s")), "init")
	e.eq(e.status(elsewhere), "")

	// git's exit codes and stderr come through (128 is git's "fatal")
	_, errOut, code := e.gmRaw(elsewhere, "rev-parse", "--verify", "no-such-ref")
	_, gitErr, gitCode := e.run(e.home, "git", "rev-parse", "--verify", "no-such-ref")
	e.eq(code, gitCode)
	e.eq(errOut, gitErr)
	_, _, code = e.gmRaw(elsewhere, "definitely-not-a-git-command")
	e.eq(code, 1, "git's unknown-command exit code")
}

func TestStdinFlagsAndGlobalOptionsReachGit(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("flags")

	// stdin is git's stdin
	out, _, code := e.runIn(dir, "hello\n", binPath, "hash-object", "--stdin")
	e.eq(code, 0)
	want, _, _ := e.runIn(dir, "hello\n", "git", "hash-object", "--stdin")
	e.eq(out, want)

	// global options before the subcommand (-c, -C) and the subcommand's own flags
	e.write(filepath.Join(e.home, "a.md"), "a\n")
	e.gm(dir, "add", "a.md")
	e.gm(dir, "commit", "-q", "-m", "a")
	e.eq(len(strings.TrimSpace(e.gm(dir, "-c", "core.abbrev=12", "log", "-1", "--format=%h"))), 12)
	e.has(e.gm(dir, "-C", ".", "log", "--oneline"), "a")

	// options-only invocations are git's, and need no repository at all
	fresh := newEnv(t)
	out, _, code = fresh.gmRaw(dir, "--version")
	fresh.eq(code, 0)
	fresh.match(out, `^git version `)
	fresh.ok(!fresh.exists(fresh.home), "--version created the memory repository")
	out, _, _ = fresh.gmRaw(dir, "help", "-a")
	fresh.has(out, "commit")
	fresh.ok(!fresh.exists(fresh.home), "help created the memory repository")
}

func TestCloneUsesAnExistingMemoryRepository(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepoNoMemory("c")
	remote := e.bare()
	seed := e.newRepo("seed")
	e.write(filepath.Join(seed, "global/memories/fact/a.md"), "---\nkind: fact\n---\n\n# A fact\n\nThe API listens on 8080.\n")
	e.git(seed, "add", ".")
	e.git(seed, "commit", "-qm", "seed")
	e.git(seed, "push", "-q", remote, "main")

	e.gm(dir, "clone", "-q", remote) // into ./.git-memory, the folder you are in
	e.home = filepath.Join(dir, ".git-memory")
	e.has(e.gm(dir, "log", "--format=%s"), "seed")
	e.ok(e.exists(filepath.Join(e.home, "global/memories/fact/a.md")), "not cloned into the memory repository")
	e.eq(e.status(dir), "", "the clone stays out of the project's git status")

	// a second clone must not trample what is there
	_, errOut, code := e.gmRaw(dir, "clone", "-q", remote)
	e.eq(code, 1)
	e.has(errOut, "already exists and is not empty")
	e.has(e.gm(dir, "log", "--format=%s"), "seed")
}

func TestFolderThatIsNotARepositoryIsNeverInitialisedByAccident(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepoNoMemory("n")
	e.home = filepath.Join(dir, ".git-memory")
	e.write(filepath.Join(e.home, "old/config.json"), "{}")

	_, errOut, code := e.gmRaw(dir, "log")
	e.eq(code, 1)
	e.has(errOut, "is not a git repository")
	e.ok(!e.exists(filepath.Join(e.home, ".git")), "initialised over somebody's files")

	e.gm(dir, "init", "-q") // the explicit way
	e.ok(e.exists(filepath.Join(e.home, ".git")), "init did not initialise it")
	e.has(e.gm(dir, "status", "--short"), "old/")
}

func TestSyncingAndSharingIsPlainGit(t *testing.T) {
	e := newEnv(t)
	remote := e.bare()
	a := e.newRepo("a")

	e.gm(a, "remote", "add", "origin", remote)
	e.commitPage(a, "global/memories/decision/pnpm.md", "---\nkind: decision\n---\n\n# Use pnpm\n\nNever npm.\n")
	e.gm(a, "push", "-q", "-u", "origin", "main")

	e.withMachine(func() { // another machine of the same person
		b := e.newRepoNoMemory("b")
		e.gm(b, "clone", "-q", remote)
		e.home = filepath.Join(b, ".git-memory")
		e.has(e.gm(b, "ls-files"), "global/memories/decision/pnpm.md")
		e.commitPage(b, "global/memories/fact/port.md", "---\nkind: fact\n---\n\n# Port\n\n8080.\n")
		e.gm(b, "push", "-q")
	})
	e.gm(a, "pull", "-q")
	e.has(e.gm(a, "ls-files"), "global/memories/fact/port.md")
}

// git-memory may only keep words that git does not use, or it would shadow git. The one word it keeps is checked
// against the real git on this machine.
func TestOurOwnCommandsNeverShadowGit(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("shadow")

	all := e.git(dir, "help", "-a")
	for _, word := range ownWords {
		for _, line := range strings.Split(all, "\n") {
			if fields := strings.Fields(line); len(fields) > 0 && fields[0] == word {
				t.Fatalf("git itself has a %q command now; pick another word for git-memory's own commands", word)
			}
		}
		_, _, code := e.run(dir, "git", word)
		e.ok(code != 0, "`git "+word+"` is a git command")
	}

	// and the words git-memory used to own are git's again
	_, errOut, code := e.gmRaw(dir, "hook")
	e.eq(code, 129)
	e.has(errOut, "git hook run", "`git-memory hook` must be git's own")
	out, _, code := e.gmRaw(dir, "version")
	e.eq(code, 0)
	e.match(out, `^git version `, "`git-memory version` must be git's own")
	e.gm(dir, "init", "-q") // creates the memory repository, as `git init` would: here
	e.ok(e.exists(filepath.Join(dir, ".git-memory/.git")), "init is git's")
}

// ---- wiring a project ---------------------------------------------------------------------------------------------

func TestAgentInitAddsNothingToTheProject(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("personal")
	out := e.agentInit(dir)
	e.has(out, "memory repository: "+e.home)

	// zero footprint: nothing for git status, no config files anywhere
	e.eq(e.status(dir), "")
	e.ok(!e.exists(filepath.Join(dir, ".gitignore")), ".gitignore touched")
	for _, f := range []string{filepath.Join(dir, "git-memory.json"), filepath.Join(dir, ".git/git-memory/config.json"), filepath.Join(e.home, "config.json")} {
		e.ok(!e.exists(f), "config file written: "+f)
	}
	e.ok(e.exists(filepath.Join(e.home, ".git")), "the memory repository is created by agent init")

	claude := e.readJSON(filepath.Join(dir, ".claude/settings.local.json"))
	e.match(at(claude, "hooks", "SessionStart", 0, "hooks", 0, "command").(string), `agent hook claude SessionStart`)
	copilot := e.readJSON(filepath.Join(dir, ".github/hooks/git-memory.json"))
	e.eq(at(copilot, "version"), 1)
	e.match(at(copilot, "hooks", "agentStop", 0, "command").(string), `agent hook copilot agentStop`)

	e.agentInit(dir) // idempotent
	e.eq(len(at(e.readJSON(filepath.Join(dir, ".claude/settings.local.json")), "hooks", "Stop").([]any)), 1)
	e.eq(e.status(dir), "")
}

func TestDoctorReportsVersionAndHealth(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("Shop App")
	e.home = filepath.Join(dir, ".git-memory")

	// before anything exists: the version, a memory repository that is not there yet, a project that is not wired
	stdout, _, code := e.gmRaw(dir, "doctor")
	e.eq(code, 0)
	e.match(stdout, `^git-memory 0\.0\.1 · git version \d`, "the first line is the version")
	e.has(stdout, "! Memory repository: none at "+e.home+": run git-memory init")
	e.has(stdout, "! Claude Code hooks: not installed")
	e.has(stdout, "! Copilot hooks: not installed")
	e.has(stdout, "· Slash commands: not installed")
	e.has(stdout, "· Hook launcher: not installed", "a project that is not wired yet is not an error")
	e.ok(!e.exists(e.home), "doctor must not create the memory repository")

	e.agentInit(dir)
	rows, out := e.doctor(dir)
	e.match(rows["Memory repository"], `^`+regexp.QuoteMeta(e.home)+` \(branch t, 1 commit\)$`)
	e.has(out, "· Remote: none: memory stays on this machine")
	e.has(out, "✓ Uncommitted changes: none")
	e.eq(projectPrefix(rows["Project folder"]), "", "the memories are at the repository's top by default")
	e.eq(strings.HasPrefix(rows["Global folder"], "global"), true)
	e.has(out, "✓ Claude Code hooks: 7 events")
	e.has(out, "✓ Copilot hooks: 6 events")
	e.has(out, "✓ Slash commands: Claude Code 9/9, Copilot 9/9")
	e.match(out, `✓ Hook launcher: \.git/git-memory/gm`)
	e.match(out, `✓ Binary used by hooks: .*git-memory`)
	e.has(out, "· Agent activity: no hook events yet")
	e.has(out, "✓ Hook errors: none")

	// activity, pages, errors and uncommitted work show up
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "d1d1d1d1"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "d1d1d1d1", "prompt": "remember that doctor counts pages"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "d1d1d1d1"})
	_, out = e.doctor(dir)
	e.has(out, "✓ Agent activity: last hook event just now")
	e.has(out, "1 memory and 1 session in this project, 0 global")
	e.match(out, `✓ Memory repository: .* \(branch t, 2 commits\)`)
	e.write(filepath.Join(e.home, "scratch.md"), "# draft\n")
	e.has(e.sh(dir, binPath, "doctor"), "! Uncommitted changes: 1 file")
	e.write(filepath.Join(dir, ".git/git-memory/errors.log"), "2026-10-25T00:00:00Z Error: boom\n")
	e.match(e.sh(dir, binPath, "doctor"), `! Hook errors: 1 logged; latest: .*boom`)

	// a launcher that points at a binary which no longer exists is an error: exit 1, unless --report
	l := launcher(dir)
	e.write(l, strings.Replace(e.read(l), realpath(binPath), "/nonexistent/git-memory", 1))
	stdout, _, code = e.gmRaw(dir, "doctor")
	e.eq(code, 1)
	e.has(stdout, "✗ Binary used by hooks: not found: /nonexistent/git-memory")
	_, _, code = e.gmRaw(dir, "doctor", "--report")
	e.eq(code, 0, "--report always exits 0")

	// a memory "repository" that is somebody's folder is reported, and left alone
	bad := e.newRepoNoMemory("bad")
	e.write(filepath.Join(bad, ".git-memory/precious.txt"), "x")
	stdout, _, code = e.gmRaw(bad, "doctor")
	e.eq(code, 1)
	e.has(stdout, "✗ Memory repository: "+filepath.Join(bad, ".git-memory")+" exists, is not empty and is not a git repository")

	// outside any project it still reports the repository, and says so
	outside := e.mkdir("outside")
	stdout, _, code = e.gmRaw(outside, "doctor")
	e.eq(code, 0)
	e.has(stdout, "· Project: not inside a git project")
	e.has(stdout, "Memory repository")
}

// Like git, which is "the .git folder", git-memory is "the ~/.git-memory folder": no environment variable moves it.
func TestNoEnvironmentVariableChangesWhereTheMemoryLives(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("env")
	elsewhere := e.mkdir("elsewhere")
	e.extra = []string{"GIT_MEMORY_HOME=" + elsewhere, "GIT_MEMORY_DISABLE=1", "GIT_MEMORY_DIR=" + elsewhere}

	e.gm(dir, "status")
	e.ok(e.exists(filepath.Join(e.home, ".git")), "the repository must be ./.git-memory")
	e.ok(!e.exists(filepath.Join(elsewhere, ".git")), "GIT_MEMORY_HOME must not be honoured")

	// and the off switch is gone with it: the hooks still work
	e.agentInit(dir)
	e.skipOnWindows()
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "ev1ev1ev1", "prompt": "remember that nothing but the folder matters"})
	e.has(e.gm(dir, "ls-files"), "nothing-but-the-folder-matters")
}

func TestAgentInitKeepsTheUsersOwnHooksAndMigratesOldOnes(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("keep")
	e.agentInit(dir)
	file := filepath.Join(dir, ".claude/settings.local.json")
	s := e.readJSON(file)
	stop := at(s, "hooks", "Stop").([]any)
	old := core.Obj{"hooks": []any{core.Obj{"type": "command", "command": `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" hook claude Stop`}}} // the form earlier builds wrote
	s["hooks"].(map[string]any)["Stop"] = append(stop, core.Obj{"hooks": []any{core.Obj{"type": "command", "command": "echo mine"}}}, old)
	s["permissions"] = core.Obj{"allow": []any{"Bash(ls)"}}
	e.write(file, core.MarshalJSON(s, true))

	e.agentInit(dir)
	after := e.readJSON(file)
	groups := at(after, "hooks", "Stop").([]any)
	e.eq(len(groups), 2, "the old entry is replaced, not duplicated")
	e.eq(at(groups[0], "hooks", 0, "command"), "echo mine")
	e.has(at(groups[1], "hooks", 0, "command").(string), "agent hook claude Stop")
	e.eq(at(after, "permissions", "allow", 0), "Bash(ls)")
}

// Project hook files written by earlier development builds call `gm hook <agent> <event>`; they keep working until re-initialised.
func TestHooksOfOlderInstallsStillWork(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("legacy")
	payload, _ := json.Marshal(map[string]any{"cwd": dir, "session_id": "old1old1", "prompt": "Add a retry to the uploader"})
	_, errOut, code := e.runIn(dir, string(payload), launcher(dir), "hook", "claude", "UserPromptSubmit")
	e.eq(code, 0, errOut)
	e.has(e.read(filepath.Join(dir, ".git/git-memory/spool/claude-old1old1.jsonl")), "Add a retry to the uploader")
}

// ---- sessions through the hooks ---------------------------------------------------------------------------------

func TestClaudeSessionCaptureCompileRecall(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("claude")
	proj := e.project(dir)
	const sid = "s1aaaaaa"

	e.eq(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": sid, "source": "startup"}), map[string]any(nil)) // empty memory
	e.eq(e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid, "prompt": "Fix the flaky upload retry test, token=abcdefghijklmnop1234"}), map[string]any(nil))
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(dir, "src/upload.ts")}})
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(dir, ".env")}})
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Bash", "tool_input": map[string]any{"command": "npm test"}})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": sid, "last_assistant_message": "Pinned the retry clock so the test is deterministic."})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": sid, "reason": "other"})

	// the session is a page in the memory repository, committed there, and nowhere near the project
	page, shown := e.sessionPage("claude-s1aaaaaa")
	e.ok(strings.HasPrefix(page, proj+"sessions/"), "session page outside the project's folder: "+page)
	e.lacks(e.gm(dir, "log", "--format=%s"), "memory: session", "session pages are kept out of git")
	e.lacks(e.tracked(dir), "claude-s1aaaaaa")
	e.eq(e.status(dir), "")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "", "nothing the hooks wrote is left uncommitted")
	e.has(shown, "Fix the flaky upload retry test")
	e.has(shown, "Pinned the retry clock")
	e.has(shown, "`src/upload.ts`")
	e.lacks(shown, ".env", "excluded file recorded")
	e.lacks(shown, "abcdefghijklmnop1234", "secret leaked")
	e.has(shown, "`npm test`")

	// next session gets a briefing and prompt-time recall
	start := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "s2bbbbbb", "source": "startup"})
	e.eq(at(start, "hookSpecificOutput", "hookEventName"), "SessionStart")
	e.has(ctxOf(start), "Recent sessions")
	e.has(ctxOf(start), "Pinned the retry clock")
	recall := e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "s2bbbbbb", "prompt": "the upload retry test is flaky again, what did we change last time?"})
	e.has(ctxOf(recall), "Fix the flaky upload retry test")
	e.has(ctxOf(recall), proj+"sessions/", "recall cites the page's path in the memory repository")
}

func TestCopilotSessionUsesCamelCasePayloads(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("copilot")
	proj := e.project(dir)
	e.commitPage(dir, proj+"memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

	start := e.hook(dir, "copilot", "sessionStart", map[string]any{"sessionId": "c1", "source": "new"})
	e.has(ctxOf(start), "Package manager")
	_, hasSpecific := start["hookSpecificOutput"]
	e.ok(!hasSpecific, "copilot output must not use hookSpecificOutput")

	e.hook(dir, "copilot", "userPromptSubmitted", map[string]any{"sessionId": "c1", "prompt": "add a lint script"})
	toolArgs, _ := json.Marshal(map[string]any{"command": "pnpm lint"})
	e.hook(dir, "copilot", "postToolUse", map[string]any{"sessionId": "c1", "toolName": "run_in_terminal", "toolArgs": string(toolArgs),
		"toolResult": map[string]any{"resultType": "failure", "textResultForLlm": "boom"}})
	e.hook(dir, "copilot", "postToolUse", map[string]any{"sessionId": "c1", "toolName": "replace_string_in_file", "toolArgs": map[string]any{"filePath": "package.json"}})
	e.eq(e.hook(dir, "copilot", "userPromptSubmitted", map[string]any{"sessionId": "c1", "prompt": strings.Repeat("x", 30)}), map[string]any(nil))
	e.hook(dir, "copilot", "agentStop", map[string]any{"sessionId": "c1", "stopReason": "end_turn"})
	e.hook(dir, "copilot", "sessionEnd", map[string]any{"sessionId": "c1", "reason": "complete"})

	_, shown := e.sessionPage("copilot-c1")
	e.has(shown, "add a lint script")
	e.has(shown, "`pnpm lint` (failed)")
	e.has(shown, "`package.json`")
}

func TestSessionPageStripsEditorContextAndEmptySessionsLeaveNothing(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("pages")
	const sid = "pg1pg1pg1"

	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": sid})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid,
		"prompt": "<ide_opened_file>The user opened the file /x/y.ts in the IDE.</ide_opened_file> add a health endpoint\n<ide_selection>const secretish = 1;</ide_selection>"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid, "prompt": "<ide_opened_file>only context, no words</ide_opened_file>"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": sid, "last_assistant_message": "Added /health.\n\n- route in src/server.ts\n- test in test/health.test.ts"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": sid})

	_, page := e.sessionPage("pg1pg1pg")
	e.match(page, `(?m)^# add a health endpoint$`, "the title is what the user asked, not editor context")
	e.ok(!strings.Contains(page, "ide_") && !strings.Contains(page, "secretish"), "editor context leaked into the page")
	e.has(page, "1 prompt(s)", "a prompt that was only context is not recorded")
	e.has(page, "## Outcome\n\nAdded /health.\n\n- route in src/server.ts\n- test in test/health.test.ts", "line breaks survive")

	// a session where nothing was asked produces neither a page nor a commit
	before := e.gm(dir, "log", "--format=%H")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "em1em1em1"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "em1em1em1"})
	e.eq(e.gm(dir, "log", "--format=%H"), before)
	e.eq(e.findSession("em1em1em"), "", "a session where nothing was asked leaves no page")
}

// ---- what the briefing reads --------------------------------------------------------------------------------------

func TestBriefingReadsTheProjectAndGlobalFoldersOnly(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	a := e.newProject("brief-a")
	pa := e.project(a)
	page := func(title, body string) string {
		return "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# " + title + "\n\n" + body + "\n"
	}
	e.commitPage(a, pa+"memories/decision/strict-ts.md", page("Strict TS", "Always use strict TypeScript."))
	e.commitPage(a, "global/memories/decision/pnpm.md", page("Use pnpm", "Never npm."))

	bf := ctxOf(e.hook(a, "claude", "SessionStart", map[string]any{"session_id": "ba1ba1ba1"}))
	e.has(bf, "- Strict TS — Always use strict TypeScript. ("+pa+"memories/decision/strict-ts.md)")
	e.has(bf, "- Use pnpm {global} — Never npm. (global/memories/decision/pnpm.md)")
	e.has(bf, "git run inside it", "the briefing tells the agent what the CLI is")
	e.has(bf, "./.git/git-memory/gm grep")

	again := ctxOf(e.hook(a, "claude", "SessionStart", map[string]any{"session_id": "bb1bb1bb1"}))
	e.has(again, "Use pnpm {global}", "global is read with the project's own")

	// moving a page between the folders is a plain git mv
	e.gm(a, "mv", pa+"memories/decision/strict-ts.md", "global/memories/decision/strict-ts.md")
	e.gm(a, "commit", "-q", "-m", "promote", "--", pa+"memories", "global/memories")
	e.has(ctxOf(e.hook(a, "claude", "SessionStart", map[string]any{"session_id": "bb2bb2bb2"})), "Strict TS {global}")
}

func TestPinnedPagesComeFirstAndSupersededOnesLeave(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("dec")
	proj := e.project(dir)
	page := func(front, title, body string) string {
		return "---\nkind: decision\n" + front + "tags: []\n---\n\n# " + title + "\n\n" + body + "\n"
	}
	e.commitPage(dir, proj+"core/no-secrets.md", page("tier: core\n", "No secrets", "Never commit secrets or .env files."))
	e.commitPage(dir, proj+"memories/decision/oldest.md", page("", "Oldest decision", "The oldest decision, made first."))
	for i := 1; i <= 12; i++ {
		e.commitPage(dir, fmt.Sprintf("%smemories/decision/d%d.md", proj, i), page("", fmt.Sprintf("Decision %d", i), fmt.Sprintf("Decision number %d body.", i)))
	}
	e.commitPage(dir, proj+"memories/decision/rest.md", page("", "Use REST", "Public APIs are REST."))

	b := ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "dc1dc1dc1"}))
	core, decisions := strings.Index(b, "## Core notes"), strings.Index(b, "## Decisions in force")
	e.ok(core > 0 && core < decisions, "pinned notes first")
	e.has(b, "### No secrets\nNever commit secrets or .env files.", "a pinned note must not repeat its own title")
	e.has(b, "- Oldest decision", "old decisions stay in force")
	for i := 1; i <= 12; i++ {
		e.has(b, fmt.Sprintf("- Decision %d", i))
	}
	e.lacks(b, "- No secrets", "a pinned note is not listed twice")
	e.has(b, "- Use REST")

	// retiring a decision is an edit of its frontmatter, committed like any other change
	e.commitPage(dir, proj+"memories/decision/rest.md", page("status: superseded\n", "Use REST", "> Superseded: clients need flexible queries\n\nPublic APIs are REST."))
	b = ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "dc2dc2dc2"}))
	e.lacks(b, "Use REST", "a superseded decision must leave the briefing")
	recall := e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "dc2dc2dc2", "prompt": "should the public api be rest or graphql for the new endpoint?"})
	e.lacks(ctxOf(recall), "Use REST", "recall must not surface superseded pages")
	e.has(e.gm(dir, "show", "HEAD:"+proj+"memories/decision/rest.md"), "Public APIs are REST", "but it is kept")
}

// ---- remembering from the prompt --------------------------------------------------------------------------------

func TestPromptTriggersAreSavedAndCommitted(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("trig")
	proj := e.project(dir)

	say := func(prompt, agent string) map[string]any {
		event := "UserPromptSubmit"
		if agent == "copilot" {
			event = "userPromptSubmitted"
		}
		return e.hook(dir, agent, event, map[string]any{"session_id": "tr1tr1tr1", "sessionId": "tr1tr1tr1", "prompt": prompt})
	}
	echo := say(strings.Join([]string{
		"<ide_opened_file>The user opened a file.</ide_opened_file> Please fix the bug.",
		"remember that we deploy on Tuesdays only",
		"Decision: use pnpm, never npm",
		"#procedure run make migrate before the tests",
		"remember to run tests first",
		"Note: the build is red on CI",
		"#fact the staging DB_PASSWORD=hunter2hunter2 must come from the vault",
	}, "\n"), "claude")
	e.eq(echo, map[string]any(nil), "a prompt must not be answered with the memories it has just saved")

	files := strings.Fields(e.gm(dir, "ls-files", "--", proj+"memories"))
	e.eq(len(files), 4, "four markers, four pages: "+strings.Join(files, " "))
	all := ""
	for _, f := range files {
		all += e.gm(dir, "show", "HEAD:"+f) + "\n"
	}
	e.has(all, "we deploy on Tuesdays only")
	e.has(all, "use pnpm, never npm")
	e.has(all, "run make migrate before the tests")
	e.lacks(all, "run tests first", "instructions are not memories")
	e.lacks(all, "build is red", "prose is not a memory")
	e.lacks(all, "hunter2hunter2", "secrets are redacted")
	e.has(all, "DB_PASSWORD=[REDACTED]")
	e.has(all, "tags: [auto, prompt]")
	e.has(all, "source: claude:tr1tr1tr1")
	e.has(e.gm(dir, "log", "--format=%s"), "memory: remember we deploy on Tuesdays only")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")
	e.eq(e.status(dir), "", "nothing is added to the project")

	// the saved memories reach the next session's briefing like any other, for Copilot's prompt hook too
	say("#remember releases are tagged with a v prefix", "copilot")
	next := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "tr2tr2tr2"})
	e.has(ctxOf(next), "- use pnpm, never npm")
	e.has(ctxOf(next), "releases are tagged with a v prefix")
}

func TestABrokenMemoryRepositoryNeverBreaksTheHook(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()

	// the memory repository is somebody's non-repo folder: agent init leaves it alone (and says so), and a hook logs and
	// exits cleanly while still recording the prompt
	{
		broken := e.newRepoNoMemory("broken")
		e.home = filepath.Join(broken, ".git-memory")
		e.write(filepath.Join(e.home, "precious.txt"), "do not touch")
		out := e.agentInit(broken)
		e.has(out, "is not empty and is not a git repository")
		hookOut := e.hookRaw(broken, "claude", "UserPromptSubmit", map[string]any{"session_id": "of2of2of2", "prompt": "remember that the folder is not a repository"})
		e.eq(strings.TrimSpace(hookOut), "")
		e.has(e.read(filepath.Join(broken, ".git/git-memory/errors.log")), "is not a git repository")
		e.has(e.read(filepath.Join(broken, ".git/git-memory/spool/claude-of2of2of2.jsonl")), "the folder is not a repository", "the session still recorded the prompt")
		e.eq(e.read(filepath.Join(e.home, "precious.txt")), "do not touch")
		e.ok(!e.exists(filepath.Join(e.home, ".git")), "the folder was initialised behind the user's back")
	}
}

// The hooks commit only their own folder: whatever else the user has staged in the memory repository stays staged.
func TestHooksOnlyCommitTheirOwnFolder(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("own")
	e.gm(dir, "status")
	e.write(filepath.Join(e.home, "scratch/draft.md"), "# half-written\n")
	e.gm(dir, "add", "scratch/draft.md")

	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "ow1ow1ow1"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "ow1ow1ow1", "prompt": "remember that the uploader retries three times"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "ow1ow1ow1"})

	e.has(e.gm(dir, "log", "--format=%s"), "memory: remember the uploader retries three times")
	e.eq(strings.TrimSpace(e.gm(dir, "diff", "--cached", "--name-only")), "scratch/draft.md", "the user's staged work is untouched")
	e.lacks(e.gm(dir, "ls-tree", "-r", "--name-only", "HEAD"), "scratch/", "and was not swept into a hook's commit")
}

// ---- slash commands -----------------------------------------------------------------------------------------------

var (
	commandNames = []string{"memory-remember", "memory-decide", "memory-recall", "memory-status", "memory-list", "memory-promote", "memory-refine", "memory-forget", "memory-sync"}
	writerNames  = []string{"memory-remember", "memory-decide", "memory-promote", "memory-refine", "memory-forget", "memory-sync"}
)

// relCLI is how the (committable) command files refer to the CLI.
func relCLI() string { return "./.git/git-memory/" + core.WrapperName() }

func frontmatterOf(text string) map[string]string {
	out := map[string]string{}
	m := regexp.MustCompile(`(?s)^---\n(.*?)\n---`).FindStringSubmatch(text)
	if m == nil {
		return out
	}
	for _, l := range strings.Split(m[1], "\n") {
		k, v, _ := strings.Cut(l, ":")
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func TestAgentInitInstallsSlashCommands(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("cmds")
	out := e.agentInit(dir)
	e.has(out, "slash commands (Claude Code, Copilot, Codex, Cursor, Gemini CLI; git-excluded): /memory-remember /memory-decide /memory-recall /memory-status /memory-list /memory-promote /memory-refine /memory-forget /memory-sync")
	e.eq(e.status(dir), "", "commands must be git-excluded")

	for _, name := range commandNames {
		skill := e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md"))
		fm := frontmatterOf(skill)
		e.eq(fm["name"], name)
		e.ok(len(fm["description"]) > 20, name+" needs a description for the / menu")
		e.eq(fm["allowed-tools"], "Bash("+relCLI()+" *)", "the CLI is pre-approved for the turn, by its project-relative path")
		want := ""
		if core.Contains(writerNames, name) {
			want = "true" // commands that write are user-only
		}
		e.eq(fm["disable-model-invocation"], want, name)
		e.has(skill, "git-memory:generated")

		prompt := e.read(filepath.Join(dir, ".github/prompts", name+".prompt.md"))
		pf := frontmatterOf(prompt)
		e.eq(pf["agent"], "agent")
		e.ok(len(pf["description"]) > 20, name+" prompt needs a description")
		e.has(prompt, "`"+relCLI()+" ", "prompt files call the same relative launcher")
	}
	// no `!` injection: Claude Code aborts the whole skill when an injected command fails (missing CLI, empty repository),
	// so every command runs through the model, which can handle the failure.
	for _, name := range commandNames {
		e.lacks(e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md")), "!`", name+" must not use shell injection")
		// and every command says what to do when the CLI is missing: offer the install, never just stop
		e.has(e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md")), "install.sh", name+" must explain how to install")
		e.has(e.read(filepath.Join(dir, ".github/prompts", name+".prompt.md")), "ask whether you should do it for them", name)
	}
	e.lacks(e.read(filepath.Join(dir, ".claude/skills/memory-recall/SKILL.md")), "disable-model-invocation")
	e.ok(!e.exists(filepath.Join(dir, ".claude/skills/memory")), "must not shadow the built-in /memory")

	exclude := e.read(filepath.Join(dir, ".git/info/exclude"))
	e.match(exclude, `(?m)^/\.claude/skills/memory-remember/$`)
	e.match(exclude, `(?m)^/\.github/prompts/memory-remember\.prompt\.md$`)
}

func TestCommandsNeverOverwriteYourOwnFiles(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("cmds2")
	own := "---\nname: remember\ndescription: my own\n---\nMine.\n"
	e.write(filepath.Join(dir, ".claude/skills/memory-remember/SKILL.md"), own)
	e.write(filepath.Join(dir, ".github/prompts/memory-recall.prompt.md"), "my own recall prompt\n")

	out := e.agentInit(dir)
	e.has(out, "note: kept your own .claude/skills/memory-remember/SKILL.md")
	e.has(out, "note: kept your own .github/prompts/memory-recall.prompt.md")
	e.eq(e.read(filepath.Join(dir, ".claude/skills/memory-remember/SKILL.md")), own)
	e.eq(e.read(filepath.Join(dir, ".github/prompts/memory-recall.prompt.md")), "my own recall prompt\n")
	e.has(e.read(filepath.Join(dir, ".claude/skills/memory-decide/SKILL.md")), "git-memory:generated")

	// re-running init rewrites only the generated files
	before := e.read(filepath.Join(dir, ".claude/skills/memory-decide/SKILL.md"))
	e.agentInit(dir)
	e.eq(e.read(filepath.Join(dir, ".claude/skills/memory-decide/SKILL.md")), before)

	none := e.newRepo("cmds3")
	e.lacks(e.agentInit(none, "--no-commands"), "slash commands")
	e.ok(!e.exists(filepath.Join(none, ".claude/skills")) && !e.exists(filepath.Join(none, ".github/prompts")), "commands written despite --no-commands")

	onlyClaude := e.newRepo("cmds4")
	e.agentInit(onlyClaude, "claude")
	e.ok(e.exists(filepath.Join(onlyClaude, ".claude/skills/memory-recall/SKILL.md")) && !e.exists(filepath.Join(onlyClaude, ".github/prompts")), "agent init claude wrote Copilot files")
}

// init writes each agent's always-loaded instructions: the memory layout and how to read it. Own files are kept.
func TestAgentInitWritesInstructions(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("instr")
	out := e.agentInit(dir)
	e.has(out, "wrote .claude/rules/git-memory.md")
	e.has(out, "wrote .github/instructions/git-memory.instructions.md")

	claude := e.read(filepath.Join(dir, ".claude/rules/git-memory.md"))
	for _, want := range []string{"git-memory:generated", "memories/<kind>", "sessions/YYYY/MM", "global/", "./.git/git-memory/gm grep", "show HEAD:<path>"} {
		e.has(claude, want)
	}
	e.ok(!strings.Contains(claude, "{SLUG}") && !strings.Contains(claude, "{GM}") && !strings.Contains(claude, "§"), "placeholders left in the instructions")
	e.ok(strings.HasPrefix(claude, "<!--"), "Claude's rules file must not start with frontmatter (it would restrict where it applies)")
	e.ok(strings.HasPrefix(e.read(filepath.Join(dir, ".github/instructions/git-memory.instructions.md")), "---\napplyTo: \"**\"\n---"), "Copilot's file needs applyTo")

	// git-excluded, and the user's own file is never overwritten
	e.ok(!strings.Contains(e.git(dir, "status", "--porcelain"), "instructions") && !strings.Contains(e.git(dir, "status", "--porcelain"), "rules"), "instructions files show in git status")
	own := "my own rules\n"
	e.write(filepath.Join(dir, ".claude/rules/git-memory.md"), own)
	e.has(e.agentInit(dir), "note: kept your own .claude/rules/git-memory.md")
	e.eq(e.read(filepath.Join(dir, ".claude/rules/git-memory.md")), own)

	none := e.newRepo("instr2")
	e.lacks(e.agentInit(none, "--no-instructions"), "wrote .claude/rules")
	e.ok(!e.exists(filepath.Join(none, ".claude/rules")) && !e.exists(filepath.Join(none, ".github/instructions")), "written despite --no-instructions")
	only := e.newRepo("instr3")
	e.agentInit(only, "claude")
	e.ok(e.exists(filepath.Join(only, ".claude/rules/git-memory.md")) && !e.exists(filepath.Join(only, ".github/instructions")), "agent init claude wrote Copilot files")
}

// Every command line the generated files tell the agent to run must exist, and the flows they describe must work with
// nothing but git: that is the whole point of the design.
func TestEveryCommandLineInTheGeneratedFilesWorks(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("cmds5")

	// drift guard: each git subcommand the files mention is a real one, and agent subcommands are ours
	var bodies strings.Builder
	for _, n := range commandNames {
		bodies.WriteString(e.read(filepath.Join(dir, ".claude/skills", n, "SKILL.md")) + "\n")
	}
	gitCommands := e.git(dir, "help", "-a")
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(regexp.QuoteMeta(relCLI())+` ([a-z-]+)`).FindAllStringSubmatch(bodies.String(), -1) {
		sub := m[1]
		seen[sub] = true
		if core.Contains(ownWords, sub) {
			continue
		}
		e.ok(regexp.MustCompile(`(?m)^\s+`+sub+`\s`).MatchString(gitCommands), "the generated commands use `"+sub+"`, which is not a git command")
	}
	for _, want := range []string{"doctor", "promote", "grep", "show", "add", "commit", "rm", "log", "status"} {
		e.ok(seen[want], "the generated commands should use "+want)
	}

	// the flows, as an agent would run them through the launcher
	gm := func(a ...string) string { return e.sh(dir, launcher(dir), a...) }
	rows := map[string]string{}
	for _, l := range strings.Split(gm("doctor", "--report"), "\n") {
		if m := regexp.MustCompile(`^[✓!✗·] ([^:]+): (.*)$`).FindStringSubmatch(l); m != nil {
			rows[m[1]] = m[2]
		}
	}
	repo, proj := strings.Split(rows["Memory repository"], " (")[0], projectPrefix(rows["Project folder"])
	e.eq(repo, e.home)

	// /remember
	page := proj + "memories/decision/api-style.md"
	e.write(filepath.Join(repo, filepath.FromSlash(page)), "---\nkind: decision\ntier: semantic\ntags: []\ncreated: 2026-11-12T10:00:00Z\n---\n\n# API style\n\nPublic APIs are REST for external clients.\n")
	gm("add", "-A", "--", proj+"memories")
	gm("commit", "-q", "-m", "memory: API style", "--", proj+"memories")
	// /recall
	e.has(gm(append([]string{"grep", "-n", "-i", "-I", "-e", "rest", "--"}, strings.Fields(rows["Read folders"])...)...), "api-style.md")
	e.has(gm("show", "HEAD:"+page), "REST for external clients")
	// /memories
	e.has(gm("log", "--oneline", "-n", "10"), "memory: API style")
	e.has(gm("ls-files", "--", proj+"memories"), "api-style.md")
	// /promote
	gm("promote", page, "--to", "global")
	e.has(gm("ls-files", "--", "global"), "global/memories/decision/api-style.md")
	e.lacks(gm("ls-files", "--", proj+"memories"), "api-style.md")
	// /forget
	gm("rm", "-q", "global/memories/decision/api-style.md")
	gm("commit", "-q", "-m", "memory: forget API style", "--", "global/memories/decision/api-style.md")
	e.lacks(gm("ls-files"), "api-style.md")
}

func TestCommandFilesAreIdenticalOnEveryMachine(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	a, b := e.newRepo("portable-a"), e.newRepo("portable-b")
	homeA := e.home
	e.agentInit(a)
	var homeB string
	e.withMachine(func() { homeB = e.home; e.agentInit(b) }) // another machine: other paths, other memory repository

	files := func(dir string) map[string]string {
		out := map[string]string{}
		for _, name := range commandNames {
			for _, rel := range []string{".claude/skills/" + name + "/SKILL.md", ".github/prompts/" + name + ".prompt.md"} {
				out[rel] = e.read(filepath.Join(dir, rel))
			}
		}
		out[".claude/settings.local.json"] = e.read(filepath.Join(dir, ".claude/settings.local.json"))
		out[".github/hooks/git-memory.json"] = e.read(filepath.Join(dir, ".github/hooks/git-memory.json"))
		return out
	}
	fa, fb := files(a), files(b)
	e.eq(len(fa), len(commandNames)*2+2)
	e.eq(core.MarshalJSON(fa, false), core.MarshalJSON(fb, false), "generated files must not depend on where the project or the memory repository are")

	home, _ := os.UserHomeDir()
	personal := []string{a, b, home, e.tmp, filepath.Dir(binPath), homeA, homeB, "GIT_MEMORY_HOME"}
	abs := regexp.MustCompile(`(^|["'\s=:])/(home|Users|tmp|var|usr)/`)
	for rel, text := range fa {
		for _, s := range personal {
			e.lacks(text, s, rel+" contains a personal path")
		}
		e.ok(!abs.MatchString(text), rel+" holds an absolute path")
	}
	for _, rel := range []string{".claude/skills/memory-recall/SKILL.md", ".github/prompts/memory-recall.prompt.md"} {
		e.has(fa[rel], relCLI(), rel+" should use the relative launcher")
		e.has(fa[rel], "does not exist")
	}
	e.eq(at(e.readJSON(filepath.Join(a, ".claude/settings.local.json")), "hooks", "Stop", 0, "hooks", 0, "command"), `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" agent hook claude Stop`)
	copilot := at(e.readJSON(filepath.Join(a, ".github/hooks/git-memory.json")), "hooks", "agentStop", 0)
	e.eq(at(copilot, "command"), "./.git/git-memory/gm agent hook copilot agentStop")
	e.eq(at(copilot, "cwd"), ".")

	// the launcher (under .git, never committed) is the only place with machine paths
	wrapper := e.read(filepath.Join(a, ".git/git-memory/gm"))
	e.match(wrapper, `exec '[^']*git-memory[^']*' "\$@"`)
	e.lacks(wrapper, "GIT_MEMORY", "the launcher bakes in no environment")
	e.lacks(wrapper, a, "the launcher holds no project path")

	// regenerating leaves them byte-identical, and committing them works: tracked files are unaffected by the local exclude
	e.agentInit(a)
	e.eq(core.MarshalJSON(files(a), false), core.MarshalJSON(fa, false))
	e.git(a, "add", "-f", ".claude/skills", ".github/prompts")
	e.git(a, "commit", "-qm", "share the memory commands")
	e.agentInit(a)
	e.eq(e.status(a), "", "init must not modify committed command files")
}

// ---- the agents' own memories: doctor lists them, promote brings them in -------------------------------------------

// testNonAlnum is how Claude Code turns a project path into the name of its memory folder.
var testNonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeMemoryOf is where Claude Code keeps its auto memory for the project at root, on the test machine.
func (e *env) claudeMemoryOf(root string) string {
	return filepath.Join(e.user, ".claude", "projects", testNonAlnum.ReplaceAllString(root, "-"), "memory")
}

// copilotMemoryOf is where VS Code Copilot Chat keeps its memories: the user-level folder, and the one of the workspace
// that is root (found through the workspace.json VS Code keeps beside it).
func (e *env) copilotMemoryOf(root string) (user, workspace string) {
	cfg := filepath.Join(e.user, ".config")
	if runtime.GOOS == "darwin" {
		cfg = filepath.Join(e.user, "Library", "Application Support")
	}
	userData := filepath.Join(cfg, "Code", "User")
	user = filepath.Join(userData, "globalStorage", "github.copilot-chat", "memory-tool", "memories")
	ws := filepath.Join(userData, "workspaceStorage", "0a1b2c3d")
	e.write(filepath.Join(ws, "workspace.json"), `{"folder":"file://`+filepath.ToSlash(root)+`"}`)
	return user, filepath.Join(ws, "github.copilot-chat", "memory-tool", "memories")
}

const claudeFeedback = "---\nname: use-tailwind\ndescription: The user prefers Tailwind over CSS modules\ntype: feedback\n---\nUse Tailwind for all styling, never CSS modules. token=abcdefghijkl1234\n\n**Why:** it matches the design system.\n"

func TestDoctorListsTheAgentsOwnMemories(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("seen")

	// nothing there yet
	_, out := e.doctor(dir)
	e.has(out, "· Claude Code memory: none found")
	e.has(out, "· Copilot memory (this workspace): none found")
	e.has(out, "· Copilot memory (user): none found")
	e.lacks(out, "Promote:", "no hint when there is nothing to promote")

	claude := e.claudeMemoryOf(dir)
	e.write(filepath.Join(claude, "feedback_tailwind.md"), claudeFeedback)
	e.write(filepath.Join(claude, "user_role.md"), "---\nname: user-role\ntype: user\n---\nA backend engineer.\n")
	e.write(filepath.Join(claude, "MEMORY.md"), "- [Tailwind](feedback_tailwind.md)\n") // Claude's own index: not a memory
	user, workspace := e.copilotMemoryOf(dir)
	e.write(filepath.Join(workspace, "workspace-notes.md"), "# Build\n\nUse make.\n")
	e.write(filepath.Join(user, "preferences.md"), "# Tabs\n\nI prefer tabs.\n")

	rows, out := e.doctor(dir)
	e.has(rows["Claude Code memory"], claude+" (2 memories: ")
	e.has(rows["Claude Code memory"], "feedback_tailwind.md")
	e.has(rows["Claude Code memory"], "user_role.md")
	e.lacks(rows["Claude Code memory"], "MEMORY.md")
	e.has(rows["Copilot memory (this workspace)"], workspace+" (1 memory: workspace-notes.md)")
	e.has(rows["Copilot memory (user)"], user+" (1 memory: preferences.md)")
	e.has(out, "git-memory promote claude:<file> --to project|global")
	e.eq(e.status(dir), "", "reading the agents' folders adds nothing to the project")

	// outside a project only the user-level Copilot folder can be found
	_, outside := e.doctor(e.mkdir("nowhere"))
	e.has(outside, "✓ Copilot memory (user): "+user)
	e.lacks(outside, "Claude Code memory")
}

func TestPromoteCopiesAClaudeMemoryIntoTheProjectOrGlobal(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("promo")
	proj := e.project(dir)
	claude := e.claudeMemoryOf(dir)
	e.write(filepath.Join(claude, "feedback_tailwind.md"), claudeFeedback)
	e.write(filepath.Join(claude, "MEMORY.md"), "- index\n")

	out := e.gm(dir, "promote", "claude:feedback_tailwind.md", "--to", "project")
	e.eq(strings.TrimSpace(out), "promoted claude:feedback_tailwind.md → "+proj+"memories/decision/use-tailwind.md")
	page := e.gm(dir, "show", "HEAD:"+proj+"memories/decision/use-tailwind.md")
	e.has(page, "kind: decision", "Claude's feedback is guidance on how to work: a decision here")
	e.has(page, "tags: [from-claude]")
	e.has(page, "source: claude:feedback_tailwind.md")
	e.has(page, "# use tailwind")
	e.has(page, "The user prefers Tailwind over CSS modules")
	e.has(page, "Use Tailwind for all styling, never CSS modules.")
	e.lacks(page, "abcdefghijkl1234", "secrets are redacted on the way in")
	e.has(e.gm(dir, "log", "--format=%s", "-n", "1"), "memory: remember use tailwind")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "", "promoted and committed")

	// the original is exactly where Claude left it
	e.eq(e.read(filepath.Join(claude, "feedback_tailwind.md")), claudeFeedback)

	// promoting twice is refused, not overwritten; a shorter reference works; the other target is allowed
	_, errOut, code := e.gmRaw(dir, "promote", "claude:feedback_tailwind", "--to", "project")
	e.eq(code, 1)
	e.has(errOut, "was already promoted")
	out = e.gm(dir, "promote", "claude:tailwind", "--to", "global")
	e.has(out, "→ global/memories/decision/use-tailwind.md")

	// what was promoted reaches the next session like any other memory
	start := ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "pr1pr1pr1"}))
	e.has(start, "use tailwind")

	// unknown and ambiguous references say so
	_, errOut, code = e.gmRaw(dir, "promote", "claude:nothing-like-this", "--to", "project")
	e.eq(code, 1)
	e.has(errOut, "no Claude Code memory matches")
}

func TestPromoteCopiesCopilotMemoriesAndPlainFiles(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("promo2")
	proj := e.project(dir)
	user, workspace := e.copilotMemoryOf(dir)
	e.write(filepath.Join(workspace, "build.md"), "# Build with make\n\nRun make before the tests.\n")
	e.write(filepath.Join(user, "tabs.md"), "# I prefer tabs\n\nAlways tabs.\n")
	plain := filepath.Join(e.mkdir("notes"), "retries.md")
	e.write(plain, "---\nkind: procedure\ntags: [net]\n---\n\n# Retries use backoff\n\nDouble the delay each time.\n")

	e.has(e.gm(dir, "promote", "copilot:build", "--to", "project"), "→ "+proj+"memories/note/build-with-make.md")
	e.has(e.gm(dir, "promote", "copilot-user:tabs.md", "--to", "global"), "→ global/memories/note/i-prefer-tabs.md")
	e.has(e.gm(dir, "promote", plain, "--to", "project"), "→ "+proj+"memories/procedure/retries-use-backoff.md")
	page := e.gm(dir, "show", "HEAD:"+proj+"memories/procedure/retries-use-backoff.md")
	e.has(page, "tags: [net, from-file]")
	e.has(page, "source: file:retries.md")
	e.has(e.gm(dir, "show", "HEAD:"+proj+"memories/note/build-with-make.md"), "tags: [from-copilot]")
	e.has(e.gm(dir, "show", "HEAD:global/memories/note/i-prefer-tabs.md"), "tags: [from-copilot-user]")

	// several at once: each is reported, a failure does not stop the others, and the exit code says so
	e.write(filepath.Join(workspace, "second.md"), "# Second\n\nAnother.\n")
	stdout, errOut, code := e.gmRaw(dir, "promote", "copilot:second", "copilot:missing", "--to", "global")
	e.eq(code, 1)
	e.has(stdout, "→ global/memories/note/second.md")
	e.has(errOut, "no Copilot memory (this workspace) matches")
}

func TestPromoteMovesPagesBetweenTheProjectAndGlobal(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("move")
	proj := e.project(dir)
	page := "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Public APIs are REST\n\nExternal clients.\n"
	e.commitPage(dir, proj+"memories/decision/use-rest.md", page)

	out := e.gm(dir, "promote", proj+"memories/decision/use-rest.md", "--to", "global")
	e.has(out, "promoted "+proj+"memories/decision/use-rest.md → global/memories/decision/use-rest.md")
	e.has(e.tracked(dir), "global/memories/decision/use-rest.md")
	e.lacks("\n"+e.tracked(dir), "\n"+proj+"memories/decision/use-rest.md")
	e.has(e.gm(dir, "log", "--format=%s", "-n", "1"), "memory: promote Public APIs are REST to global")
	e.has(e.gm(dir, "log", "-n", "1", "--name-status", "--format="), "R", "recorded as a rename, so history follows the page")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")

	// the other way, by a part of the name; the same folder is refused
	e.has(e.gm(dir, "promote", "use-rest", "--to", "project"), "→ "+proj+"memories/decision/use-rest.md")
	_, errOut, code := e.gmRaw(dir, "promote", "use-rest", "--to", "project")
	e.eq(code, 1)
	e.has(errOut, "is already in "+proj)

	// a page that was never committed moves too, and a destination that exists is not overwritten
	e.write(filepath.Join(e.home, proj, "memories/note/draft.md"), "# Draft\n\nnot committed yet\n")
	e.has(e.gm(dir, "promote", "draft", "--to", "global"), "→ global/memories/note/draft.md")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")
	e.write(filepath.Join(e.home, proj, "memories/note/draft.md"), "# Draft again\n")
	_, errOut, code = e.gmRaw(dir, "promote", "draft", "--to", "global")
	e.eq(code, 1)
	e.has(errOut, "matches more than one page", "a bare name that matches two pages is not guessed")
	_, errOut, code = e.gmRaw(dir, "promote", proj+"memories/note/draft.md", "--to", "global")
	e.eq(code, 1)
	e.has(errOut, "global/memories/note/draft.md already exists")
}

func TestPromoteExplainsItselfWhenUsedWrong(t *testing.T) {
	e := newEnv(t)
	dir := e.newProject("wrong")
	for _, args := range [][]string{{"promote"}, {"promote", "x.md"}, {"promote", "x.md", "--to", "nowhere"}, {"promote", "--to", "global"}} {
		_, errOut, code := e.gmRaw(dir, args...)
		e.eq(code, 1, strings.Join(args, " "))
		e.has(errOut, "usage: git-memory promote <source>... --to project|scope|global")
	}
	// it needs a project: that is where "project" is and where Claude's memory for it is found
	_, errOut, code := e.gmRaw(e.mkdir("not-a-project"), "promote", "x.md", "--to", "global")
	e.eq(code, 1)
	e.has(errOut, "promote runs inside a project")
}

func TestDemoteNarrowsAGlobalPageToTheProject(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("demo")
	proj := e.project(dir)
	page := "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Public APIs are REST\n\nExternal clients.\n"
	e.commitPage(dir, "global/memories/decision/use-rest.md", page)

	out := e.gm(dir, "demote", "use-rest")
	e.has(out, "demoted global/memories/decision/use-rest.md → "+proj+"memories/decision/use-rest.md")
	e.has(e.tracked(dir), proj+"memories/decision/use-rest.md")
	e.lacks(e.tracked(dir), "global/memories/decision/use-rest.md")
	e.has(e.gm(dir, "log", "--format=%s", "-n", "1"), "memory: demote Public APIs are REST to project")
	e.has(e.gm(dir, "log", "-n", "1", "--name-status", "--format="), "R", "recorded as a rename")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")

	// it is the inverse of promote: the page lands where it started
	e.has(e.gm(dir, "promote", "use-rest", "--to", "global"), "→ global/memories/decision/use-rest.md")
	e.has(e.gm(dir, "demote", "global/memories/decision/use-rest.md"), "→ "+proj+"memories/decision/use-rest.md")

	// nothing is narrower than a project
	_, errOut, code := e.gmRaw(dir, "demote", proj+"memories/decision/use-rest.md")
	e.eq(code, 1)
	e.has(errOut, "is already in this project's own memories: nothing is narrower than a project")

	// a page that was never committed moves too; when the same name exists in both folders the global one is the target
	e.write(filepath.Join(e.home, "global/memories/note/draft.md"), "# Draft\n\nnot committed\n")
	e.has(e.gm(dir, "demote", "draft"), "→ "+proj+"memories/note/draft.md")
	e.commitPage(dir, "global/memories/note/draft.md", "# Draft v2\n")
	_, errOut, code = e.gmRaw(dir, "demote", "draft")
	e.eq(code, 1)
	e.has(errOut, proj+"memories/note/draft.md already exists", "the global page is picked, and it does not overwrite")

	// several at once: failures are reported, the rest still move
	e.commitPage(dir, "global/memories/fact/a.md", "# A\n\na\n")
	stdout, errOut, code := e.gmRaw(dir, "demote", "global/memories/fact/a.md", "no-such-page")
	e.eq(code, 1)
	e.has(stdout, "→ "+proj+"memories/fact/a.md")
	e.has(errOut, "no page in the memory repository matches")
}

func TestDemoteOnlyTakesPagesOfTheMemoryRepository(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("demo2")
	plain := filepath.Join(e.mkdir("notes"), "x.md")
	e.write(plain, "# X\n\nx\n")

	_, errOut, code := e.gmRaw(dir, "demote", "claude:anything.md")
	e.eq(code, 1)
	e.has(errOut, "is an agent's own memory: demote narrows pages of the memory repository (git-memory promote brings it in)")
	_, errOut, code = e.gmRaw(dir, "demote", plain)
	e.eq(code, 1)
	e.has(errOut, "is not a page of the memory repository")
	for _, args := range [][]string{{"demote"}} {
		_, errOut, code = e.gmRaw(dir, args...)
		e.eq(code, 1)
		e.has(errOut, "usage: git-memory demote <page>...")
	}
	_, errOut, code = e.gmRaw(e.mkdir("not-a-project"), "demote", "x")
	e.eq(code, 1)
	e.has(errOut, "demote runs inside a project")
}

// A page that was staged (git-memory add) but never committed can be moved like any other.
func TestMovingAStagedButUncommittedPage(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("staged")
	proj := e.project(dir)

	e.write(filepath.Join(e.home, "global/memories/note/staged.md"), "# Staged\n\nadded, not committed\n")
	e.gm(dir, "add", "global")
	e.has(e.gm(dir, "demote", "staged"), "→ "+proj+"memories/note/staged.md")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "", "no half-staged leftovers")
	e.has(e.tracked(dir), proj+"memories/note/staged.md")
	e.lacks(e.tracked(dir), "global/memories/note/staged.md")

	e.write(filepath.Join(e.home, proj, "memories/note/staged2.md"), "# Staged two\n\nadded, not committed\n")
	e.gm(dir, "add", "--", proj+"memories")
	e.has(e.gm(dir, "promote", "staged2", "--to", "global"), "→ global/memories/note/staged2.md")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")
}

func TestTheVersionIsZeroZeroOneUntilTheFirstRelease(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("version")
	e.eq(strings.TrimSpace(e.gm(dir, "agent", "version")), "git-memory 0.0.1")
	// `version` and `--version` are git's own, and say so
	out, _, code := e.gmRaw(dir, "--version")
	e.eq(code, 0)
	e.match(out, `^git version `)
}

// The memory repository is the .git-memory folder of the folder you are in: nothing searches upwards, and nothing but
// init (or clone, or agent init) makes one.
func TestTheMemoryRepositoryIsTheFolderYouAreInAndNothingIsSearched(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("here")
	sub := filepath.Join(dir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// a subfolder of a project that has a memory repository does not have one
	_, errOut, code := e.gmRaw(sub, "status")
	e.eq(code, 1)
	e.has(errOut, "no memory repository at "+filepath.Join(sub, ".git-memory"))
	e.ok(!e.exists(filepath.Join(sub, ".git-memory")), "a command created a memory repository")

	// and neither does a folder under a home directory that has one
	e.git(e.user, "init", "-q", "-b", "main", filepath.Join(e.user, ".git-memory"))
	outside := e.mkdir("outside")
	_, errOut, code = e.gmRaw(outside, "log")
	e.eq(code, 1)
	e.has(errOut, "no memory repository at "+filepath.Join(outside, ".git-memory"))

	// init is "here", also in a subfolder of a project, where it is kept out of git status like the one at the root
	e.gm(sub, "init", "-q")
	e.ok(e.exists(filepath.Join(sub, ".git-memory", ".git")), "init did not create it here")
	e.eq(e.status(dir), "")
	e.eq(strings.TrimSpace(e.gm(sub, "rev-parse", "--show-toplevel")), filepath.Join(sub, ".git-memory"))

	// the hooks of a project never make one: without it they log and record nothing in a repository that is not there
	e.skipOnWindows()
	proj := e.newRepoNoMemory("unwired")
	e.agentInit(proj) // wiring a project is the other way to get one
	e.ok(e.exists(filepath.Join(proj, ".git-memory", ".git")), "agent init did not create the project's memory repository")
	e.eq(e.status(proj), "")
	e.eq(len(strings.TrimSpace(e.git(proj, "ls-files", "--others", "--exclude-standard"))), 0)
}

func TestListShowsEveryMemoryGroupedByWhereItLives(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("lister")
	proj := e.project(dir)

	// in the memory repository: this project's (one committed, one not yet, one retired), a global one, and a session page
	e.commitPage(dir, proj+"memories/decision/use-rest.md", "---\nkind: decision\ntier: semantic\n---\n\n# Public APIs are REST\n\nBecause clients.\n")
	e.write(filepath.Join(e.home, proj, "memories/fact/port.md"), "---\nkind: fact\n---\n\n# The API listens on 8080\n\nx\n")
	e.commitPage(dir, proj+"memories/note/old.md", "---\nkind: note\nstatus: superseded\n---\n\n# An old note\n\nx\n")
	e.commitPage(dir, "global/core/pnpm.md", "---\nkind: decision\ntier: core\n---\n\n# Use pnpm\n\nNever npm.\n")
	e.write(filepath.Join(e.home, proj, "sessions/2026/11/2026-11-12-claude-ab12cd34.md"), "---\nkind: session\n---\n\n# A session\n\nx\n")
	// and in the agents' own folders
	e.write(filepath.Join(e.claudeMemoryOf(dir), "feedback_tailwind.md"), claudeFeedback)
	e.write(filepath.Join(e.claudeMemoryOf(dir), "MEMORY.md"), "- [x](feedback_tailwind.md)\n")
	user, workspace := e.copilotMemoryOf(dir)
	e.write(filepath.Join(workspace, "build.md"), "# Build with make\n\nRun make.\n")
	e.write(filepath.Join(user, "tabs.md"), "# I prefer tabs\n\nAlways tabs.\n")

	out := e.gm(dir, "list")
	heading := ".git-memory · this project"
	if proj != "" {
		heading += " (" + strings.TrimSuffix(proj, "/") + ")"
	}
	e.has(out, heading+" · 3 memories")
	e.match(out, `(?m)^  decision  Public APIs are REST +`+regexp.QuoteMeta(proj)+`memories/decision/use-rest\.md$`)
	e.has(out, proj+"memories/fact/port.md", "a memory that is not committed yet is listed")
	e.match(out, `(?m)memories/note/old\.md +— superseded$`)
	e.lacks(out, "A session", "session pages are not memories")
	e.has(out, ".git-memory · global · 1 memory")
	e.match(out, `(?m)^  decision  Use pnpm +global/core/pnpm\.md +— pinned$`)
	e.has(out, "Claude Code memory ("+e.claudeMemoryOf(dir)+") · 1 memory")
	e.match(out, `(?m)^  decision  use-tailwind +claude:feedback_tailwind\.md +— The user prefers Tailwind over CSS modules$`)
	e.lacks(out, "MEMORY.md", "Claude's index is not a memory")
	e.match(out, `(?m)^  note  Build with make +copilot:build\.md$`)
	e.match(out, `(?m)^  note  I prefer tabs +copilot-user:tabs\.md$`)

	// a group on its own
	only := e.gm(dir, "list", "claude")
	e.has(only, "claude:feedback_tailwind.md")
	e.lacks(only, "Use pnpm")
	e.lacks(only, "copilot:")
	e.has(e.gm(dir, "list", "copilot"), "copilot-user:tabs.md")
	e.lacks(e.gm(dir, "list", "repo"), "claude:")

	// sessions: only when asked for, newest first, optionally narrowed to a year, a month or a day
	e.write(filepath.Join(e.home, proj, "sessions/2026/11/2026-11-13-copilot-ee55ff66.md"), "---\nkind: session\nagent: copilot\n---\n\n# Later session\n\nx\n")
	e.write(filepath.Join(e.home, proj, "sessions/2025/12/2025-12-31-claude-00aa11bb.md"), "---\nkind: session\nagent: claude\n---\n\n# Last year\n\nx\n")
	all := e.gm(dir, "list", "sessions")
	e.has(all, ".git-memory · sessions ("+proj+"sessions) · 3 sessions")
	e.match(all, `(?m)^  session  Later session +`+regexp.QuoteMeta(proj)+`sessions/2026/11/2026-11-13-copilot-ee55ff66\.md +— 2026-11-13 · copilot$`)
	e.ok(strings.Index(all, "Later session") < strings.Index(all, "A session") && strings.Index(all, "A session") < strings.Index(all, "Last year"), "newest first:\n"+all)
	e.lacks(all, "Use pnpm", "sessions on their own list nothing else")
	e.lacks(all, "claude:")
	day := e.gm(dir, "list", "sessions", "2026-11-12")
	e.has(day, ".git-memory · sessions 2026-11-12 (")
	e.has(day, "A session")
	e.lacks(day, "Later session")
	e.has(e.gm(dir, "list", "2026-11"), "· 2 sessions")
	e.has(e.gm(dir, "list", "2025"), "Last year")
	e.has(e.gm(dir, "list", "2024"), "· empty")
	e.lacks(e.gm(dir, "list", "claude", "2026-11-13"), "A session", "a date only narrows the sessions")
	e.has(e.gm(dir, "list"), "· 3 memories")
	e.lacks(e.gm(dir, "list"), "Later session")

	// every row's name is one promote takes
	e.has(e.gm(dir, "promote", "claude:feedback_tailwind.md", "--to", "project"), "→ "+proj+"memories/decision/use-tailwind.md")

	// it only reads
	e.eq(e.status(dir), "")
	_, errOut, code := e.gmRaw(dir, "list", "everything")
	e.eq(code, 1)
	e.has(errOut, "usage: git-memory list")

	// outside a project: no repository there, and only what does not depend on a project
	outside := e.mkdir("outside")
	e.has(e.gm(outside, "list", "sessions"), "sessions: they belong to a project")
	nothing := e.gm(outside, "list")
	e.has(nothing, ".git-memory: none at "+filepath.Join(outside, ".git-memory"))
	e.has(nothing, "copilot-user:tabs.md")
	e.lacks(nothing, "claude:")
}

// Session pages are the local record of each agent session: the memory repository's .gitignore keeps them out of git,
// while the briefing, `list sessions` and `doctor` still read them from disk.
func TestSessionPagesAreIgnoredByGit(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	const rule = "/sessions/"

	// the user already has a .gitignore in the memory repository: it is kept, and the rule is added to it
	dir := e.newRepoNoMemory("ignored")
	e.home = filepath.Join(dir, ".git-memory")
	e.gm(dir, "init", "-q")
	e.eq(strings.Count(e.read(filepath.Join(e.home, ".gitignore")), rule), 1, "git-memory init writes the rule")
	e.write(filepath.Join(e.home, ".gitignore"), "*.tmp\n")
	e.agentInit(dir)
	ignore := e.read(filepath.Join(e.home, ".gitignore"))
	e.has(ignore, "*.tmp")
	e.eq(strings.Count(ignore, rule), 1)
	e.has(e.gm(dir, "log", "--format=%s"), "memory: keep session pages out of git")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain", "--", ".gitignore")), "", ".gitignore is committed with the rule")
	e.agentInit(dir) // idempotent: nothing is added or committed twice
	e.eq(strings.Count(e.read(filepath.Join(e.home, ".gitignore")), rule), 1)
	e.eq(strings.Count(e.gm(dir, "log", "--format=%s"), "keep session pages out of git"), 1)

	// a session's page is written, but git does not see it
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "ig1ig1ig1"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "ig1ig1ig1", "prompt": "Add a health endpoint to the API"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "ig1ig1ig1"})
	page, text := e.sessionPage("claude-ig1ig1ig")
	e.has(text, "Add a health endpoint")
	_, _, code := e.gmRaw(dir, "check-ignore", "-q", page)
	e.eq(code, 0, "git must ignore the session page")
	e.lacks(e.tracked(dir), "sessions/")
	e.gm(dir, "add", ".") // even a blanket add leaves it out
	e.lacks(e.gm(dir, "diff", "--cached", "--name-only"), "sessions/")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")

	// and everything that reads sessions still finds it
	e.has(e.gm(dir, "list", "sessions"), "ig1ig1ig")
	e.has(ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "ig2ig2ig2"})), "Recent sessions")
	rows, _ := e.doctor(dir)
	e.has(rows["Pages"], "1 session")

	// the rule is put back by the hooks if it goes missing (a repository cloned without it, a deleted file)
	e.write(filepath.Join(e.home, ".gitignore"), "*.tmp\n")
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "ig2ig2ig2", "prompt": "another question"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": "ig2ig2ig2", "last_assistant_message": "done"})
	e.eq(strings.Count(e.read(filepath.Join(e.home, ".gitignore")), rule), 1)
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "")
}

// `agent init` wires the agents you name, `all` is every one, and naming none is an error.
func TestAgentInitWiresTheAgentsYouName(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	claudeFiles := []string{".claude/settings.local.json", ".claude/skills/memory-recall/SKILL.md", ".claude/rules/git-memory.md"}
	copilotFiles := []string{".github/hooks/git-memory.json", ".github/prompts/memory-recall.prompt.md", ".github/instructions/git-memory.instructions.md"}
	has := func(dir string, files []string) bool {
		for _, f := range files {
			if !e.exists(filepath.Join(dir, f)) {
				return false
			}
		}
		return true
	}
	none := func(dir string, files []string) bool {
		for _, f := range files {
			if e.exists(filepath.Join(dir, f)) {
				return false
			}
		}
		return true
	}

	// one agent: its hooks, commands and instructions, and nothing of the other's
	dir := e.newRepoNoMemory("pick")
	out := e.gm(dir, "agent", "init", "copilot")
	e.has(out, "wrote .github/hooks/git-memory.json")
	e.lacks(out, ".claude")
	e.ok(has(dir, copilotFiles) && none(dir, claudeFiles), "agent init copilot must wire Copilot only")
	e.eq(e.status(dir), "")
	e.eq(len(at(e.readJSON(filepath.Join(dir, ".github/hooks/git-memory.json")), "hooks").(map[string]any)), 6)

	// doctor: the other agent was not asked for, so it is not a warning
	_, doc := e.doctor(dir)
	e.has(doc, "✓ Copilot hooks: 6 events")
	e.has(doc, "· Claude Code hooks: not set up (git-memory agent init claude)")
	e.has(doc, "✓ Slash commands: Claude Code 0/9, Copilot 9/9")

	// adding the other one later keeps the first
	e.gm(dir, "agent", "init", "claude")
	e.ok(has(dir, copilotFiles) && has(dir, claudeFiles), "both agents wired after naming them one at a time")
	_, doc = e.doctor(dir)
	e.has(doc, "✓ Claude Code hooks: 7 events")
	e.has(doc, "✓ Copilot hooks: 6 events")

	// naming both, in any order and with repeats, is the same as all
	both := e.newRepoNoMemory("both")
	e.gm(both, "agent", "init", "copilot", "claude", "copilot")
	e.ok(has(both, claudeFiles) && has(both, copilotFiles), "naming both agents")
	every := e.newRepoNoMemory("every")
	e.gm(every, "agent", "init", "all")
	e.ok(has(every, claudeFiles) && has(every, copilotFiles), "all means every agent")

	// no agent named is an error that says what to type, and nothing is written (not even the memory repository)
	bare := e.newRepoNoMemory("bare")
	_, errOut, code := e.gmRaw(bare, "agent", "init")
	e.eq(code, 1)
	e.has(errOut, "say which agent to set up: git-memory agent init <claude|copilot|codex|cursor|gemini|all>")
	_, errOut, code = e.gmRaw(bare, "agent", "init", "--no-commands")
	e.eq(code, 1)
	e.has(errOut, "say which agent to set up")
	e.ok(none(bare, claudeFiles) && none(bare, copilotFiles) && !e.exists(filepath.Join(bare, ".git-memory")), "something was written with no agent named")

	// a name that is not an agent is refused before anything is written
	typo := e.newRepoNoMemory("typo")
	_, errOut, code = e.gmRaw(typo, "agent", "init", "windsurf")
	e.eq(code, 1)
	e.has(errOut, `unknown agent "windsurf" (choose: claude, copilot, codex, cursor, gemini, all)`)
	e.ok(none(typo, claudeFiles) && none(typo, copilotFiles) && !e.exists(filepath.Join(typo, ".git-memory")), "something was written for an unknown agent")
	_, errOut, code = e.gmRaw(typo, "agent", "init", "claude", "windsurf")
	e.eq(code, 1)
	e.ok(none(typo, claudeFiles), "one bad name must stop the whole command")

	// the flags the argument replaced say so, instead of being ignored
	_, errOut, code = e.gmRaw(typo, "agent", "init", "--no-claude")
	e.eq(code, 1)
	e.has(errOut, "--no-claude is gone: name the agents you want instead, e.g. git-memory agent init copilot")
	_, errOut, code = e.gmRaw(typo, "agent", "init", "--no-copilot")
	e.eq(code, 1)
	e.has(errOut, "git-memory agent init claude")
}
