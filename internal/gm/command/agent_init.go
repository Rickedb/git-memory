package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

type initResult struct {
	Written  []string
	Mem      string
	Branch   string // the branch the user's memory commits go to
	MemNote  string // why the memory repository could not be created now, if it could not
	Commands commandsResult
	// Instructions are the files that tell each agent how to read the memories; InstructionsSkipped are the user's own.
	Instructions        []string
	InstructionsSkipped []string
}

// allAgents is the name that stands for every agent git-memory serves.
const allAgents = "all"

// chooseAgents turns the names given to `agent init` into the agents to wire. Naming none is an error (so is a name that
// is not an agent): `all` wires every agent, otherwise the ones named, in wiring order, each once.
func chooseAgents(names []string) []agent.Agent {
	var known []string
	for _, a := range hook.Agents {
		known = append(known, a.Name)
	}
	if len(names) == 0 {
		core.Fail("say which agent to set up: git-memory agent init %s", agentChoices())
	}
	for _, n := range names {
		if _, ok := hook.Lookup(n); !ok && n != allAgents {
			core.Fail(`unknown agent "%s" (choose: %s)`, n, strings.Join(append(known, allAgents), ", "))
		}
	}
	if core.Contains(names, allAgents) {
		return hook.Agents
	}
	var chosen []agent.Agent
	for _, a := range hook.Agents { // in wiring order, each once
		if core.Contains(names, a.Name) {
			chosen = append(chosen, a)
		}
	}
	return chosen
}

// wireProject wires the current project to the memory repository for the given agents. It adds nothing to the project's tracked files:
//
//	.git/git-memory/gm                                            the launcher (machine-specific, never committed)
//	.claude/settings.local.json, .github/hooks/git-memory.json    hook wiring (git-excluded)
//	.claude/skills/*, .github/prompts/*                           the slash commands (git-excluded)
//	.claude/rules/git-memory.md, .github/instructions/git-memory.instructions.md
//	                                                              how to read the memories (git-excluded)
//
// The memory repository itself (<project>/.git-memory) is created if it does not exist yet.
func wireProject(base core.BaseLoc, flags core.Flags, agents []agent.Agent) initResult {
	launcher := filepath.Join(base.Data, core.WrapperName())
	rel := func(p string) string {
		r, _ := filepath.Rel(base.Root, p)
		return core.Slash(r)
	}
	commandCli := "./" + rel(launcher)
	installLauncher(launcher)
	res := initResult{}
	// The scope is this checkout's own choice: a file under .git, never committed.
	switch {
	case flags.Has("scope"):
		core.SetScope(base.Data, flags.Str("scope"))
		res.Written = append(res.Written, rel(filepath.Join(base.Data, core.ScopeFile))+"   (the scope this checkout works in; local, never committed)")
	case flags.Has("no-scope"):
		core.SetScope(base.Data, "")
		res.Written = append(res.Written, "no scope: every scope is read")
	}

	res.Mem = core.MemoryDirFor(base.Root)
	core.ExcludeLocally(base, "/"+core.MemoryFolder+"/")
	if err := core.Catch(func() {
		core.InitMemoryRepo(res.Mem)
		res.Branch, _ = core.EnsureUserBranch(res.Mem) // before anything is committed: every commit is the user's own
		if flags.Has("root") {                         // before anything is written into the root folder
			core.SetRootFolder(res.Mem, flags.Str("root"))
			res.Written = append(res.Written, core.RootFile+"   (the memory root folder; commit it with the project)")
		}
		core.EnsureSessionsIgnored(res.Mem)
	}); err != nil {
		res.MemNote = err.Error()
	}

	root := ""
	if core.IsMemoryRepo(res.Mem) {
		root = core.RootFolder(res.Mem) // fails before anything else is written if the setting is not a folder
	}

	// Each agent writes its own hook entries, in its own file, calling the launcher the way it can (see package hook).
	for _, a := range agents {
		relFile := a.HookFiles[0]
		a.Install(filepath.Join(base.Root, filepath.FromSlash(relFile)), func(event string) string { return a.Command(rel(launcher), event) })
		core.ExcludeLocally(base, "/"+relFile)
		res.Written = append(res.Written, relFile)
	}
	// The instructions file is what the agent loads on its own, so it knows where the memories are and how to read them.
	if !flags.Has("no-instructions") {
		pr := core.OpenProject(base.Root)
		who := agents
		if flags.Has("scope") || flags.Has("no-scope") { // the instructions of the agents already wired name the scope too
			who = withInstructions(base, agents)
		}
		res.Instructions, res.InstructionsSkipped = installInstructions(base, commandCli, root, pr.Scope, who)
	}
	// The command files may be committed, so they read the same on every machine (see slash.go).
	if !flags.Has("no-commands") {
		res.Commands = installCommands(base, commandCli, "Bash("+commandCli+" *)", agents)
	}
	return res
}

// AgentInit is `git-memory agent init <claude|copilot|all>...`: it wires the project in the current directory for the
// agents named (`all` is every one of them) and says what it wrote.
func AgentInit(flags core.Flags, names []string) {
	for _, a := range hook.Agents {
		if flags.Has("no-" + a.Name) { // the flags this argument replaced
			core.Fail("--no-%s is gone: name the agents you want instead, e.g. git-memory agent init %s", a.Name, otherAgentNames(a.Name))
		}
	}
	agents := chooseAgents(names)
	if flags.Has("scope") && flags.Has("no-scope") {
		core.Fail("--scope and --no-scope together: choose one")
	}
	if flags.Has("scope") {
		if flags.Str("scope") == "" {
			core.Fail("--scope needs a name, like --scope checkout")
		}
		core.NormalizeScope(flags.Str("scope")) // refuse a bad one before anything is written
	}
	if flags.Has("root") {
		root := flags.Str("root")
		if root == "" {
			core.Fail("--root needs a folder of the memory repository, like --root docs/memory")
		}
		core.NormalizeRoot(root) // refuse a bad one before anything is written
	}
	cwd, _ := os.Getwd()
	r := wireProject(core.Locate(cwd), flags, agents)
	for _, f := range r.Written {
		out("wrote " + f)
	}
	for _, f := range r.Instructions {
		out("wrote " + f + "   (how the agent reads the memories)")
	}
	for _, f := range r.InstructionsSkipped {
		out(fmt.Sprintf("note: kept your own %s (not generated by git-memory)", f))
	}
	for _, a := range agents {
		if a.Note != "" {
			out("note (" + a.Label + "): " + a.Note)
		}
	}
	out("\nmemory repository: " + r.Mem + "   (git-memory <git command> works on it)")
	if r.Branch != "" {
		out("memory branch: " + r.Branch + "   (your commits go here; git-memory sync merges them with the team's and shares them)")
	}
	if r.MemNote != "" {
		out("note: " + r.MemNote)
	}
	if len(r.Commands.Written) > 0 {
		var names []string
		for _, n := range r.Commands.Names {
			names = append(names, "/"+n)
		}
		out(fmt.Sprintf("slash commands (%s; git-excluded): %s", strings.Join(r.Commands.For, ", "), strings.Join(names, " ")))
	}
	for _, f := range r.Commands.Skipped {
		out(fmt.Sprintf("note: kept your own %s (not generated by git-memory)", f))
	}
}

// otherAgentNames lists the agents but one, for a message.
func otherAgentNames(but string) string {
	var out []string
	for _, a := range hook.Agents {
		if a.Name != but {
			out = append(out, a.Name)
		}
	}
	return strings.Join(out, " ")
}
