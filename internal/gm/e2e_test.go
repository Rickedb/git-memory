package gm

// End-to-end tests: the real binary against real git repos in temp dirs. TestMain builds it once; every test then
// drives it exactly as an agent or a person would (init, hooks on stdin, the installed launcher, the CLI).

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
	t    *testing.T
	tmp  string // every directory of the test lives here
	home string // GIT_MEMORY_HOME of the "machine" currently running commands
	n    int
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
	e.home = e.mkdir("gm-home")
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
	cmd.Env = append(os.Environ(), "GIT_MEMORY_HOME="+e.home, "GIT_TERMINAL_PROMPT=0")
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

// initCLI runs `git-memory init …` with the built binary (what a user runs once per project).
func (e *env) initCLI(dir string, args ...string) string {
	e.t.Helper()
	return e.sh(dir, binPath, append([]string{"init"}, args...)...)
}

func launcher(dir string) string { return filepath.Join(dir, ".git", "git-memory", wrapperName()) }

// gm runs the installed launcher, the way agents run it.
func (e *env) gm(dir string, args ...string) string {
	e.t.Helper()
	return e.sh(dir, launcher(dir), args...)
}

// gmFail runs the launcher and returns stderr and the exit code (it may fail).
func (e *env) gmFail(dir string, args ...string) (string, int) {
	_, errOut, code := e.run(dir, launcher(dir), args...)
	return errOut, code
}

func (e *env) hookRaw(dir, agent, event string, payload map[string]any) string {
	e.t.Helper()
	p := map[string]any{"cwd": dir}
	for k, v := range payload {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	out, errOut, code := e.runIn(dir, string(raw), launcher(dir), "hook", agent, event)
	if code != 0 {
		e.t.Fatalf("hook %s %s: exit %d\n%s", agent, event, code, errOut)
	}
	return out
}

// hook fires a hook and returns what it printed (nil when it printed nothing).
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

func (e *env) newRepo(name string) string {
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

func (e *env) cloneOf(remote string) string {
	d := e.mkdir("gm-verify")
	e.git(d, "clone", "-q", remote, d)
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

// withHome runs fn as another machine: another GIT_MEMORY_HOME.
func (e *env) withHome(home string, fn func()) {
	prev := e.home
	e.home = home
	defer func() { e.home = prev }()
	fn()
}

func (e *env) freshHome() string { return e.mkdir("gm-home") }

func (e *env) personalDir() string { return filepath.Join(e.home, "personal") }

// shared opts into a shared project store on a branch and makes it this machine's default, like a team repo would.
func (e *env) shared(dir string) {
	e.initCLI(dir, "--branch")
	e.gm(dir, "scopes", "default", "project", "--local")
	e.gm(dir, "scopes", "sessions", "project", "--local")
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

func names(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// ---- init -------------------------------------------------------------------------------------------------------

func TestPlainInitIsPersonalOnly(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("personal")
	e.initCLI(dir)

	// zero footprint: no setup file, no memory branch, no checkout, nothing for git status
	e.eq(e.status(dir), "")
	e.ok(!e.exists(filepath.Join(dir, "git-memory.json")), "setup file written")
	e.ok(!e.exists(filepath.Join(dir, ".git-memory")), "checkout created")
	e.eq(strings.TrimSpace(e.git(dir, "branch", "--list", "memory")), "")
	e.ok(!e.exists(filepath.Join(dir, ".gitignore")), ".gitignore touched")
	e.ok(!e.exists(e.personalDir()), "the personal store is created on first write")

	// hooks are wired (git-excluded)
	claude := e.readJSON(filepath.Join(dir, ".claude/settings.local.json"))
	e.match(at(claude, "hooks", "SessionStart", 0, "hooks", 0, "command").(string), `hook claude SessionStart`)
	copilot := e.readJSON(filepath.Join(dir, ".github/hooks/git-memory.json"))
	e.eq(at(copilot, "version"), 1)
	e.match(at(copilot, "hooks", "agentStop", 0, "command").(string), `hook copilot agentStop`)

	// memories default to the personal scope; the project scope is off until a store is chosen
	e.match(e.gm(dir, "remember", "I prefer tabs.", "--title", "Tabs"), `^remembered personal:memories/note/tabs\.md`)
	e.match(e.gm(dir, "list"), `\[\{personal\} note\] Tabs`)
	e.match(e.gm(dir, "scopes"), `project\s+Project \(shared\)\s+not set up`)
	stderr, code := e.gmFail(dir, "remember", "x", "--scope", "project")
	e.eq(code, 1)
	e.has(stderr, "project scope has no shared store yet: run `git-memory init --store <repo>`")
	e.eq(e.status(dir), "")

	e.initCLI(dir) // idempotent
	e.eq(len(at(e.readJSON(filepath.Join(dir, ".claude/settings.local.json")), "hooks", "Stop").([]any)), 1)
}

func TestInitBranchWritesOneSetupFile(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("init")
	e.initCLI(dir, "--branch")

	// the only change to the project's tracked/untracked files is the setup file
	e.eq(e.status(dir), "?? git-memory.json")
	e.eq(at(e.readJSON(filepath.Join(dir, "git-memory.json")), "store", "repo"), nil)

	// memory is a checkout of the orphan branch in .git-memory/
	e.ok(e.exists(filepath.Join(dir, ".git-memory/.git")), "no checkout")
	e.has(e.git(dir, "branch", "--list"), "memory")
	e.match(e.read(filepath.Join(dir, ".git/info/exclude")), `(?m)^/\.git-memory/$`)
	e.ok(!e.exists(filepath.Join(dir, ".gitignore")), ".gitignore is never touched")
	e.ok(!e.exists(filepath.Join(dir, "CLAUDE.md")), "CLAUDE.md written")

	// idempotent, and a plain re-run reads the setup file
	e.initCLI(dir)
	e.eq(len(at(e.readJSON(filepath.Join(dir, ".claude/settings.local.json")), "hooks", "Stop").([]any)), 1)
	e.eq(e.status(dir), "?? git-memory.json")
	e.eq(strings.TrimSpace(e.git(dir, "rev-parse", "--abbrev-ref", "HEAD")), "main")
}

// ---- sessions through the hooks --------------------------------------------------------------------------------

func TestClaudeSessionCaptureCompileRecall(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("claude")
	e.shared(dir)
	const sid = "s1aaaaaa"

	e.eq(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": sid, "source": "startup"}), map[string]any(nil)) // empty memory
	e.eq(e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid, "prompt": "Fix the flaky upload retry test, token=abcdefghijklmnop1234"}), map[string]any(nil))
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(dir, "src/upload.ts")}})
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(dir, ".env")}})
	e.hook(dir, "claude", "PostToolUse", map[string]any{"session_id": sid, "tool_name": "Bash", "tool_input": map[string]any{"command": "npm test"}})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": sid, "last_assistant_message": "Pinned the retry clock so the test is deterministic."})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": sid, "reason": "other"})

	e.has(e.git(dir, "log", "--oneline", "memory"), "memory: session s1aaaaaa (claude)")
	e.has(e.gm(dir, "list"), "Fix the flaky upload retry test")
	shown := e.gm(dir, "show", "s1aaaaaa")
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
}

