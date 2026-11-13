package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

const promoteUsage = `usage: git-memory promote <source>... --to project|global

  --to project   this project's folder in the memory repository (projects/<project>/)
  --to global    the folder whose memories apply to every project (global/)

<source> is any of:
  a page of the memory repository      projects/shop/memories/decision/use-rest.md   (it is moved)
  an agent's own memory                claude:<file>   copilot:<file>   copilot-user:<file>   (it is copied)
  any markdown file                    ./notes/retries.md                              (it is copied)
'git-memory doctor' lists the agents' memories it finds.`

// promoteSource is where a promotion starts.
type promoteSource struct {
	repoPath string // set: a page of the memory repository (moved)
	file     string // else: a file on disk (copied)
	name     string // claude | copilot | copilot-user | file
	rel      string // the file's name inside its source, for the record
}

// runPromote copies or moves memories into the project's folder or the global one.
func Promote(flags core.Flags, pos []string) int {
	to := flags.Str("to")
	if len(pos) == 0 || (to != "project" && to != "global") {
		core.Fail("%s", promoteUsage)
	}
	cwd, _ := os.Getwd()
	var p *core.Project
	if err := core.Catch(func() { p = core.OpenProject(cwd) }); err != nil {
		core.Fail("promote runs inside a project (a git repository): %v", err)
	}
	core.RequireMemoryRepo(p.Mem)
	code := 0
	for _, arg := range pos {
		if err := core.Catch(func() { out(promoteOne(p, arg, to)) }); err != nil {
			fmt.Fprintf(os.Stderr, "git-memory: %v\n", err)
			code = 1
		}
	}
	return code
}

func promoteOne(p *core.Project, arg, to string) string {
	dest := core.GlobalRel
	if to == "project" {
		dest = p.DirRel()
	}
	src := resolvePromoteSource(p, arg)
	if src.repoPath != "" {
		return movePage(p, src.repoPath, dest, to, "promote")
	}
	return copyPage(p, src, dest)
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

func resolvePromoteSource(p *core.Project, arg string) promoteSource {
	return resolveSource(p, arg, nil)
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

// folderOf is the folder of the memory repository a page lives in: a project's, or global.
func folderOf(repoPath string) string {
	if strings.HasPrefix(repoPath, core.GlobalRel+"/") {
		return core.GlobalRel
	}
	if rest, ok := strings.CutPrefix(repoPath, "projects/"); ok {
		if name, _, ok := strings.Cut(rest, "/"); ok && name != "" {
			return "projects/" + name
		}
	}
	return ""
}

// movePage moves a page of the memory repository to another folder and commits the move.
func movePage(p *core.Project, repoPath, dest, to, verb string) string {
	from := folderOf(repoPath)
	if from == "" {
		core.Fail("%s is not a page of a project or of the global folder", repoPath)
	}
	if from == dest {
		core.Fail("%s is already in %s", repoPath, dest)
	}
	target := dest + "/" + strings.TrimPrefix(repoPath, from+"/")
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

// copyPage turns a file that lives outside the memory repository (an agent's own memory, or any markdown file) into a
// page of the destination folder. The original stays exactly where it is.
func copyPage(p *core.Project, src promoteSource, dest string) string {
	text, ok := core.ReadFileTry(src.file)
	if !ok {
		core.Fail("cannot read %s", src.file)
	}
	pg := core.ParsePage(text, src.rel, "")
	kind := strings.ToLower(pg.Meta.Str("kind"))
	if !core.IsKind(kind) {
		kind = kindFromType(pg.Meta.Str("type"))
	}
	title := pg.Title
	if !core.TitleLine.MatchString(pg.Body) { // no heading of its own: the agent's name for it, else the file's
		if name := pg.Meta.Str("name"); name != "" {
			title = name
		}
		title = strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(title))
	}
	body := core.WithoutTitle(pg.Body)
	if d := pg.Meta.Str("description"); d != "" {
		body = strings.TrimSpace(d + "\n\n" + body)
	}
	rel := "memories/" + kind + "/" + core.Slugify(title, 60) + ".md"
	target := dest + "/" + rel
	if core.Exists(filepath.Join(p.Mem, filepath.FromSlash(target))) {
		core.Fail("%s was already promoted (%s exists)", src.name+":"+src.rel, target)
	}
	tags := append(append([]string{}, pg.Tags...), "from-"+src.name)
	made := p.RememberIn(dest, core.RememberInput{Text: body, Kind: kind, Title: title, Tags: tags, Extra: [][2]string{{"source", src.name + ":" + src.rel}}})
	return "promoted " + src.name + ":" + src.rel + " → " + made
}

const demoteUsage = `usage: git-memory demote <page>...

Narrows a global memory to this project: moves it from global/ to this project's folder
(projects/<project>/), the opposite of 'git-memory promote <page> --to global'.
<page> is a page of the memory repository (its path, or a unique part of it).`

// runDemote moves global memories into the current project's folder.
func Demote(flags core.Flags, pos []string) int {
	if len(pos) == 0 {
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
		if err := core.Catch(func() { out(demoteOne(p, arg)) }); err != nil {
			fmt.Fprintf(os.Stderr, "git-memory: %v\n", err)
			code = 1
		}
	}
	return code
}

func isGlobalPage(repoPath string) bool { return strings.HasPrefix(repoPath, core.GlobalRel+"/") }

func demoteOne(p *core.Project, arg string) string {
	if name, _, ok := strings.Cut(arg, ":"); ok && isAgentSource(name) {
		core.Fail("%s is an agent's own memory: demote narrows pages of the memory repository (git-memory promote brings it in)", arg)
	}
	var src promoteSource
	if err := core.Catch(func() { src = resolveSource(p, arg, isGlobalPage) }); err != nil {
		// say why it is not a global page, if it is one of the others
		var other promoteSource
		if core.Catch(func() { other = resolveSource(p, arg, nil) }) == nil && other.repoPath != "" {
			if folder := folderOf(other.repoPath); folder == p.DirRel() {
				core.Fail("%s is already in this project's folder: nothing is narrower than a project", other.repoPath)
			} else if folder != "" {
				core.Fail("%s belongs to %s: demote narrows global pages to this project (git-memory promote --to project brings it here)", other.repoPath, folder)
			}
		}
		core.Fail("%v", err)
	}
	if src.repoPath == "" {
		core.Fail("%s is not a page of the memory repository: demote narrows global pages (git-memory promote brings other memories in)", arg)
	}
	return movePage(p, src.repoPath, p.DirRel(), "project", "demote")
}
