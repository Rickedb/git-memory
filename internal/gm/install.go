package gm

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// wrapperName is the per-project launcher inside .git/git-memory. Hook files refer to it by a project-relative path, so
// they read the same on every machine; it is the only file that knows where the real binary lives.
func wrapperName() string {
	if runtime.GOOS == "windows" {
		return "gm.exe"
	}
	return "gm"
}

// isOurs recognizes our own hook commands. It also matches the Node-era entry points (git-memory.mjs, cli.js), so
// re-running init replaces them.
var oursRe = regexp.MustCompile(`(?:git-memory\.mjs|[\\/]cli\.js|[\\/]gm(?:\.exe)?|\bgit-memory(?:\.exe)?)"? hook `)

func isOurs(command any) bool {
	s, ok := command.(string)
	return ok && oursRe.MatchString(s)
}

type claudeEvent struct{ name, matcher string }

var claudeEvents = []claudeEvent{
	{"SessionStart", ""},
	{"UserPromptSubmit", ""},
	{"PostToolUse", "Bash|Edit|Write|MultiEdit|NotebookEdit"},
	{"PostToolUseFailure", "Bash|Edit|Write|MultiEdit|NotebookEdit"},
	{"Stop", ""},
	{"PreCompact", ""},
	{"SessionEnd", ""},
}

var copilotEvents = []string{"sessionStart", "userPromptSubmitted", "postToolUse", "agentStop", "preCompact", "sessionEnd"}

// hookCmd builds the command line that runs one agent/event pair.
type hookCmd func(agent, event string) string

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func groupIsOurs(g any) bool {
	for _, h := range asSlice(asObj(g)["hooks"]) {
		if isOurs(asObj(h)["command"]) {
			return true
		}
	}
	return false
}

func installClaude(file string, cmd hookCmd) {
	settings := readJSON(file)
	hooks := asObj(settings["hooks"])
	for _, ev := range claudeEvents {
		kept := []any{}
		for _, g := range asSlice(hooks[ev.name]) {
			if !groupIsOurs(g) {
				kept = append(kept, g)
			}
		}
		group := obj{"hooks": []any{obj{"type": "command", "command": cmd("claude", ev.name), "timeout": 30}}}
		if ev.matcher != "" {
			group["matcher"] = ev.matcher
		}
		hooks[ev.name] = append(kept, group)
	}
	settings["hooks"] = hooks
	writeJSON(file, settings)
}

func installCopilot(file string, cmd hookCmd) {
	hooks := obj{}
	for _, ev := range copilotEvents {
		hooks[ev] = []any{obj{"type": "command", "command": cmd("copilot", ev), "cwd": ".", "timeoutSec": 30}}
	}
	writeJSON(file, obj{"version": 1, "hooks": hooks})
}

type initResult struct {
	Written []string
	Loc     *Loc
	Cfg     Config
	Scopes  *Scopes
	// Warnings are scopes whose store could not be created now (e.g. an unreachable remote); `sync` retries them.
	Warnings []string
	// Commands are the slash commands installed for Claude Code (skills) and Copilot (prompt files).
	Commands commandsResult
	// SetupIsNew is true when this run created git-memory.json (a shared project store was requested for the first time).
	SetupIsNew bool
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// installLauncher writes .git/git-memory/gm. On Unix it is a tiny sh script that execs the installed binary (so
// upgrading the binary needs no re-init), and it bakes in GIT_MEMORY_HOME so an isolated home (a sandbox, a test) stays
// isolated whatever environment an agent later launches the hooks with. Windows has no sh, so the executable itself is
// copied there.
func installLauncher(dest string) {
	exe, err := os.Executable()
	check(err)
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	mkdirAll(filepath.Dir(dest))
	if runtime.GOOS == "windows" {
		if sameFile(exe, dest) {
			return // already running from the installed copy
		}
		copyExecutable(exe, dest)
		return
	}
	home := ""
	if h := os.Getenv("GIT_MEMORY_HOME"); h != "" {
		home = "export GIT_MEMORY_HOME=" + shQuote(h) + "\n"
	}
	writeFile(dest, "#!/bin/sh\n"+home+"exec "+shQuote(exe)+" \"$@\"\n")
	check(os.Chmod(dest, 0o755))
}

func sameFile(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}

func copyExecutable(src, dest string) {
	in, err := os.Open(src)
	check(err)
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	check(err)
	_, err = io.Copy(out, in)
	out.Close()
	check(err)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		if !exists(dest) { // a running copy cannot be replaced on Windows; keep it
			check(err)
		}
	}
}