func TestCopilotSessionUsesCamelCasePayloads(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("copilot")
	e.initCLI(dir)
	e.gm(dir, "remember", "We use pnpm, never npm, in this monorepo", "--kind", "decision", "--title", "Package manager")

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

	shown := e.gm(dir, "show", "copilot-c1")
	e.has(shown, "add a lint script")
	e.has(shown, "`pnpm lint` (failed)")
	e.has(shown, "`package.json`")
}

func TestRememberSearchForget(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("mem")
	e.initCLI(dir)
	e.gm(dir, "remember", "Auth tokens are rotated by the gateway, services must not cache them.", "--kind", "fact", "--title", "Token rotation")
	e.gm(dir, "remember", "Run db migrations with make migrate before the integration tests.", "--kind", "procedure", "--title", "Migrations")

	hits := e.gm(dir, "search", "how do migrations run before tests")
	e.match(strings.Split(hits, "\n")[0], `memories/procedure/migrations\.md`)
	e.has(e.gm(dir, "search", "tokens cache", "--json"), "token-rotation")

	e.gm(dir, "forget", "migrations")
	e.has(e.gm(dir, "search", "migrations"), "no matches")
	e.has(e.gm(dir, "log"), "memory: forget memories/procedure/migrations.md")
}

// ---- sync and stores --------------------------------------------------------------------------------------------

func TestTwoClonesShareMemoryThroughSync(t *testing.T) {
	e := newEnv(t)
	remote := e.bare()
	a := e.newRepo("a")
	e.git(a, "remote", "add", "origin", remote)
	e.git(a, "push", "-q", "origin", "main")
	b := e.mkdir("gm-b")
	e.git(b, "clone", "-q", remote, b)
	e.git(b, "config", "user.email", "b@example.com")
	e.git(b, "config", "user.name", "B")

	// both initialise independently: the deterministic root commit keeps histories compatible
	e.initCLI(a, "--branch")
	e.initCLI(b, "--branch")
	e.gm(a, "remember", "Deploys happen from the release branch only.", "--scope", "project", "--kind", "decision", "--title", "Deploy policy")
	e.gm(b, "remember", "Staging uses a separate database.", "--scope", "project", "--kind", "fact", "--title", "Staging db")
	e.gm(a, "sync")
	e.gm(b, "sync")
	e.gm(a, "sync")

	e.has(e.gm(b, "list"), "Deploy policy")
	e.has(e.gm(a, "list"), "Staging db")
}

func TestDedicatedStoreRepoIsNamespacedPerProject(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	memRemote := e.bare()
	p1, p2 := e.newRepo("proj1"), e.newRepo("proj2")
	e.initCLI(p1, "--store", memRemote, "--project", "alpha")
	e.initCLI(p2, "--store", memRemote, "--project", "beta")
	e.gm(p1, "scopes", "sessions", "project", "--local")

	for _, p := range []string{p1, p2} {
		e.eq(e.status(p), "?? git-memory.json")
		e.eq(strings.TrimSpace(e.git(p, "branch", "--list", "memory")), "")
		e.ok(e.exists(filepath.Join(p, ".git-memory/.git")), "no checkout")
	}
	setup := e.readJSON(filepath.Join(p1, "git-memory.json"))
	e.eq(at(setup, "store", "project"), "alpha")
	repo := at(setup, "store", "repo").(string)
	e.ok(!filepath.IsAbs(repo), "the setup file holds a relative path")
	e.eq(resolvePath(p1, repo), memRemote)

	fire := func(dir, event string, payload map[string]any) { e.hookRaw(dir, "claude", event, payload) }
	fire(p1, "SessionStart", map[string]any{"session_id": "x1x1x1x1"})
	fire(p1, "UserPromptSubmit", map[string]any{"session_id": "x1x1x1x1", "prompt": "Rework the billing webhook handler"})
	fire(p1, "SessionEnd", map[string]any{"session_id": "x1x1x1x1"})
	e.gm(p2, "remember", "Beta uses Redis for queues.", "--scope", "project", "--kind", "fact", "--title", "Queues")

	e.ok(len(names(filepath.Join(p1, ".git-memory/projects/alpha/sessions"))) > 0, "no session page")
	list := e.gm(p1, "list")
	e.has(list, "Rework the billing webhook handler")
	e.lacks(list, "Queues", "projects must not see each other")
	e.eq(e.status(p1), "?? git-memory.json")

	e.has(e.gm(p1, "sync"), "synced")
	e.has(e.gm(p2, "sync"), "synced")
	verify := e.cloneOf(memRemote)
	e.ok(e.exists(filepath.Join(verify, "projects/beta/memories/fact/queues.md")), "beta's memory not pushed")
	e.ok(e.exists(filepath.Join(verify, "projects/alpha/sessions")), "alpha's sessions not pushed")
}

func TestTeammateClonesAndRunsPlainInit(t *testing.T) {
	e := newEnv(t)
	memRemote, projRemote := e.bare(), e.bare()
	alice := e.newRepo("alice")
	e.git(alice, "remote", "add", "origin", projRemote)
	e.initCLI(alice, "--store", memRemote)
	e.git(alice, "add", "git-memory.json")
	e.git(alice, "commit", "-qm", "add git-memory setup")
	e.git(alice, "push", "-q", "origin", "main")
	e.gm(alice, "remember", "Releases are cut from main on Fridays.", "--scope", "project", "--kind", "decision", "--title", "Release cadence")
	e.gm(alice, "sync")

	bob := e.mkdir("gm-bob")
	e.git(bob, "clone", "-q", projRemote, bob)
	e.withHome(e.freshHome(), func() { // bob is another machine
		e.initCLI(bob)
		e.eq(e.status(bob), "")
		e.has(e.gm(bob, "list"), "Release cadence")
	})
}

