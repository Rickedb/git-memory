package command

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

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
	for _, h := range asSlice(core.AsObj(g)["hooks"]) {
		if isOurs(core.AsObj(h)["command"]) {
			return true
		}
	}
	return false
}

func installClaude(file string, cmd hookCmd) {
	settings := core.ReadJSON(file)
	hooks := core.AsObj(settings["hooks"])
	for _, ev := range claudeEvents {
		kept := []any{}
		for _, g := range asSlice(hooks[ev.name]) {
			if !groupIsOurs(g) {
				kept = append(kept, g)
			}
		}
		group := core.Obj{"hooks": []any{core.Obj{"type": "command", "command": cmd("claude", ev.name), "timeout": 30}}}
		if ev.matcher != "" {
			group["matcher"] = ev.matcher
		}
		hooks[ev.name] = append(kept, group)
	}
	settings["hooks"] = hooks
	core.WriteJSON(file, settings)
}

func installCopilot(file string, cmd hookCmd) {
	hooks := core.Obj{}
	for _, ev := range copilotEvents {
		hooks[ev] = []any{core.Obj{"type": "command", "command": cmd("copilot", ev), "cwd": ".", "timeoutSec": 30}}
	}
	core.WriteJSON(file, core.Obj{"version": 1, "hooks": hooks})
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

// wireProject wires the current project to the memory repository. It adds nothing to the project's tracked files:
//
//	.git/git-memory/gm                                            the launcher (machine-specific, never committed)
//	.claude/settings.local.json, .github/hooks/git-memory.json    hook wiring (git-excluded)
//	.claude/skills/*, .github/prompts/*                           the slash commands (git-excluded)
//
// The memory repository itself (<project>/.git-memory) is created if it does not exist yet.
func wireProject(base core.BaseLoc, flags core.Flags) initResult {
	launcher := filepath.Join(base.Data, core.WrapperName())
	rel := func(p string) string {
		r, _ := filepath.Rel(base.Root, p)
		return core.Slash(r)
	}
	commandCli := "./" + rel(launcher)
	installLauncher(launcher)

	res := initResult{Mem: core.MemoryDirFor(base.Root)}
	core.ExcludeLocally(base, "/"+core.MemoryFolder+"/")
	if err := core.Catch(func() { core.InitMemoryRepo(res.Mem) }); err != nil {
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
		core.ExcludeLocally(base, "/"+relFile)
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

// runAgentInit is `git-memory agent init`: it wires the project in the current directory and says what it wrote.
func AgentInit(flags core.Flags) {
	cwd, _ := os.Getwd()
	r := wireProject(core.Locate(cwd), flags)
	for _, f := range r.Written {
		out("wrote " + f)
	}
	out("\nmemory repository: " + r.Mem + "   (git-memory <git command> works on it)")
	if r.MemNote != "" {
		out("note: " + r.MemNote)
	}
	if len(r.Commands.Written) > 0 {
		seen := map[string]bool{}
		var names []string
		for _, f := range r.Commands.Written {
			n := "/" + filepath.Base(filepath.Dir(f))
			if strings.HasSuffix(f, ".prompt.md") {
				n = "/" + strings.TrimSuffix(filepath.Base(f), ".prompt.md")
			}
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		out(fmt.Sprintf("slash commands (Claude Code skills, Copilot prompt files; git-excluded): %s", strings.Join(names, " ")))
	}
	for _, f := range r.Commands.Skipped {
		out(fmt.Sprintf("note: kept your own %s (not generated by git-memory)", f))
	}
}