// initProject wires the project. Personal-only (plain `init`) adds nothing to the project's tracked files. Opting into a
// shared project store (`--store <repo>` or `--branch`) adds one committed file, git-memory.json (the setup). Everything
// else stays out of the project's tracked files:
//
//	.git-memory/              the shared project store's checkout (git-excluded)
//	.git/git-memory/          launcher, spool and machine-local config
//	.claude/settings.local.json, .github/hooks/git-memory.json   hook wiring (git-excluded)
//
// A teammate who clones the project runs `git-memory init` with no flags and gets the same setup.
func initProject(base BaseLoc, flags Flags) initResult {
	setupFile := projectConfigPath(base.Root)
	setup := readJSON(setupFile)
	wantsShared := flags.Has("store") || flags.Has("branch") || flags.Has("project") || flags.Has("path")
	isNew := wantsShared && !exists(setupFile)
	if wantsShared {
		store := obj{"repo": nil}
		for k, v := range asObj(setup["store"]) {
			store[k] = v
		}
		setup["store"] = store
		if s, ok := flags["store"].(string); ok {
			store["repo"] = portable(s, base.Root)
		} else if flags.Has("branch") {
			store["repo"] = nil
		}
		if s, ok := flags["project"].(string); ok {
			store["project"] = s
		}
		if s, ok := flags["path"].(string); ok {
			if filepath.IsAbs(s) || strings.HasPrefix(s, "~") {
				s = portable(s, base.Root)
			}
			store["path"] = s
		}
		writeJSON(setupFile, setup)
	}

	// Everything written into the project or a config file refers to the CLI by a path relative to the project root.
	// The only file with machine-specific paths is the launcher itself, which lives under .git (never committed).
	launcher := filepath.Join(base.Data, wrapperName())
	rel := func(p string) string {
		r, _ := filepath.Rel(base.Root, p)
		return slash(r)
	}
	commandCli := "./" + rel(launcher)
	local := readJSON(localConfigPath(base.Data))
	local["cli"] = commandCli
	writeJSON(localConfigPath(base.Data), local)

	cfg := loadConfig(base)
	scopes := resolveScopes(base, cfg)
	loc := scopes.Project
	if scopes.ProjectEnabled {
		ensureStore(loc, cfg)
	}
	var warnings []string
	for _, s := range scopes.All {
		if s.Scope == "project" || s.Mode == modeLocal {
			continue // the local personal store is created on first write
		}
		if msg := tryEnsure(s, cfg); msg != "" {
			warnings = append(warnings, s.Scope+": "+msg)
		}
	}
	installLauncher(launcher)

	var written []string
	if wantsShared {
		written = append(written, rel(setupFile))
	}
	// Hooks call the launcher through $CLAUDE_PROJECT_DIR (Claude's documented way, robust to a changed working
	// directory) or a project-relative path (Copilot runs hooks with cwd ".").
	claudeCmd := func(agent, event string) string {
		return `"$CLAUDE_PROJECT_DIR/` + rel(launcher) + `" hook ` + agent + " " + event
	}
	copilotCmd := func(agent, event string) string { return commandCli + " hook " + agent + " " + event }
	hookFile := func(relFile string, install func(string, hookCmd), cmd hookCmd) {
		install(filepath.Join(base.Root, filepath.FromSlash(relFile)), cmd)
		excludeLocally(base, "/"+relFile)
		written = append(written, relFile)
	}
	if !flags.Has("no-claude") {
		hookFile(".claude/settings.local.json", installClaude, claudeCmd)
	}
	if !flags.Has("no-copilot") {
		hookFile(".github/hooks/git-memory.json", installCopilot, copilotCmd)
	}

	// The command files may be committed, so they read the same on every machine (see commands.go).
	var cmds commandsResult
	if !flags.Has("no-commands") {
		cmds = installCommands(base, commandCli, "Bash("+commandCli+" *)", !flags.Has("no-claude"), !flags.Has("no-copilot"))
	}
	return initResult{Written: written, Loc: loc, Cfg: cfg, Scopes: scopes, Warnings: warnings, Commands: cmds, SetupIsNew: isNew}
}
