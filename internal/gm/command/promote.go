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

func resolvePromoteSource(p *core.Project, arg string) promoteSource {
	return resolveSource(p, arg, nil)
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
