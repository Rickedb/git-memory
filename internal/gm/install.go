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
var oursRe = regexp.MustCompile(`(?:git-memory\.mjs|[\\/]cli\.js|[\\/]gm(?:\.exe)?|\bgit-memory(?:\.exe)?)"? (?:agent )?hook `)

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
	Written  []string
	Mem      string
	MemNote  string // why the memory repository could not be created now, if it could not
	Commands commandsResult
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// installLauncher writes .git/git-memory/gm. On Unix it is a tiny sh script that execs the installed binary, so upgrading
// the binary needs no re-init and agents launched without your shell's PATH still find it. Windows has no sh, so the
// executable itself is copied there.
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
	writeFile(dest, "#!/bin/sh\nexec "+shQuote(exe)+" \"$@\"\n")
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

// agentInit wires the current project to the memory repository. It adds nothing to the project's tracked files:
//
//	.git/git-memory/gm                                            the launcher (machine-specific, never committed)
//	.claude/settings.local.json, .github/hooks/git-memory.json    hook wiring (git-excluded)
//	.claude/skills/*, .github/prompts/*                           the slash commands (git-excluded)
//
// The memory repository itself (~/.git-memory) is created if it does not exist yet.
func agentInit(base BaseLoc, flags Flags) initResult {
	launcher := filepath.Join(base.Data, wrapperName())
	rel := func(p string) string {
		r, _ := filepath.Rel(base.Root, p)
		return slash(r)
	}
	commandCli := "./" + rel(launcher)
	installLauncher(launcher)

	res := initResult{Mem: memoryDir()}
	if err := Catch(func() { ensureMemoryRepo(res.Mem) }); err != nil {
		res.MemNote = err.Error()
	}

	// Hooks call the launcher through $CLAUDE_PROJECT_DIR (Claude's documented way, robust to a changed working
	// directory) or a project-relative path (Copilot runs hooks with cwd ".").
	claudeCmd := func(agent, event string) string {
		return `"$CLAUDE_PROJECT_DIR/` + rel(launcher) + `" agent hook ` + agent + " " + event
	}
	copilotCmd := func(agent, event string) string { return commandCli + " agent hook " + agent + " " + event }
	hookFile := func(relFile string, install func(string, hookCmd), cmd hookCmd) {
		install(filepath.Join(base.Root, filepath.FromSlash(relFile)), cmd)
		excludeLocally(base, "/"+relFile)
		res.Written = append(res.Written, relFile)
	}
	if !flags.Has("no-claude") {
		hookFile(".claude/settings.local.json", installClaude, claudeCmd)
	}
	if !flags.Has("no-copilot") {
		hookFile(".github/hooks/git-memory.json", installCopilot, copilotCmd)
	}
	// The command files may be committed, so they read the same on every machine (see commands.go).
	if !flags.Has("no-commands") {
		res.Commands = installCommands(base, commandCli, "Bash("+commandCli+" *)", !flags.Has("no-claude"), !flags.Has("no-copilot"))
	}
	return res
}
