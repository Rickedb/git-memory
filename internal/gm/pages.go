package gm

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
	titleLine   = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	leadTitle   = regexp.MustCompile(`^\s*#\s+.+\n?`)
	headingLine = regexp.MustCompile(`(?m)^# .+$`)
)

func parsePage(text, rel, scope string) *Page {
	meta := newMeta()
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
	if m := titleLine.FindStringSubmatch(body); m != nil {
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

func serializePage(meta *Meta, body string) string {
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

func listPages(wt, scope string) []*Page {
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
				rel = slash(rel)
				if rel == "README.md" {
					continue
				}
				if text, ok := readFileTry(full); ok {
					out = append(out, parsePage(text, rel, scope))
				}
			}
		}
	}
	if exists(wt) {
		walk(wt)
	}
	return out
}

// sectionOf is the text under `## heading` up to the next `## ` heading.
func sectionOf(body, heading string) string {
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

func pageTime(p *Page) int64 {
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

// pageRef is how a page is referred to: its path in the memory repository, e.g. projects/shop/memories/decision/use-pnpm.md.
func pageRef(p *Page) string { return p.Repo }

// listProjectPages reads the pages that apply to the project: its own folder and the global one.
func listProjectPages(p *Project) []*Page {
	var out []*Page
	for _, d := range []struct{ dir, scope, rel string }{{p.Dir(), "project", p.DirRel()}, {p.GlobalDir(), "global", globalRel}} {
		for _, pg := range listPages(d.dir, d.scope) {
			pg.Repo = d.rel + "/" + pg.Path
			out = append(out, pg)
		}
	}
	return out
}

func isActive(p *Page) bool { return p.Status == "active" }

// withoutTitle is the page body without its leading `# Title` line (the title is shown separately).
func withoutTitle(body string) string {
	return strings.TrimSpace(leadTitle.ReplaceAllString(body, ""))
}

func sortPagesNewest(pages []*Page) {
	sort.SliceStable(pages, func(i, j int) bool { return pageTime(pages[i]) > pageTime(pages[j]) })
}