func TestPathMovesTheCheckoutAndGlobalStoreApplies(t *testing.T) {
	e := newEnv(t)
	memRemote := e.bare()
	home := e.freshHome()
	e.write(filepath.Join(home, "config.json"), marshalJSON(obj{"store": obj{"repo": portable(memRemote, home)}}, false))
	e.withHome(home, func() {
		dir := e.newRepo("global")
		e.initCLI(dir, "--path", ".mem")
		e.ok(e.exists(filepath.Join(dir, ".mem/projects")), "project folder created in the global store")
		e.match(e.read(filepath.Join(dir, ".git/info/exclude")), `(?m)^/\.mem/$`)
		e.eq(strings.TrimSpace(e.git(dir, "branch", "--list", "memory")), "")
	})
}

func TestDoctorReportsWiringActivityAndErrors(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("doctor")
	e.initCLI(dir, "--branch")
	before := e.gm(dir, "doctor")
	e.has(before, "✓ Hook runtime")
	e.has(before, "✓ Claude Code hooks: 7 events")
	e.has(before, "✓ Copilot hooks: 6 events")
	e.has(before, "· Agent activity: no hook events yet")
	e.has(before, "! Sync remote: no remote")

	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "d1d1d1d1"})
	e.has(e.gm(dir, "doctor"), "✓ Agent activity: last hook event just now")

	e.write(filepath.Join(dir, ".git/git-memory/errors.log"), "2026-10-25T00:00:00Z Error: boom\n")
	e.match(e.gm(dir, "doctor"), `! Hook errors: 1 logged; latest: .*boom`)

	// a launcher that points at a binary which no longer exists is flagged as an error
	l := launcher(dir)
	e.write(l, strings.Replace(e.read(l), realpath(binPath), "/nonexistent/git-memory", 1))
	stdout, _, code := e.run(dir, binPath, "doctor")
	e.eq(code, 1)
	e.has(stdout, "✗ Binary used by hooks: not found: /nonexistent/git-memory")
}

// ---- scopes -----------------------------------------------------------------------------------------------------

func TestMemoriesStartPersonalUntilPromoted(t *testing.T) {
	e := newEnv(t)
	a, b := e.newRepo("pa"), e.newRepo("pb")
	e.initCLI(a)
	e.initCLI(b)

	e.gm(a, "remember", "Always use strict TypeScript.", "--kind", "decision", "--title", "Strict TS")
	slugA := names(filepath.Join(e.personalDir(), "private/projects"))[0]
	e.ok(e.exists(filepath.Join(e.personalDir(), "private/projects", slugA, "memories/decision/strict-ts.md")), "not stored per project")

	// personal memories are per project: B does not see A's
	e.lacks(e.gm(b, "briefing"), "Strict TS")
	e.has(e.gm(a, "briefing"), "- Strict TS {personal} — Always use strict TypeScript. (personal:memories/decision/strict-ts.md)")

	// promote: now every project sees it, tagged global
	e.has(e.gm(a, "promote", "strict-ts"), "promoted to global:memories/decision/strict-ts.md")
	e.ok(e.exists(filepath.Join(e.personalDir(), "global/memories/decision/strict-ts.md")), "not in global")
	e.ok(!e.exists(filepath.Join(e.personalDir(), "private/projects", slugA, "memories/decision/strict-ts.md")), "still personal")
	e.has(e.gm(b, "briefing"), "- Strict TS {global}")
	e.match(e.gm(b, "search", "strict typescript"), `^global:memories/decision/strict-ts\.md`)
	e.eq(strings.TrimSpace(e.gm(b, "search", "strict typescript", "--scope", "personal")), "no matches")

	// and it can be made personal again
	e.has(e.gm(a, "move", "global:strict-ts", "--to", "personal"), "moved to personal:")
	e.lacks(e.gm(b, "briefing"), "Strict TS")
	e.eq(e.status(a), "")
	e.eq(e.status(b), "")
}

func TestNamedScopesJoinWithoutCreatingAProjectStore(t *testing.T) {
	e := newEnv(t)
	repo := e.bare()
	homeA, homeB := e.freshHome(), e.freshHome()
	a, b, c := e.newRepo("na"), e.newRepo("nb"), e.newRepo("nc")

	e.withHome(homeA, func() {
		e.initCLI(a)
		e.gm(a, "scopes", "add", "payments", "--repo", repo)
		// the committed file now lists the scope, but there is still no project store
		e.eq(marshalJSON(e.readJSON(filepath.Join(a, "git-memory.json")), false), marshalJSON(obj{"scopes": obj{"payments": obj{"repo": portable(repo, a)}}}, false))
		e.match(e.gm(a, "scopes"), `project\s+Project \(shared\)\s+not set up`)
		e.eq(strings.TrimSpace(e.git(a, "branch", "--list", "memory")), "")

		e.gm(a, "remember", "Amounts are integers in minor units.", "--kind", "decision", "--title", "Money as integers")
		e.has(e.gm(a, "promote", "money-as-integers", "--to", "payments"), "promoted to payments:memories/decision/money-as-integers.md")
		e.has(e.gm(a, "sync"), "payments: synced")
	})
	e.withHome(homeB, func() {
		e.initCLI(b)
		e.gm(b, "scopes", "add", "payments", "--repo", repo, "--local")
		e.gm(b, "sync") // clones the scope's repo
		e.has(e.gm(b, "briefing"), "- Money as integers {payments}")
		e.match(e.gm(b, "search", "minor units money", "--scope", "payments"), `^payments:memories/decision/money-as-integers\.md`)
		e.ok(!e.exists(filepath.Join(b, "git-memory.json")), "--local keeps the join out of the project")
	})
	e.withHome(e.freshHome(), func() {
		e.initCLI(c)
		e.lacks(e.gm(c, "briefing"), "Money as integers", "a project that did not join must not see it")
	})
}

