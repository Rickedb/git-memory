package command

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
)

const scopeUsage = `usage: git-memory agent scope [<name>|--clear]

Chooses the scope this checkout works in: a part of the project (a module of an app) whose memories live in
scopes/<name>/. Only that scope's memories, the project's own and the global ones are read: the
others stay in the repository and are left out. Without a name it says which scope is chosen.
  git-memory agent scope checkout     work in the "checkout" scope
  git-memory agent scope --clear      read every scope again
The choice is local to this checkout (under .git, never committed). To read beyond it for one request, say so in the
prompt ("across all scopes", or the name of another scope).`

// AgentScope is `git-memory agent scope [<name>|--clear]`: it shows, sets or clears the scope this checkout works in, and
// rewrites the instructions of the agents the project is wired for, so they name it.
func AgentScope(flags core.Flags, pos []string) {
	if len(pos) > 1 || (len(pos) == 1 && flags.Has("clear")) {
		core.Fail("%s", scopeUsage)
	}
	cwd, _ := os.Getwd()
	base := core.Locate(cwd)
	switch {
	case flags.Has("clear"):
		core.SetScope(base.Data, "")
		out("no scope: every scope is read")
	case len(pos) == 1:
		core.SetScope(base.Data, pos[0])
		out("scope: " + core.ReadScope(base.Data) + "   (local to this checkout: " + filepath.ToSlash(filepath.Join(".git", "git-memory", core.ScopeFile)) + ")")
	default:
		p := core.OpenProject(cwd)
		if p.Scope == "" {
			out("no scope chosen: every scope is read" + existingScopes(p))
		} else {
			out("scope: " + p.Scope + existingScopes(p))
		}
		return
	}
	for _, f := range refreshInstructions(base) {
		out("wrote " + f + "   (how the agent reads the memories)")
	}
}

func existingScopes(p *core.Project) string {
	if names := p.Scopes(); len(names) > 0 {
		return "   (scopes in the project: " + strings.Join(names, ", ") + ")"
	}
	return ""
}

// refreshInstructions rewrites the instructions file of every agent that already has one of ours, so a change of scope
// reaches all the agents the project is wired for. It returns the files written.
func refreshInstructions(base core.BaseLoc) []string {
	p := core.OpenProject(base.Root)
	var wired []string
	for _, a := range hook.Agents {
		if a.Instructions == "" {
			continue
		}
		if f := filepath.Join(base.Root, filepath.FromSlash(a.Instructions)); core.Exists(f) && isGenerated(f) {
			wired = append(wired, a.Name)
		}
	}
	var agents = hook.Agents[:0:0]
	for _, a := range hook.Agents {
		if core.Contains(wired, a.Name) {
			agents = append(agents, a)
		}
	}
	launcher := "./" + core.Slash(mustRel(base.Root, filepath.Join(base.Data, core.WrapperName())))
	written, _ := installInstructions(base, launcher, p.Base, p.Scope, agents)
	return written
}

func mustRel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}
