package gm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Version is the version of git-memory: 0.0.1 until the first release. A release build can set it:
// go build -ldflags "-X github.com/Rickedb/git-memory/internal/gm.Version=v1.2.3" (a leading "v", as in a git tag, is not shown).
var Version = "0.0.1"

func displayVersion() string { return strings.TrimPrefix(Version, "v") }

const helpText = `git-memory: git, pointed at your memory repository

  git-memory <git command> [args...]
      runs  git <git command> [args...]  inside the memory repository: the .git-memory folder in your home.
      Anything git understands works, and nothing is renamed:
        git-memory add . && git-memory commit -m "note about retries"
        git-memory log --oneline          git-memory grep -n -i "retry"
        git-memory remote add origin <url> && git-memory push
      It is created (git init) the first time it is needed, as long as the folder is missing or empty.

  git-memory clone <url>     use an existing memory repository: clones it into ~/.git-memory
  git-memory init            git init there, explicitly (for a folder that already has files in it)

git-memory keeps four words for itself, all unknown to git:

  git-memory doctor [--report]
                             the version, and the health of the memory repository and of the project you are in:
                             where everything is, the agents' own memories it finds (Claude Code, Copilot), whether
                             the hooks are wired, recent activity, errors.
                             Exits 1 when something is broken (--report: always 0).
  git-memory promote <source>... --to project|global
                             copy or move memories into this project's folder or the global one. A <source> is a page
                             of the memory repository (moved), or an agent's own memory (claude:<file>, copilot:<file>,
                             copilot-user:<file>) or any markdown file (copied; the original stays where it is).
  git-memory demote <page>...
                             the opposite: narrow a global memory to this project (moves it from global/ to the
                             project's folder).
  git-memory agent init [--no-claude] [--no-copilot] [--no-commands]
                             wire the project in the current directory to the memory repository: Claude Code and
                             Copilot hooks (session capture, briefing, recall) and the slash commands. Adds nothing
                             to the project's tracked files.
  git-memory agent hook <claude|copilot> <event>    the hook entry point (reads the event payload on stdin)
  git-memory agent version

Layout inside the memory repository:  projects/<project>/{sessions,memories}/   global/{memories}/
There is nothing to configure: the .git-memory folder is the setting, as .git is for git.
`

func out(a ...any) { fmt.Println(a...) }

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice != 0
}

func readStdinJSON() HookPayload {
	if isTTY(os.Stdin) {
		return HookPayload{}
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return HookPayload{}
	}
	var p HookPayload
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return HookPayload{}
	}
	return p
}

func runHook(agent, event string) {
	payload := readStdinJSON()
	err := catchAll(func() {
		if agent != "claude" && agent != "copilot" {
			fail(`unknown agent "%s"`, agent)
		}
		if o := handleHook(agent, event, payload); o != nil {
			fmt.Println(marshalJSON(o, false))
		}
	})
	if err != nil {
		// Hooks must never break the agent: log and exit cleanly.
		cwd := pickStr(payload, "cwd")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		logError(cwd, err)
	}
}

// ---- routing ----------------------------------------------------------------------------------------------------

// ownWords are the words git-memory keeps for itself. Git has no such commands (a test checks them against
// `git help -a`), so nothing a user types for git is ever intercepted.
const agentWord = "agent"

var ownWords = []string{agentWord, "doctor", "promote", "demote"}

// optionsWithValue are git's global options whose value is the next argument.
var optionsWithValue = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true}

// subcommandOf finds the git subcommand in args: the first argument that is not a global option or an option's value.
func subcommandOf(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if optionsWithValue[a] {
			i++
		}
	}
	return ""
}

// route runs `git <args>` inside the memory repository and returns git's exit code.
func route(args []string) int {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		fmt.Fprintln(os.Stderr, "git-memory: git was not found on PATH")
		return 127
	}
	mem := memoryDir()
	sub := subcommandOf(args)
	switch sub {
	case "":
		// only options (git --version, git --help, ...): no repository involved
		return execGit(gitPath, args)
	case "help", "version":
		return execGit(gitPath, args)
	case "clone":
		// clone INTO the memory repository: the folder must be missing or empty, and the destination is not the user's to give
		if isMemoryRepo(mem) || !dirIsEmpty(mem) {
			fmt.Fprintf(os.Stderr, "git-memory: %s already exists and is not empty; move it aside first, or work with it directly (git-memory log, git-memory pull)\n", mem)
			return 1
		}
		mkdirAllQuiet(filepath.Dir(mem))
		return execGit(gitPath, append(append([]string{}, args...), mem))
	case "init":
		mkdirAllQuiet(mem)
	default:
		if err := Catch(func() { ensureMemoryRepo(mem) }); err != nil {
			fmt.Fprintf(os.Stderr, "git-memory: %v\n", err)
			return 1
		}
	}
	return execGit(gitPath, append([]string{"-C", mem}, args...))
}

func mkdirAllQuiet(p string) { _ = os.MkdirAll(p, 0o755) }

// ---- the agent commands -----------------------------------------------------------------------------------------

func runAgentInit(flags Flags) {
	cwd, _ := os.Getwd()
	r := agentInit(locate(cwd), flags)
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

func runAgent(args []string) int {
	if len(args) == 0 {
		out(helpText)
		return 0
	}
	flags, pos := parseArgs(args[1:])
	switch args[0] {
	case "init":
		runAgentInit(flags)
	case "hook":
		var agent, event string
		if len(pos) > 0 {
			agent = pos[0]
		}
		if len(pos) > 1 {
			event = pos[1]
		}
		runHook(agent, event)
	case "version":
		out("git-memory " + displayVersion())
	default:
		fail(`unknown command "%s %s" (try: %s init | hook | version)`, agentWord, args[0], agentWord)
	}
	return 0
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(failure); ok {
				fmt.Fprintf(os.Stderr, "git-memory: %s\n", f.msg)
			} else {
				fmt.Fprintf(os.Stderr, "git-memory: internal error: %v\n", r)
			}
			code = 1
		}
	}()
	switch {
	case len(args) == 0:
		out(helpText)
		return 0
	case args[0] == agentWord:
		return runAgent(args[1:])
	case args[0] == "doctor":
		flags, _ := parseArgs(args[1:])
		return runDoctor(flags)
	case args[0] == "promote":
		flags, pos := parseArgs(args[1:])
		return runPromote(flags, pos)
	case args[0] == "demote":
		flags, pos := parseArgs(args[1:])
		return runDemote(flags, pos)
	case args[0] == "hook" && len(args) > 2 && (args[1] == "claude" || args[1] == "copilot"):
		// The hook entry point of earlier development builds (hook files written by an older `init`). `git hook` only knows `run`,
		// so these two agent names cannot be a git command; keeping them stops old projects' hooks from failing.
		runHook(args[1], args[2])
		return 0
	}
	return route(args)
}