func TestPromoteToProjectNeedsAnOptedInStore(t *testing.T) {
	e := newEnv(t)
	store := e.bare()
	dir := e.newRepo("pp")
	e.initCLI(dir)
	e.gm(dir, "remember", "Deploys go through the release branch.", "--kind", "procedure", "--title", "Deploys")

	stderr, code := e.gmFail(dir, "promote", "deploys", "--to", "project")
	e.eq(code, 1)
	e.has(stderr, "project scope has no shared store yet")

	e.initCLI(dir, "--store", store, "--project", "shop")
	e.has(e.gm(dir, "promote", "personal:deploys", "--to", "project"), "promoted to project:memories/procedure/deploys.md")
	e.gm(dir, "sync")
	e.ok(e.exists(filepath.Join(e.cloneOf(store), "projects/shop/memories/procedure/deploys.md")), "not shared")
}

func TestDefaultScopeLayers(t *testing.T) {
	e := newEnv(t)
	store := e.bare()
	dir := e.newRepo("def")
	e.initCLI(dir, "--store", store, "--project", "x")
	e.match(e.gm(dir, "scopes"), `default scope: personal {3}session pages → personal`)

	e.match(e.gm(dir, "remember", "one"), `^remembered personal:`)
	e.gm(dir, "scopes", "default", "global", "--local")
	e.match(e.gm(dir, "remember", "two"), `^remembered global:`)
	e.match(e.gm(dir, "remember", "three", "--scope", "project"), `^remembered project:`) // an explicit scope always wins

	// --shared puts the team's default in the committed file; the machine-local choice still overrides it
	e.gm(dir, "scopes", "default", "project", "--shared")
	e.eq(at(e.readJSON(filepath.Join(dir, "git-memory.json")), "defaultScope"), "project")
	e.match(e.gm(dir, "remember", "four"), `^remembered global:`)
	// --personal applies to all of this user's projects (lowest of the three layers)
	e.gm(dir, "scopes", "default", "personal", "--local")
	e.gm(dir, "scopes", "default", "global", "--personal")
	e.match(e.gm(dir, "remember", "five"), `^remembered personal:`)
	e.eq(at(e.readJSON(filepath.Join(e.home, "config.json")), "defaultScope"), "global")

	stderr, _ := e.gmFail(dir, "scopes", "default", "nope")
	e.has(stderr, `unknown scope "nope"; available: project, personal, global`)
}

func TestSessionPagesPersonalByDefault(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("sess")
	e.initCLI(dir, "--branch")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "u1u1u1u1"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "u1u1u1u1", "prompt": "Debug my private staging credentials issue"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": "u1u1u1u1", "last_assistant_message": "Rotated the staging key."})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "u1u1u1u1"})

	// the prompt went to the personal store, not the shared project store
	e.lacks(e.git(dir, "ls-tree", "-r", "--name-only", "memory"), "sessions/")
	e.eq(len(names(filepath.Join(e.personalDir(), "private/projects"))), 1)
	e.match(e.gm(dir, "list"), `\{personal\} session\] Debug my private staging credentials issue`)
	next := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "u2u2u2u2"})
	e.has(ctxOf(next), "Rotated the staging key")
	e.has(e.gm(dir, "doctor"), "✓ Session pages: personal (private to you)")

	// opting in to shared sessions
	e.gm(dir, "scopes", "sessions", "project", "--local")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "u3u3u3u3"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "u3u3u3u3", "prompt": "Refactor the shared checkout flow"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "u3u3u3u3"})
	e.match(e.git(dir, "ls-tree", "-r", "--name-only", "memory"), `sessions/.*u3u3u3u3`)
	e.has(e.gm(dir, "doctor"), "✓ Session pages: project")
}

func TestPersonalStoreWithRemoteFollowsYouAndNeverReachesTheTeam(t *testing.T) {
	e := newEnv(t)
	team, mine := e.bare(), e.bare()
	// two clones of the same project share an origin, which is what identifies the project in the personal scope
	origin := filepath.Join(e.mkdir("gm-origin"), "shop.git")
	p := e.newRepo("priv")
	e.git(p, "remote", "add", "origin", origin)
	e.initCLI(p, "--store", team, "--project", "shop")
	e.gm(p, "scopes", "personal-repo", mine)

	e.gm(p, "remember", "My DB runs on port 5433.", "--kind", "fact", "--title", "My DB port")
	e.gm(p, "remember", "The API listens on 8080.", "--scope", "project", "--kind", "fact", "--title", "API port")
	e.has(e.gm(p, "sync"), "synced")

	teamClone := e.cloneOf(team)
	e.ok(e.exists(filepath.Join(teamClone, "projects/shop/memories/fact/api-port.md")), "team memory missing")
	e.lacks(e.git(teamClone, "ls-tree", "-r", "--name-only", "HEAD"), "my-db-port", "a personal memory leaked to the team store")
	e.ok(e.exists(filepath.Join(e.cloneOf(mine), "private/projects/shop/memories/fact/my-db-port.md")), "personal memory not pushed")

	// another machine of the same user
	p2 := e.newRepo("priv2")
	e.git(p2, "remote", "add", "origin", origin)
	e.withHome(e.freshHome(), func() {
		e.initCLI(p2, "--store", team, "--project", "shop")
		e.gm(p2, "scopes", "personal-repo", mine)
		e.gm(p2, "sync")
		e.has(e.gm(p2, "list"), "My DB port")
	})
}

func TestSyncWithNoRemoteReportsLocalOnly(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("loc")
	e.initCLI(dir)
	e.gm(dir, "remember", "only here", "--title", "Local")
	e.has(e.gm(dir, "sync"), "local only (no remote)")
}

func TestPersonalMemoriesSurviveCustomProjectName(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("stable")
	e.initCLI(dir)
	e.gm(dir, "remember", "Keep it private.", "--title", "Private note")
	before := names(filepath.Join(e.personalDir(), "private/projects"))

	e.initCLI(dir, "--store", e.bare(), "--project", "renamed-for-the-team")
	e.eq(names(filepath.Join(e.personalDir(), "private/projects")), before, "the personal folder must not change")
	e.match(e.gm(dir, "list"), `\{personal\} note\] Private note`)
}

// ---- decisions --------------------------------------------------------------------------------------------------

