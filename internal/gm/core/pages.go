package core

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	frontmatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)
	TitleLine   = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	leadTitle   = regexp.MustCompile(`^\s*#\s+.+\n?`)
	headingLine = regexp.MustCompile(`(?m)^# .+$`)
)

func ParsePage(text, rel, scope string) *Page {
	meta := NewMeta()
	body := text
	if m := frontmatter.FindStringSubmatch(text); m != nil {
		body = m[2]
		for _, line := range strings.Split(m[1], "\n") {
			i := strings.Index(line, ":")
			if i <= 0 {
				continue
			}
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			if len(v) >= 2 && strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
				var list []string
				for _, s := range strings.Split(v[1:len(v)-1], ",") {
					if s = strings.TrimSpace(s); s != "" {
						list = append(list, s)
					}
				}
				meta.SetList(k, list)
			} else {
				meta.Set(k, v)
			}
		}
	}
	title := strings.TrimSuffix(filepath.Base(rel), ".md")
	if m := TitleLine.FindStringSubmatch(body); m != nil {
		title = m[1]
	}
	p := &Page{
		Scope: scope, Status: "active", Path: rel, Meta: meta, Body: body, Title: strings.TrimSpace(title),
		Kind: "note", Tier: "semantic", Tags: meta.List("tags"),
	}
	if meta.Str("status") == "superseded" {
		p.Status = "superseded"
	}
	if k := meta.Str("kind"); k != "" {
		p.Kind = k
	}
	if t := meta.Str("tier"); t != "" {
		p.Tier = t
	}
	return p
}

func SerializePage(meta *Meta, body string) string {
	lines := []string{"---"}
	for _, k := range meta.keys {
		v := meta.vals[k]
		if v.isList {
			lines = append(lines, k+": ["+strings.Join(v.l, ", ")+"]")
		} else if v.s != "" {
			lines = append(lines, k+": "+strings.ReplaceAll(v.s, "\n", " "))
		}
	}
	lines = append(lines, "---", "", strings.TrimSpace(body), "")
	return strings.Join(lines, "\n")
}

func ListPages(wt, scope string) []*Page { return ListPagesIn(wt, scope) }

// ListPagesIn is ListPages restricted to some folders of wt (all of it when none are named). The paths of the pages stay
// relative to wt.
func ListPagesIn(wt, scope string, folders ...string) []*Page {
	var out []*Page
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.Name() == ".git" {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				walk(full)
			} else if strings.HasSuffix(e.Name(), ".md") {
				rel, _ := filepath.Rel(wt, full)
				rel = Slash(rel)
				if rel == "README.md" {
					continue
				}
				if text, ok := ReadFileTry(full); ok {
					out = append(out, ParsePage(text, rel, scope))
				}
			}
		}
	}
	if len(folders) == 0 {
		folders = []string{""}
	}
	for _, f := range folders {
		if dir := filepath.Join(wt, f); Exists(dir) {
			walk(dir)
		}
	}
	return out
}

// projectFolders are the folders of the project's folder that hold its pages. The memory root can also hold global/
// (read apart) and whatever else the memory repository has, so the project's folder is never scanned as a whole.
var projectFolders = []string{"memories", "core", "scopes", "sessions"}

// ListProjectOwn reads the pages of the project's own folder (not global).
func ListProjectOwn(p *Project) []*Page { return ListPagesIn(p.Dir(), "project", projectFolders...) }

// sectionOf is the text under `## heading` up to the next `## ` heading.
func SectionOf(body, heading string) string {
	head := regexp.MustCompile(`^##\s+` + regexp.QuoteMeta(heading) + `\s*$`)
	next := regexp.MustCompile(`^##\s`)
	var out []string
	in := false
	for _, line := range strings.Split(body, "\n") {
		switch {
		case !in && head.MatchString(line):
			in = true
		case in && next.MatchString(line):
			return strings.TrimSpace(strings.Join(out, "\n"))
		case in:
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func PageTime(p *Page) int64 {
	s := p.Meta.Updated()
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// pageRef is how a page is referred to: its path in the memory repository, e.g. memories/decision/use-pnpm.md.
func PageRef(p *Page) string { return p.Repo }

// ListProjectPages reads the pages that apply to the project: its own folder, the global one, and of its scopes only the
// one chosen on this checkout (every scope when none is).
func ListProjectPages(p *Project) []*Page {
	return ListProjectPagesIn(p, func(scope string) bool { return p.Scope == "" || scope == p.Scope })
}

// ListProjectPagesIn is ListProjectPages with the scopes to read decided by the caller (a prompt can ask to read more).
func ListProjectPagesIn(p *Project, scopeOK func(scope string) bool) []*Page {
	var out []*Page
	for _, d := range []struct {
		pages []*Page
		rel   string
	}{{ListProjectOwn(p), p.DirRel()}, {ListPages(p.GlobalDir(), "global"), p.GlobalRel()}} {
		for _, pg := range d.pages {
			if s := ScopeOf(pg.Path); s != "" && !scopeOK(s) {
				continue // another scope of the project: it is in the repository, and not read
			}
			pg.Repo = InRoot(d.rel, pg.Path)
			out = append(out, pg)
		}
	}
	return out
}

func IsActive(p *Page) bool { return p.Status == "active" }

// withoutTitle is the page body without its leading `# Title` line (the title is shown separately).
func WithoutTitle(body string) string {
	return strings.TrimSpace(leadTitle.ReplaceAllString(body, ""))
}

func SortPagesNewest(pages []*Page) {
	sort.SliceStable(pages, func(i, j int) bool { return PageTime(pages[i]) > PageTime(pages[j]) })
}
