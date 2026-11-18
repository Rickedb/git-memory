package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

const demoteUsage = `usage: git-memory demote <page>... [--to project|scope]

Narrows a global memory to this project: moves it from global/ to this project's folder
(the memory root folder), the opposite of 'git-memory promote <page> --to global'. With --to scope it
goes into the scope you work in (scopes/<scope>/) instead.
<page> is a page of the memory repository (its path, or a unique part of it).`

// runDemote moves global memories into the current project's folder.
func Demote(flags core.Flags, pos []string) int {
	to := flags.Str("to")
	if len(pos) == 0 || (to != "" && to != "project" && to != "scope") {
		core.Fail("%s", demoteUsage)
	}
	cwd, _ := os.Getwd()
	var p *core.Project
	if err := core.Catch(func() { p = core.OpenProject(cwd) }); err != nil {
		core.Fail("demote runs inside a project (a git repository): %v", err)
	}
	core.RequireMemoryRepo(p.Mem)
	code := 0
	for _, arg := range pos {
		if err := core.Catch(func() { out(demoteOne(p, arg, to)) }); err != nil {
			fmt.Fprintf(os.Stderr, "git-memory: %v\n", err)
			code = 1
		}
	}
	return code
}

func isGlobalPage(p *core.Project) func(string) bool {
	return func(repoPath string) bool { return strings.HasPrefix(repoPath, p.GlobalRel()+"/") }
}

func demoteOne(p *core.Project, arg, to string) string {
	if name, _, ok := strings.Cut(arg, ":"); ok && isAgentSource(name) {
		core.Fail("%s is an agent's own memory: demote narrows pages of the memory repository (git-memory promote brings it in)", arg)
	}
	var src promoteSource
	if err := core.Catch(func() { src = resolveSource(p, arg, isGlobalPage(p)) }); err != nil {
		// say why it is not a global page, if it is one of the others
		var other promoteSource
		if core.Catch(func() { other = resolveSource(p, arg, nil) }) == nil && other.repoPath != "" {
			if folder, ok := folderOf(p, other.repoPath); ok && folder == p.DirRel() {
				core.Fail("%s is already in this project's own memories: nothing is narrower than a project", other.repoPath)
			} else if ok {
				core.Fail("%s belongs to %s: demote narrows global pages to this project (git-memory promote --to project brings it here)", other.repoPath, folder)
			}
		}
		core.Fail("%v", err)
	}
	if src.repoPath == "" {
		core.Fail("%s is not a page of the memory repository: demote narrows global pages (git-memory promote brings other memories in)", arg)
	}
	if to == "scope" {
		return movePage(p, src.repoPath, needScope(p), "scope", "demote")
	}
	return movePage(p, src.repoPath, p.DirRel(), "project", "demote")
}