func TestEveryDecisionIsInTheBriefing(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("dec")
	e.initCLI(dir)
	e.gm(dir, "remember", "Never commit secrets or .env files.", "--kind", "decision", "--title", "No secrets", "--core")
	e.gm(dir, "remember", "The oldest decision, made first.", "--kind", "decision", "--title", "Oldest decision")
	for i := 1; i <= 12; i++ {
		e.gm(dir, "remember", fmt.Sprintf("Decision number %d body.", i), "--kind", "decision", "--title", fmt.Sprintf("Decision %d", i))
	}
	b := e.gm(dir, "briefing")
	core, decisions := strings.Index(b, "## Core notes"), strings.Index(b, "## Decisions in force")
	e.ok(core > 0 && core < decisions, "pinned notes first")
	e.has(b, "### No secrets {personal}\nNever commit secrets or .env files.", "a pinned note must not repeat its own title")
	e.has(b, "- Oldest decision {personal}", "old decisions stay in force")
	for i := 1; i <= 12; i++ {
		e.has(b, fmt.Sprintf("- Decision %d {personal}", i))
	}
	e.lacks(b, "- No secrets", "a pinned note is not listed twice")
}

func TestSupersedeRetiresADecision(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("sup")
	e.initCLI(dir, "--store", e.bare(), "--project", "s")
	e.gm(dir, "remember", "Public APIs are REST.", "--kind", "decision", "--title", "Use REST", "--scope", "project", "--core")
	e.gm(dir, "remember", "Public APIs are GraphQL, REST is legacy.", "--kind", "decision", "--title", "Use GraphQL")

	out := e.gm(dir, "supersede", "use-rest", "--by", "use-graphql", "--reason", "clients need flexible queries")
	e.has(out, "superseded project:core/use-rest.md by personal:memories/decision/use-graphql.md (the replacement is now pinned)")

	// gone from everything that guides the agent...
	b := e.gm(dir, "briefing")
	e.lacks(b, "Use REST", "a superseded decision must leave the briefing")
	e.match(b, `## Core notes[\s\S]*### Use GraphQL \{personal\}`, "the replacement took its place")
	e.lacks(b, "- Use GraphQL", "and is not listed twice")
	e.lacks(e.gm(dir, "search", "rest api"), "use-rest")
	e.lacks(e.gm(dir, "list"), "Use REST")
	recall := e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "sup1sup1", "prompt": "should the public api be rest or graphql for the new endpoint?"})
	e.lacks(ctxOf(recall), "Use REST", "recall must not surface superseded pages")

	// ...but kept, linked, and findable on request
	e.match(e.gm(dir, "search", "rest api", "--all"), `^project:core/use-rest\.md {2}\[\{project\} decision, superseded\] Use REST`)
	e.has(e.gm(dir, "list", "--all"), "[{project} decision, pinned, superseded] Use REST")
	old := e.gm(dir, "show", "project:core/use-rest")
	e.has(old, "status: superseded")
	e.has(old, "superseded_by: personal:memories/decision/use-graphql.md")
	e.has(old, "superseded_reason: clients need flexible queries")
	e.has(old, "> Superseded by personal:memories/decision/use-graphql.md: clients need flexible queries")
	e.has(e.gm(dir, "show", "personal:use-graphql"), "supersedes: project:core/use-rest.md")
	e.has(e.git(filepath.Join(dir, ".git-memory"), "log", "--oneline"), "memory: supersede core/use-rest.md")

	again, _ := e.gmFail(dir, "supersede", "project:use-rest")
	e.has(again, "already superseded")
	self, _ := e.gmFail(dir, "supersede", "use-graphql", "--by", "use-graphql")
	e.has(self, "cannot supersede itself")

	// retiring without a replacement also works
	e.gm(dir, "remember", "We deploy on Fridays.", "--kind", "decision", "--title", "Friday deploys")
	e.match(e.gm(dir, "supersede", "friday-deploys", "--reason", "caused too many incidents"), `(?m)^superseded personal:memories/decision/friday-deploys\.md$`)
	e.lacks(e.gm(dir, "briefing"), "Friday deploys")
}

// ---- what a session page contains ------------------------------------------------------------------------------

func TestSessionPageStripsEditorContext(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("pages")
	e.initCLI(dir)
	const sid = "pg1pg1pg1"

	// a prompt wrapped in the context blocks that editors prepend
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": sid})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid,
		"prompt": "<ide_opened_file>The user opened the file /x/y.ts in the IDE.</ide_opened_file> add a health endpoint\n<ide_selection>const secretish = 1;</ide_selection>"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid, "prompt": "<ide_opened_file>only context, no words</ide_opened_file>"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": sid, "last_assistant_message": "Added /health.\n\n- route in src/server.ts\n- test in test/health.test.ts"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": sid})

	page := e.gm(dir, "show", "pg1pg1pg")
	e.match(page, `(?m)^# add a health endpoint$`, "the title is what the user asked, not editor context")
	e.ok(!strings.Contains(page, "ide_") && !strings.Contains(page, "secretish"), "editor context leaked into the page")
	e.has(page, "1 prompt(s)", "a prompt that was only context is not recorded")
	e.has(page, "## Outcome\n\nAdded /health.\n\n- route in src/server.ts\n- test in test/health.test.ts", "line breaks survive")

	// a session where nothing was asked produces neither a page nor a commit
	before := e.gm(dir, "list")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "em1em1em1"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "em1em1em1"})
	e.eq(e.gm(dir, "list"), before)
}

// ---- auto-remember (prompt triggers) ---------------------------------------------------------------------------

