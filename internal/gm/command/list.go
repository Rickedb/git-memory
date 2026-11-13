package command

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

const listUsage = `usage: git-memory list [repo|claude|copilot|sessions] [YYYY[-MM[-DD]]]...

Lists every memory there is for the project you are in, grouped by where it lives:
  repo      the memory repository (.git-memory): this project's memories and the global ones
  claude    Claude Code's own memory for this project
  copilot   Copilot's own memory: this workspace's and yours
  sessions  this project's session pages (the record of each agent session), newest first
With no argument, the memories of all of them (sessions are not memories, so only when asked for).
A date (a year, a month or a day) keeps the sessions of that period; on its own it means 'sessions'.
Each row ends with the name 'git-memory promote' takes (a session's is its page path).`

// listDate is a period to filter sessions by: a year, a month or a day.
var listDate = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)

// listRow is one memory in a listing.
type listRow struct{ kind, title, ref, note string }

// runList prints the memories of the memory repository and of the agents, read-only. Outside a project it lists what does
// not depend on one: the global memories and Copilot's user-level memory.
func List(_ core.Flags, pos []string) int {
	want := map[string]bool{}
	date := ""
	for _, w := range pos {
		switch {
		case w == "all":
		case w == "repo" || w == "claude" || w == "copilot" || w == "sessions":
			want[w] = true
		case listDate.MatchString(w):
			date, want["sessions"] = w, true
		default:
			core.Fail("%s", listUsage)
		}
	}
	show := func(name string) bool { return len(want) == 0 || want[name] }

	cwd, _ := os.Getwd()
	var p *core.Project
	root, mem := "", core.MemoryDirFor(cwd)
	if err := core.Catch(func() { p = core.OpenProject(cwd) }); err == nil {
		root, mem = p.Root, p.Mem
	}

	if show("repo") {
		if !core.IsMemoryRepo(mem) {
			out(".git-memory: none at " + mem + " (git-memory init creates it)")
		} else {
			if p != nil {
				listGroup(".git-memory · this project", p.DirRel(), repoRows(p.Dir(), p.DirRel()))
			}
			listGroup(".git-memory · global", "", repoRows(filepath.Join(mem, core.GlobalRel), core.GlobalRel))
		}
	}
	if want["sessions"] {
		if p == nil {
			out("sessions: they belong to a project, so run this inside one")
		} else {
			label := ".git-memory · sessions"
			if date != "" {
				label += " " + date
			}
			listGroupAs(label, p.DirRel()+"/sessions", sessionRows(p.Dir(), p.DirRel(), date), "session", "sessions")
		}
	}
	for _, s := range agentSources(root) {
		group := "claude"
		if s.Name != "claude" {
			group = "copilot"
		}
		if !show(group) || (root == "" && s.Name != "copilot-user") {
			continue
		}
		if s.Dir == "" {
			listGroup(s.Label, "none found", nil)
			continue
		}
		listGroup(s.Label, s.Dir, agentRows(s))
	}
	return 0
}

// repoRows reads the memories (not the session pages) of one folder of the memory repository, committed or not.
func repoRows(dir, rel string) []listRow {
	var rows []listRow
	for _, pg := range core.ListPages(dir, "") {
		if !strings.HasPrefix(pg.Path, "memories/") && !strings.HasPrefix(pg.Path, "core/") {
			continue
		}
		row := listRow{kind: pg.Kind, title: pg.Title, ref: rel + "/" + pg.Path}
		if pg.Tier == "core" {
			row.note = "pinned"
		}
		if pg.Status == "superseded" {
			row.note = "superseded"
		}
		rows = append(rows, row)
	}
	return rows
}

// sessionRows reads the project's session pages, newest first, keeping those whose date starts with the given period.
// A session page is named <year>-<month>-<day>-<agent>-<id>.md, so the date is in the file name.
func sessionRows(dir, rel, date string) []listRow {
	var rows []listRow
	for _, pg := range core.ListPages(dir, "") {
		if !strings.HasPrefix(pg.Path, "sessions/") {
			continue
		}
		base := filepath.Base(pg.Path)
		if date != "" && !strings.HasPrefix(base, date) {
			continue
		}
		note := base
		if len(base) >= 10 {
			note = base[:10]
		}
		if a := pg.Meta.Str("agent"); a != "" {
			note += " · " + a
		}
		rows = append(rows, listRow{kind: "session", title: pg.Title, ref: rel + "/" + pg.Path, note: note})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ref > rows[j].ref })
	return rows
}

// agentRows reads the memories one agent keeps. The ref is what `promote` takes.
func agentRows(s agentSource) []listRow {
	var rows []listRow
	for _, rel := range s.Files() {
		text, _ := core.ReadFileTry(filepath.Join(s.Dir, filepath.FromSlash(rel)))
		pg := core.ParsePage(text, rel, "")
		kind := strings.ToLower(pg.Meta.Str("kind"))
		if !core.IsKind(kind) {
			kind = kindFromType(pg.Meta.Str("type"))
		}
		title := pg.Title
		if !core.TitleLine.MatchString(pg.Body) {
			if name := pg.Meta.Str("name"); name != "" {
				title = name
			}
		}
		rows = append(rows, listRow{kind: kind, title: title, ref: s.Name + ":" + rel, note: pg.Meta.Str("description")})
	}
	return rows
}

// listGroup prints a heading and its rows, in columns.
func listGroup(label, where string, rows []listRow) {
	listGroupAs(label, where, rows, "memory", "memories")
}

func listGroupAs(label, where string, rows []listRow, one, many string) {
	count := ""
	if len(rows) > 0 {
		count = " · " + plural(len(rows), one, many)
	} else if where != "none found" {
		count = " · empty"
	}
	if where != "" {
		label += " (" + where + ")"
	}
	out(label + count)
	kindW, titleW := 0, 0
	for i := range rows {
		rows[i].title = core.Truncate(strings.Join(strings.Fields(rows[i].title), " "), 48)
		kindW = max(kindW, len([]rune(rows[i].kind)))
		titleW = max(titleW, len([]rune(rows[i].title)))
	}
	for _, r := range rows {
		line := fmt.Sprintf("  %-*s  %-*s  %s", kindW, r.kind, titleW, r.title, r.ref)
		if r.note != "" {
			line += "  — " + core.Truncate(strings.Join(strings.Fields(r.note), " "), 70)
		}
		out(line)
	}
	out()
}
