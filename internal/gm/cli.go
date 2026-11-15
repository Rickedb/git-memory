// Package gm is the git-memory command line: routing to git, the help, and dispatching its own commands. The commands
// are in package command, the agent hooks in package hook, and the memory repository and pages in package core.
package gm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/command"
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
)

// Version is the version of git-memory: 0.0.1 until the first release. A release build can set it:
// go build -ldflags "-X github.com/Rickedb/git-memory/internal/gm.Version=v1.2.3" (a leading "v", as in a git tag, is not shown).
var Version = "0.0.1"

func displayVersion() string { return strings.TrimPrefix(Version, "v") }

const helpText = `git-memory: git, pointed at your memory repository

  git-memory <git command> [args...]
      runs  git <git command> [args...]  inside the memory repository: the .git-memory folder of the folder you are
      in (the project's root, where agent init was run). Nothing is searched for: be in the folder that has it.
      Anything git understands works, and nothing is renamed:
        git-memory add . && git-memory commit -m "note about retries"
        git-memory log --oneline          git-memory grep -n -i "retry"
        git-memory remote add origin <url> && git-memory push
      Without one, git-memory says so and changes nothing: only init, clone and agent init create it.

  git-memory init            git init, here: creates the .git-memory folder in the current folder (kept out of the
                             project's git status). Also the way to use a .git-memory that already has files in it.
  git-memory clone <url>     use an existing memory repository: clones it into .git-memory in the current folder

git-memory keeps five words for itself, all unknown to git:

  git-memory doctor [--report]
                             the version, and the health of the memory repository and of the project you are in:
                             where everything is, the agents' own memories it finds (Claude Code, Copilot), whether
                             the hooks are wired, recent activity, errors.
                             Exits 1 when something is broken (--report: always 0).
  git-memory list [repo|claude|copilot]...
                             every memory there is for this project, grouped by where it lives: the memory
                             repository (this project's and the global ones), Claude Code's own memory and Copilot's.
                             Each row ends with the name promote takes.
  git-memory promote <source>... --to project|global
                             copy or move memories into this project's folder or the global one. A <source> is a page
                             of the memory repository (moved), or an agent's own memory (claude:<file>, copilot:<file>,
                             copilot-user:<file>) or any markdown file (copied; the original stays where it is).
  git-memory demote <page>...
                             the opposite: narrow a global memory to this project (moves it from global/ to the
                             project's folder).
  git-memory agent init <claude|copilot|codex|cursor|gemini|all>... [--no-commands] [--no-instructions]
                             wire the project in the current directory for the agents you name (all = every
                             agent; naming none is an error): creates its .git-memory (if missing), and for each agent its hooks (session
                             capture, briefing, recall), slash commands and instructions file.
                             Adds nothing to the project's tracked files.
  git-memory agent hook <agent> <event>    the hook entry point (reads the event payload on stdin)
  git-memory agent version

Layout inside the memory repository:  projects/<project>/{sessions,memories}/   global/{memories}/
There is nothing to configure: the .git-memory folder you are in is the setting, as .git is for git.
`

func out(a ...any) { fmt.Println(a...) }

// ---- routing ----------------------------------------------------------------------------------------------------

// ownWords are the words git-memory keeps for itself. Git has no such commands (a test checks them against
// `git help -a`), so nothing a user types for git is ever intercepted.
const agentWord = "agent"

var ownWords = []string{agentWord, "doctor", "list", "promote", "demote"}

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
	cwd, _ := os.Getwd()
	mem := core.MemoryDirFor(cwd)
	sub := subcommandOf(args)
	switch sub {
	case "":
		// only options (git --version, git --help, ...): no repository involved
		return execGit(gitPath, args)
	case "help", "version":
		return execGit(gitPath, args)
	case "clone":
		// clone INTO a .git-memory folder here: it must be missing or empty, and the destination is not the user's to give
		here := filepath.Join(cwd, core.MemoryFolder)
		if core.IsMemoryRepo(here) || !core.DirIsEmpty(here) {
			fmt.Fprintf(os.Stderr, "git-memory: %s already exists and is not empty; move it aside first, or work with it directly (git-memory log, git-memory pull)\n", here)
			return 1
		}
		excludeFromProject(cwd)
		return execGit(gitPath, append(append([]string{}, args...), here))
	case "init":
		// init means here, as for git: a .git-memory folder in the current folder
		mem = filepath.Join(cwd, core.MemoryFolder)
		mkdirAllQuiet(mem)
		core.IgnoreSessions(mem)
		excludeFromProject(cwd)
		if !hasInitialBranch(args) {
			args = append(append([]string{}, args...), "-b", "main") // and no hint about git's default branch name
		}
	default:
		if err := core.Catch(func() { core.RequireMemoryRepo(mem) }); err != nil {
			fmt.Fprintf(os.Stderr, "git-memory: %v\n", err)
			return 1
		}
	}
	return execGit(gitPath, append([]string{"-C", mem}, args...))
}

func mkdirAllQuiet(p string) { _ = os.MkdirAll(p, 0o755) }

func hasInitialBranch(args []string) bool {
	for _, a := range args {
		if a == "-b" || strings.HasPrefix(a, "--initial-branch") || strings.HasPrefix(a, "-b") && len(a) > 2 {
			return true
		}
	}
	return false
}

// excludeFromProject keeps a .git-memory folder made inside a project out of that project's `git status`, without
// touching its .gitignore (.git/info/exclude). Nothing happens outside a git project.
func excludeFromProject(dir string) {
	_ = core.Catch(func() {
		loc := core.Locate(dir)
		if rel, err := filepath.Rel(loc.Root, filepath.Join(dir, core.MemoryFolder)); err == nil && !strings.HasPrefix(rel, "..") {
			core.ExcludeLocally(loc, "/"+core.Slash(rel)+"/")
		}
	})
}

// ---- the agent commands -----------------------------------------------------------------------------------------

func runAgent(args []string) int {
	if len(args) == 0 {
		out(helpText)
		return 0
	}
	flags, pos := core.ParseArgs(args[1:])
	switch args[0] {
	case "init":
		command.AgentInit(flags, pos)
	case "hook":
		var agent, event string
		if len(pos) > 0 {
			agent = pos[0]
		}
		if len(pos) > 1 {
			event = pos[1]
		}
		hook.Run(agent, event)
	case "version":
		out("git-memory " + displayVersion())
	default:
		core.Fail(`unknown command "%s %s" (try: %s init | hook | version)`, agentWord, args[0], agentWord)
	}
	return 0
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(core.Failure); ok {
				fmt.Fprintf(os.Stderr, "git-memory: %s\n", f.Msg)
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
		flags, _ := core.ParseArgs(args[1:])
		return command.Doctor(flags, displayVersion())
	case args[0] == "list":
		flags, pos := core.ParseArgs(args[1:])
		return command.List(flags, pos)
	case args[0] == "promote":
		flags, pos := core.ParseArgs(args[1:])
		return command.Promote(flags, pos)
	case args[0] == "demote":
		flags, pos := core.ParseArgs(args[1:])
		return command.Demote(flags, pos)
	case args[0] == "hook" && len(args) > 2 && (args[1] == "claude" || args[1] == "copilot"):
		// The hook entry point of earlier development builds (hook files written by an older `init`). `git hook` only knows `run`,
		// so these two agent names cannot be a git command; keeping them stops old projects' hooks from failing.
		hook.Run(args[1], args[2])
		return 0
	}
	return route(args)
}