func TestPromptTriggersAreSavedByTheHook(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("trig")
	e.initCLI(dir)
	e.has(e.gm(dir, "auto"), "prompt triggers on")
	e.has(e.gm(dir, "doctor"), "✓ Auto-remember: prompt triggers on → personal scope")

	say := func(prompt, agent string) {
		event := "UserPromptSubmit"
		if agent == "copilot" {
			event = "userPromptSubmitted"
		}
		e.hook(dir, agent, event, map[string]any{"session_id": "tr1tr1tr1", "sessionId": "tr1tr1tr1", "prompt": prompt})
	}
	say(strings.Join([]string{
		"<ide_opened_file>The user opened a file.</ide_opened_file> Please fix the bug.",
		"remember that we deploy on Tuesdays only",
		"Decision: use pnpm, never npm",
		"#procedure run make migrate before the tests",
		"remember to run tests first",
		"Note: the build is red on CI",
		"#fact the staging DB_PASSWORD=hunter2hunter2 must come from the vault",
	}, "\n"), "claude")
	list := e.gm(dir, "list", "--tag", "auto")
	e.has(list, "[{personal} note] we deploy on Tuesdays only")
	e.has(list, "[{personal} decision] use pnpm, never npm")
	e.has(list, "[{personal} procedure] run make migrate before the tests")
	e.eq(len(strings.Split(strings.TrimSpace(list), "\n")), 4)
	e.ok(!strings.Contains(list, "run tests first") && !strings.Contains(list, "build is red"), "instructions and prose are not memories")
	e.ok(!strings.Contains(list, "hunter2hunter2") && strings.Contains(list, "DB_PASSWORD=[REDACTED]"), "secrets are redacted")

	page := e.gm(dir, "show", "personal:we-deploy-on-tuesdays")
	e.has(page, "tags: [auto, prompt]")
	e.has(page, "source: claude:tr1tr1tr1")

	// the saved memories reach the next session's briefing like any other
	next := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "tr2tr2tr2"})
	e.has(ctxOf(next), "- use pnpm, never npm {personal}")

	// same for Copilot's prompt hook, with the configured scope
	e.gm(dir, "auto", "scope", "global")
	say("#remember releases are tagged with a v prefix", "copilot")
	e.has(e.gm(dir, "list", "--scope", "global"), "{global} note] releases are tagged with a v prefix")
	e.eq(e.status(dir), "", "nothing is added to the project")
}

func TestTriggersCanBeTurnedOffAndABadScopeCannotBreakTheHook(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("trigoff")
	e.initCLI(dir, "--branch")
	e.gm(dir, "auto", "off")
	e.has(e.gm(dir, "auto"), "prompt triggers off")
	e.has(e.gm(dir, "doctor"), "· Auto-remember: off")
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "to1to1to1", "prompt": "remember that this must not be stored"})
	e.eq(strings.TrimSpace(e.gm(dir, "list", "--tag", "auto")), "")

	e.gm(dir, "auto", "on", "--shared")
	e.eq(at(e.readJSON(filepath.Join(dir, "git-memory.json")), "autoRemember", "triggers"), true)
	e.gm(dir, "auto", "off", "--personal")
	e.eq(at(e.readJSON(filepath.Join(e.home, "config.json")), "autoRemember", "triggers"), false)

	// an unknown scope in the config is logged, not thrown: the prompt must still be recorded for the session
	local := e.readJSON(filepath.Join(dir, ".git/git-memory/config.json"))
	local["autoRemember"] = obj{"triggers": true, "scope": "nope"}
	e.write(filepath.Join(dir, ".git/git-memory/config.json"), marshalJSON(local, false))
	e.eq(e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "to2to2to2", "prompt": "remember that the scope above does not exist"}), map[string]any(nil))
	e.has(e.read(filepath.Join(dir, ".git/git-memory/errors.log")), `unknown scope "nope"`)
	e.has(e.read(filepath.Join(dir, ".git/git-memory/spool/claude-to2to2to2.jsonl")), "the scope above does not exist", "the session still recorded the prompt")

	stderr, _ := e.gmFail(dir, "auto", "scope", "nope2")
	e.has(stderr, `unknown scope "nope2"`)
}

// ---- slash commands ---------------------------------------------------------------------------------------------

var (
	commandNames = []string{"remember", "decide", "recall", "memories", "promote", "forget"}
	writerNames  = []string{"remember", "decide", "promote", "forget"}
)

// rel is how the (committable) command files refer to the CLI.
func relCLI() string { return "./.git/git-memory/" + wrapperName() }

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

func TestInitInstallsSlashCommands(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("cmds")
	out := e.initCLI(dir)
	e.has(out, "slash commands (Claude Code skills, Copilot prompt files; git-excluded): /remember /decide /recall /memories /promote /forget")
	e.eq(e.status(dir), "", "commands must be git-excluded")

	for _, name := range commandNames {
		skill := e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md"))
		fm := frontmatterOf(skill)
		e.eq(fm["name"], name)
		e.ok(len(fm["description"]) > 20, name+" needs a description for the / menu")
		e.eq(fm["allowed-tools"], "Bash("+relCLI()+" *)", "the CLI is pre-approved for the turn, by its project-relative path")
		// commands that write are user-only; the model may load the read-only ones when relevant
		want := ""
		if contains(writerNames, name) {
			want = "true"
		}
		e.eq(fm["disable-model-invocation"], want, name)
		e.has(skill, "git-memory:generated")

		prompt := e.read(filepath.Join(dir, ".github/prompts", name+".prompt.md"))
		pf := frontmatterOf(prompt)
		e.eq(pf["agent"], "agent")
		e.ok(len(pf["description"]) > 20, name+" prompt needs a description")
		e.has(prompt, "`"+relCLI()+" ", "prompt files call the same relative launcher")
	}
	// /memories runs its read-only commands before the model sees the file (Claude Code `!` injection)
	memories := e.read(filepath.Join(dir, ".claude/skills/memories/SKILL.md"))
	e.match(memories, "(?m)^!`"+regexp.QuoteMeta(relCLI())+" scopes`")
	e.has(memories, "doctor --report`")
	e.lacks(e.read(filepath.Join(dir, ".claude/skills/recall/SKILL.md")), "disable-model-invocation")
	e.ok(!e.exists(filepath.Join(dir, ".claude/skills/memory")), "must not shadow the built-in /memory")

	exclude := e.read(filepath.Join(dir, ".git/info/exclude"))
	e.match(exclude, `(?m)^/\.claude/skills/remember/$`)
	e.match(exclude, `(?m)^/\.github/prompts/remember\.prompt\.md$`)

	// the launcher is what agents are told to run, and it is also the CLI shown in the briefing
	e.has(e.gm(dir, "doctor"), "✓ Slash commands: Claude Code 6/6, Copilot 6/6")
	e.gm(dir, "remember", "x marks the spot for the test", "--title", "Spot")
	e.has(e.sh(dir, launcher(dir), "list"), "Spot")
	start := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "cm1cm1cm1"})
	e.has(ctxOf(start), "Memory lookup (run from the repository root): `./.git/git-memory/gm search")
}

