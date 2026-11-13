package hook

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

type compiledSession struct {
	Rel  string
	Meta *core.Meta
	Body string
	Text string
}

func pad(n int) string { return fmt.Sprintf("%02d", n) }

// compileSession turns a session's event stream into a readable markdown page. It is pure: no I/O.
func compileSession(events []core.Event, agent, sid string) compiledSession {
	first := core.Event{T: core.NowMs(), Ev: "start"}
	if len(events) > 0 {
		first = events[0]
	}
	last := first
	if len(events) > 0 {
		last = events[len(events)-1]
	}
	d := time.UnixMilli(first.T).UTC()
	sid8 := core.RuneSlice(sid, 8)
	rel := fmt.Sprintf("sessions/%d/%s/%d-%s-%s-%s-%s.md", d.Year(), pad(int(d.Month())), d.Year(), pad(int(d.Month())), pad(d.Day()), agent, sid8)

	var start *core.Event
	var prompts []core.Event
	for i := range events {
		switch events[i].Ev {
		case "start":
			if start == nil {
				start = &events[i]
			}
		case "prompt":
			prompts = append(prompts, events[i])
		}
	}
	var fileOrder []string
	fileCount := map[string]int{}
	type cmd struct {
		command string
		failed  bool
	}
	var cmds []cmd
	failures := 0
	for _, e := range events {
		if e.Ev != "tool" {
			continue
		}
		for _, f := range e.Files {
			if _, ok := fileCount[f]; !ok {
				fileOrder = append(fileOrder, f)
			}
			fileCount[f]++
		}
		if e.Command != "" {
			cmds = append(cmds, cmd{e.Command, e.Failed})
		}
		if e.Failed {
			failures++
		}
	}
	outcome := ""
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Ev == "stop" && events[i].Last != "" {
			outcome = events[i].Last
			break
		}
	}

	title := "Session " + sid8
	if len(prompts) > 0 {
		title = core.OneLine(prompts[0].Text, 80)
	}
	var tags []string
	for _, f := range fileOrder {
		if i := strings.Index(f, "/"); i >= 0 && !core.Contains(tags, f[:i]) {
			tags = append(tags, f[:i])
		}
	}
	if len(tags) > 5 {
		tags = tags[:5]
	}

	body := []string{"# " + title, ""}
	agentLine := "- Agent: " + agent
	if start != nil && start.Branch != "" {
		agentLine += " · branch `" + start.Branch + "`"
	}
	if start != nil && start.Head != "" {
		agentLine += " · from `" + start.Head + "`"
	}
	minutes := int(math.Max(1, math.Round(float64(last.T-first.T)/60000)))
	body = append(body, agentLine, fmt.Sprintf("- Duration: %d min · %d prompt(s)", minutes, len(prompts)), "")
	if outcome != "" {
		body = append(body, "## Outcome", "", strings.TrimSpace(outcome), "")
	}
	if len(prompts) > 0 {
		body = append(body, "## Prompts", "")
		for i, p := range prompts {
			if i >= 20 {
				break
			}
			body = append(body, fmt.Sprintf("%d. %s", i+1, core.OneLine(p.Text, 300)))
		}
		if len(prompts) > 20 {
			body = append(body, fmt.Sprintf("- … %d more", len(prompts)-20))
		}
		body = append(body, "")
	}
	if len(fileOrder) > 0 {
		body = append(body, "## Files touched", "")
		for i, f := range fileOrder {
			if i >= 40 {
				break
			}
			line := "- `" + f + "`"
			if n := fileCount[f]; n > 1 {
				line += fmt.Sprintf(" ×%d", n)
			}
			body = append(body, line)
		}
		body = append(body, "")
	}
	if len(cmds) > 0 {
		body = append(body, "## Commands", "")
		seen := map[string]bool{}
		for i := len(cmds) - 1; i >= 0; i-- { // newest first
			c := cmds[i]
			if seen[c.command] || len(seen) >= 15 {
				continue
			}
			seen[c.command] = true
			line := "- `" + strings.ReplaceAll(core.OneLine(c.command, 160), "`", "'") + "`"
			if c.failed {
				line += " (failed)"
			}
			body = append(body, line)
		}
		body = append(body, "")
	}
	if failures > 0 {
		body = append(body, "## Failures", "", fmt.Sprintf("%d tool call(s) failed.", failures), "")
	}

	id := strings.TrimSuffix(strings.TrimPrefix(rel, "sessions/"), ".md")
	meta := core.NewMeta().Set("id", id).Set("kind", "session").Set("tier", "episodic").Set("agent", agent).Set("session", sid).
		SetList("tags", tags).Set("created", core.Iso(time.UnixMilli(first.T))).Set("updated", core.Iso(time.UnixMilli(last.T)))
	text := core.JoinLines(body)
	return compiledSession{Rel: rel, Meta: meta, Body: text, Text: core.SerializePage(meta, text)}
}

// writeSessionPage compiles one session's spool into its page under the project's sessions/ folder. A session where
// nothing was asked (opened and closed, or a bare resume) is not worth a page or a commit.
func writeSessionPage(p *core.Project, file string) *compiledSession {
	base := strings.TrimSuffix(filepath.Base(file), ".jsonl")
	agent, sid, _ := strings.Cut(base, "-")
	events := readEvents(file)
	asked := false
	for _, e := range events {
		if e.Ev == "prompt" {
			asked = true
			break
		}
	}
	if !asked {
		if len(events) > 0 {
			markCompiled(file)
		}
		return nil
	}
	page := compileSession(events, agent, sid)
	dest := filepath.Join(p.Dir(), filepath.FromSlash(page.Rel))
	core.MkdirAll(filepath.Dir(dest))
	core.WriteFile(dest, page.Text)
	markCompiled(file)
	return &page
}

// flushStale compiles every spool of this project that changed since it was last compiled (except skip). It returns how
// many pages were written.
func flushStale(p *core.Project, skip string) int {
	n := 0
	for _, file := range ListSpools(p.Spool) {
		if file == skip || isCompiled(file) {
			continue
		}
		if writeSessionPage(p, file) != nil {
			n++
		}
	}
	return n
}
