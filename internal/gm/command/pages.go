// Helpers shared by promote and demote: finding a page in the memory repository and moving it between folders.
package command

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

// promoteSource is where a promotion starts.
type promoteSource struct {
	repoPath string // set: a page of the memory repository (moved)
	file     string // else: a file on disk (copied)
	name     string // claude | copilot | copilot-user | file
	rel      string // the file's name inside its source, for the record
}

// within reports path as relative to dir when it is inside it.
func within(dir, path string) (string, bool) {
	rel, err := filepath.Rel(realOr(dir), realOr(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return core.Slash(rel), true
}

func realOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// pickOne finds the single candidate that matches want exactly (also without .md) or, failing that, contains it.
func pickOne(candidates []string, want, what string) string {
	want = strings.TrimSuffix(want, ".md")
	var exact, partial []string
	for _, c := range candidates {
		base := strings.TrimSuffix(c, ".md")
		switch {
		case base == want:
			exact = append(exact, c)
		case strings.Contains(strings.ToLower(base), strings.ToLower(want)):
			partial = append(partial, c)
		}
	}
	hits := exact
	if len(hits) == 0 {
		hits = partial
	}
	switch len(hits) {
	case 1:
		return hits[0]
	case 0:
		core.Fail("no %s matches %q", what, want)
	}
	core.Fail("%q matches more than one %s: %s", want, what, strings.Join(hits, ", "))
	return ""
}

// resolveSource is resolvePromoteSource, optionally limited to the pages of the memory repository that keep accepts.
func resolveSource(p *core.Project, arg string, keep func(repoPath string) bool) promoteSource {
	// an agent's own memory: claude:<file>
	if name, rest, ok := strings.Cut(arg, ":"); ok && isAgentSource(name) && rest != "" {
		for _, s := range agentSources(p.Root) {
			if s.Name != name {
				continue
			}
			if s.Dir == "" {
				core.Fail("%s: none found for this project (git-memory doctor shows what is found)", s.Label)
			}
			rel := pickOne(s.Files(), rest, s.Label)
			return promoteSource{file: filepath.Join(s.Dir, filepath.FromSlash(rel)), name: name, rel: rel}
		}
	}
	// a file on disk
	if st, err := os.Stat(arg); err == nil && !st.IsDir() {
		abs, _ := filepath.Abs(arg)
		if rel, ok := within(p.Mem, abs); ok {
			return promoteSource{repoPath: rel}
		}
		return promoteSource{file: abs, name: "file", rel: filepath.Base(abs)}
	}
	// a page of the memory repository, by its path or part of it
	var pages []string
	for _, f := range strings.Split(core.Git(p.Mem, "ls-files", "--cached", "--others", "--exclude-standard"), "\n") { // also pages not committed yet
		if strings.HasSuffix(f, ".md") && (keep == nil || keep(f)) {
			pages = append(pages, f)
		}
	}
	return promoteSource{repoPath: pickOne(pages, arg, "page in the memory repository")}
}

// needScope is the active scope's folder, or an error saying how to choose one.
func needScope(p *core.Project) string {
	if p.Scope == "" {
		core.Fail("no scope is chosen on this checkout: git-memory agent scope <name> chooses one")
	}
	return p.ScopeRel()
}

// folderOf is the folder of the memory repository a page lives in: the project's own, one of its scopes, or global. It
// reports false for a file that is not a memory page (the folder can be "" when the memory root is the repository's top).
func folderOf(p *core.Project, repoPath string) (string, bool) {
	if strings.HasPrefix(repoPath, p.GlobalRel()+"/") {
		return p.GlobalRel(), true
	}
	rest := repoPath
	if p.Base != "" {
		var ok bool
		if rest, ok = strings.CutPrefix(repoPath, p.Base+"/"); !ok {
			return "", false
		}
	}
	first, inside, _ := strings.Cut(rest, "/")
	switch first {
	case "memories", "core":
		return p.DirRel(), true
	case "scopes":
		if name, _, ok := strings.Cut(inside, "/"); ok && name != "" {
			return p.Rel("scopes/" + name), true
		}
	}
	return "", false
}

// below is a page's path inside the folder it lives in.
func below(folder, repoPath string) string {
	if folder == "" {
		return repoPath
	}
	return strings.TrimPrefix(repoPath, folder+"/")
}

// movePage moves a page of the memory repository to another folder and commits the move.
func movePage(p *core.Project, repoPath, dest, to, verb string) string {
	from, ok := folderOf(p, repoPath)
	if !ok {
		core.Fail("%s is not a memory page of this project or of the global folder", repoPath)
	}
	if from == dest {
		core.Fail("%s is already in %s", repoPath, core.InRoot(dest, ""))
	}
	target := core.InRoot(dest, below(from, repoPath))
	srcAbs := filepath.Join(p.Mem, filepath.FromSlash(repoPath))
	dstAbs := filepath.Join(p.Mem, filepath.FromSlash(target))
	if core.Exists(dstAbs) {
		core.Fail("%s already exists", target)
	}
	title := core.ParsePage(readFileOr(srcAbs), repoPath, "").Title
	core.WithLock(p.Mem, func() {
		core.MkdirAll(filepath.Dir(dstAbs))
		core.Check(os.Rename(srcAbs, dstAbs))
		paths := []string{target}
		if _, inIndex := core.GitTry(p.Mem, "ls-files", "--error-unmatch", "--", repoPath); inIndex {
			if _, inHead := core.GitTry(p.Mem, "cat-file", "-e", "HEAD:"+repoPath); inHead {
				paths = append(paths, repoPath) // stages the deletion too, so the commit is a rename
			} else {
				core.Git(p.Mem, "rm", "--cached", "-q", "--", repoPath) // only ever staged: forget it, there is nothing to rename
			}
		}
		core.CommitPaths(p.Mem, "memory: "+verb+" "+core.OneLine(title, 60)+" to "+to, paths...)
	})
	return verb + "d " + repoPath + " → " + target
}

func readFileOr(p string) string {
	s, _ := core.ReadFileTry(p)
	return s
}