func TestCommandsNeverOverwriteYourOwnFiles(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("cmds2")
	own := "---\nname: remember\ndescription: my own\n---\nMine.\n"
	e.write(filepath.Join(dir, ".claude/skills/remember/SKILL.md"), own)
	e.write(filepath.Join(dir, ".github/prompts/recall.prompt.md"), "my own recall prompt\n")

	out := e.initCLI(dir)
	e.has(out, "note: kept your own .claude/skills/remember/SKILL.md")
	e.has(out, "note: kept your own .github/prompts/recall.prompt.md")
	e.eq(e.read(filepath.Join(dir, ".claude/skills/remember/SKILL.md")), own)
	e.eq(e.read(filepath.Join(dir, ".github/prompts/recall.prompt.md")), "my own recall prompt\n")
	e.has(e.read(filepath.Join(dir, ".claude/skills/decide/SKILL.md")), "git-memory:generated")
	e.has(e.gm(dir, "doctor"), "! Slash commands: Claude Code 5/6, Copilot 5/6")

	// re-running init rewrites only the generated files
	before := e.read(filepath.Join(dir, ".claude/skills/decide/SKILL.md"))
	e.initCLI(dir)
	e.eq(e.read(filepath.Join(dir, ".claude/skills/decide/SKILL.md")), before)

	none := e.newRepo("cmds3")
	e.lacks(e.initCLI(none, "--no-commands"), "slash commands")
	e.ok(!e.exists(filepath.Join(none, ".claude/skills")) && !e.exists(filepath.Join(none, ".github/prompts")), "commands written despite --no-commands")
	e.has(e.gm(none, "doctor"), "· Slash commands: not installed")

	onlyClaude := e.newRepo("cmds4")
	e.initCLI(onlyClaude, "--no-copilot")
	e.ok(e.exists(filepath.Join(onlyClaude, ".claude/skills/recall/SKILL.md")) && !e.exists(filepath.Join(onlyClaude, ".github/prompts")), "--no-copilot")
}

func TestEveryCommandLineInTheGeneratedFilesWorks(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("cmds5")
	e.initCLI(dir, "--branch")
	helpText := e.sh(dir, binPath, "help")

	// drift guard: each subcommand and flag the files mention exists in the CLI help
	known := []string{"remember", "search", "show", "scopes", "doctor", "briefing", "promote", "move", "supersede", "forget"}
	var bodies strings.Builder
	for _, n := range commandNames {
		bodies.WriteString(e.read(filepath.Join(dir, ".claude/skills", n, "SKILL.md")) + "\n")
	}
	for _, m := range regexp.MustCompile(regexp.QuoteMeta(relCLI())+` ([a-z]+)`).FindAllStringSubmatch(bodies.String(), -1) {
		e.ok(contains(known, m[1]), "unknown subcommand in a generated command: "+m[1])
	}
	for _, m := range regexp.MustCompile(` (--[a-z]+)`).FindAllStringSubmatch(bodies.String(), -1) {
		e.has(helpText, m[1], "flag is not in the CLI help")
	}

	// execute the documented flows end to end through the launcher
	run := func(a ...string) string { return e.gm(dir, a...) }
	e.match(run("remember", "Public APIs are REST for external clients.", "--kind", "decision", "--title", "API style", "--scope", "personal"), `remembered personal:memories/decision/api-style\.md`)
	e.has(run("remember", "Public APIs are GraphQL now.", "--kind", "decision", "--title", "API style v2", "--scope", "personal"), "remembered")
	e.has(run("search", "public api style", "--limit", "8"), "personal:memories/decision/api-style")
	e.has(run("show", "personal:memories/decision/api-style.md"), "REST for external clients")
	e.has(run("supersede", "personal:memories/decision/api-style.md", "--by", "personal:memories/decision/api-style-v2.md", "--reason", "clients need flexible queries"), "superseded")
	e.has(run("search", "public api style", "--all", "--limit", "8"), "superseded")
	e.has(run("promote", "personal:memories/decision/api-style-v2.md", "--to", "global"), "promoted to global:")
	e.has(run("move", "global:memories/decision/api-style-v2.md", "--to", "personal"), "moved to personal:")
	e.has(run("scopes"), "personal")
	e.match(run("briefing"), `Decisions in force|Core notes`)
	e.has(run("forget", "personal:memories/decision/api-style.md"), "forgot")

	// /memories calls doctor --report so a warning can never abort the injected block; plain doctor fails when broken
	if err := os.RemoveAll(filepath.Join(dir, ".git-memory")); err != nil {
		t.Fatal(err)
	}
	_, _, code := e.run(dir, launcher(dir), "doctor")
	e.eq(code, 1, "plain doctor fails when something is broken")
	_, _, code = e.run(dir, launcher(dir), "doctor", "--report")
	e.eq(code, 0, "doctor --report always succeeds")
}

func TestCommandFilesAreIdenticalOnEveryMachine(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	a, b := e.newRepo("portable-a"), e.newRepo("portable-b")
	homeB := e.freshHome()
	homeA := e.home
	e.initCLI(a)
	e.withHome(homeB, func() { e.initCLI(b) }) // another machine: other paths, other memory home

	files := func(dir string) map[string]string {
		out := map[string]string{}
		for _, name := range commandNames {
			for _, rel := range []string{".claude/skills/" + name + "/SKILL.md", ".github/prompts/" + name + ".prompt.md"} {
				out[rel] = e.read(filepath.Join(dir, rel))
			}
		}
		return out
	}
	fa, fb := files(a), files(b)
	e.eq(len(fa), len(commandNames)*2)
	e.eq(marshalJSON(fa, false), marshalJSON(fb, false), "generated command files must not depend on where the project or the memory home are")

	home, _ := os.UserHomeDir()
	personal := []string{a, b, home, e.tmp, filepath.Dir(binPath), homeA, homeB, "GIT_MEMORY_HOME"}
	for rel, text := range fa {
		for _, s := range personal {
			e.lacks(text, s, rel+" contains a personal path")
		}
		e.ok(!regexp.MustCompile(`/(home|Users|tmp)/`).MatchString(text), rel+" contains an absolute path")
		e.has(text, relCLI(), rel+" should use the relative launcher")
		e.has(text, "If that file does not exist, git-memory is not set up on this machine yet: tell the user to run that and stop", rel+" must say what to do before init")
	}

	// regenerating leaves them byte-identical, so committed copies never show up as modified
	e.initCLI(a)
	e.eq(marshalJSON(files(a), false), marshalJSON(fa, false))

	// and committing them works: tracked files are unaffected by the local exclude
	e.git(a, "add", "-f", ".claude/skills", ".github/prompts")
	e.git(a, "commit", "-qm", "share the memory commands")
	e.initCLI(a)
	e.eq(e.status(a), "", "init must not modify committed command files")
}

// ---- relative paths everywhere ----------------------------------------------------------------------------------

func TestNothingInitWritesHoldsAnAbsolutePath(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("relative")
	store, payments := e.bare(), e.bare()
	e.initCLI(dir, "--store", store, "--project", "rel") // an ABSOLUTE path typed by the user
	e.gm(dir, "scopes", "add", "payments", "--repo", payments)
	e.gm(dir, "scopes", "add", "mine", "--repo", payments, "--local")
	e.gm(dir, "scopes", "personal-repo", store)
	e.gm(dir, "scopes", "global-repo", "git@github.com:acme/conventions.git") // URLs are left alone
	e.gm(dir, "auto", "off", "--local")

	home, _ := os.UserHomeDir()
	personal := []string{dir, home, e.tmp, e.home, filepath.Dir(binPath)}
	written := []string{"git-memory.json", ".claude/settings.local.json", ".github/hooks/git-memory.json", ".git/git-memory/config.json", ".git/info/exclude"}
	for _, n := range commandNames {
		written = append(written, ".claude/skills/"+n+"/SKILL.md", ".github/prompts/"+n+".prompt.md")
	}
	abs := regexp.MustCompile(`(^|["'\s=:])/(home|Users|tmp|var|usr)/`)
	for _, rel := range written {
		text := e.read(filepath.Join(dir, rel))
		for _, p := range personal {
			e.lacks(text, p, rel+" contains a personal path")
		}
		e.ok(!abs.MatchString(text), rel+" holds an absolute path")
	}

	// what is written is relative, and means the same thing when it is read back
	setup := e.readJSON(filepath.Join(dir, "git-memory.json"))
	e.eq(at(setup, "store", "repo"), portable(store, dir))
	e.eq(at(setup, "scopes", "payments", "repo"), portable(payments, dir))
	e.ok(strings.HasPrefix(at(setup, "store", "repo").(string), "../"), "store repo should be relative")
	local := e.readJSON(filepath.Join(dir, ".git/git-memory/config.json"))
	e.eq(at(local, "cli"), "./.git/git-memory/gm")
	e.eq(at(local, "scopes", "mine", "repo"), portable(payments, dir))
	global := e.readJSON(filepath.Join(e.home, "config.json"))
	e.eq(at(global, "personal", "repo"), portable(store, e.home), "the per-user file is relative to its own directory")
	e.eq(at(global, "global", "repo"), "git@github.com:acme/conventions.git")
	e.match(e.gm(dir, "scopes"), "payments .*"+regexp.QuoteMeta(filepath.Base(payments)), "the relative path resolves to the repo it names")

	// hooks use the launcher by relative path; the launcher (under .git, never committed) is the only place with machine paths
	e.eq(at(e.readJSON(filepath.Join(dir, ".claude/settings.local.json")), "hooks", "Stop", 0, "hooks", 0, "command"), `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" hook claude Stop`)
	copilot := at(e.readJSON(filepath.Join(dir, ".github/hooks/git-memory.json")), "hooks", "agentStop", 0)
	e.eq(at(copilot, "command"), "./.git/git-memory/gm hook copilot agentStop")
	e.eq(at(copilot, "cwd"), ".")
	wrapper := e.read(filepath.Join(dir, ".git/git-memory/gm"))
	e.match(wrapper, `exec '[^']*git-memory[^']*' "\$@"`)
	e.lacks(wrapper, dir, "the launcher holds no project path")
}

func TestHookFilesAndSetupAreTheSameOnEveryMachine(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	parent := e.mkdir("siblings")
	mk := func(name string) string {
		d := filepath.Join(parent, name)
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		e.git(d, "init", "-q", "-b", "main")
		e.git(d, "config", "user.email", "t@example.com")
		e.git(d, "config", "user.name", "T")
		e.write(filepath.Join(d, "f"), "x")
		e.git(d, "add", ".")
		e.git(d, "commit", "-qm", "i")
		return d
	}
	store := filepath.Join(parent, "memory.git")
	e.git(parent, "init", "-q", "--bare", "-b", "main", store)
	a, b := mk("alice-checkout"), mk("alice-checkout-2")
	e.initCLI(a, "--store", store, "--project", "same")
	e.withHome(e.freshHome(), func() { e.initCLI(b, "--store", store, "--project", "same") })

	rels := []string{"git-memory.json", ".claude/settings.local.json", ".github/hooks/git-memory.json"}
	for _, n := range commandNames {
		rels = append(rels, ".claude/skills/"+n+"/SKILL.md")
	}
	for _, rel := range rels {
		e.eq(e.read(filepath.Join(a, rel)), e.read(filepath.Join(b, rel)), rel+" differs between machines")
	}
	e.eq(at(e.readJSON(filepath.Join(a, "git-memory.json")), "store", "repo"), "../memory.git")
}

// ---- switching implementations ---------------------------------------------------------------------------------

// A user's own hooks and settings survive init, and re-running it never duplicates our entries.
func TestInitKeepsTheUsersOwnHooksAndSettings(t *testing.T) {
	e := newEnv(t)
	dir := e.newRepo("keep")
	e.initCLI(dir)
	file := filepath.Join(dir, ".claude/settings.local.json")
	s := e.readJSON(file)
	stop := at(s, "hooks", "Stop").([]any)
	s["hooks"].(map[string]any)["Stop"] = append(stop, obj{"hooks": []any{obj{"type": "command", "command": "echo mine"}}})
	s["permissions"] = obj{"allow": []any{"Bash(ls)"}}
	e.write(file, marshalJSON(s, true))

	e.initCLI(dir)
	after := e.readJSON(file)
	groups := at(after, "hooks", "Stop").([]any)
	e.eq(len(groups), 2)
	e.eq(at(groups[0], "hooks", 0, "command"), "echo mine")
	e.has(at(groups[1], "hooks", 0, "command").(string), "hook claude Stop")
	e.eq(at(after, "permissions", "allow", 0), "Bash(ls)")
}
