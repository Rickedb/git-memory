package gm

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

type compiledSession struct {
	Rel  string
	Meta *Meta
	Body string
	Text string
}

func pad(n int) string { return fmt.Sprintf("%02d", n) }

// compileSession turns a session's event stream into a readable markdown page. It is pure: no I/O.
func compileSession(events []Event, agent, sid string) compiledSession {
	first := Event{T: nowMs(), Ev: "start"}
	if len(events) > 0 {
		first = events[0]
	}
	last := first
	if len(events) > 0 {
		last = events[len(events)-1]
	}
	d := time.UnixMilli(first.T).UTC()
	sid8 := runeSlice(sid, 8)
	rel := fmt.Sprintf("sessions/%d/%s/%d-%s-%s-%s-%s.md", d.Year(), pad(int(d.Month())), d.Year(), pad(int(d.Month())), pad(d.Day()), agent, sid8)

	var start *Event
	var prompts []Event
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
		title = oneLine(prompts[0].Text, 80)
	}
	var tags []string
	for _, f := range fileOrder {
		if i := strings.Index(f, "/"); i >= 0 && !contains(tags, f[:i]) {
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
			body = append(body, fmt.Sprintf("%d. %s", i+1, oneLine(p.Text, 300)))
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
			line := "- `" + strings.ReplaceAll(oneLine(c.command, 160), "`", "'") + "`"
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
	meta := newMeta().Set("id", id).Set("kind", "session").Set("tier", "episodic").Set("agent", agent).Set("session", sid).
		SetList("tags", tags).Set("created", iso(time.UnixMilli(first.T))).Set("updated", iso(time.UnixMilli(last.T)))
	text := joinLines(body)
	return compiledSession{Rel: rel, Meta: meta, Body: text, Text: serializePage(meta, text)}
}

// summarize runs the optional summarizer command (an LLM or anything else). It is never required and never fatal.
func summarize(pageText string, cfg Config) string {
	if cfg.Summarizer == nil || *cfg.Summarizer == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.SummarizerTimeoutSec)*time.Second)
	defer cancel()
	out, err := shellOutput(ctx, *cfg.Summarizer, pageText)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// spliceFirstSection inserts block before the first "\n## " of body.
func spliceFirstSection(body, block string) string {
	return strings.Replace(body, "\n## ", block, 1)
}

// summarySection finds an existing `## Summary` section (up to the next `## ` heading), starting with its newline.
func summarySection(text string) string {
	const head = "\n## Summary\n"
	i := strings.Index(text, head)
	if i < 0 {
		return ""
	}
	end := strings.Index(text[i+len(head):], "\n## ")
	if end < 0 {
		return ""
	}
	return text[i : i+len(head)+end]
}

func writeSessionPage(loc *Loc, cfg Config, file string, withSummary bool) *compiledSession {
	base := strings.TrimSuffix(filepath.Base(file), ".jsonl")
	agent, sid, _ := strings.Cut(base, "-")
	events := readEvents(file)
	// A session where nothing was asked (opened and closed, or a bare resume) is not worth a page or a commit.
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
	text := page.Text
	dest := filepath.Join(loc.Wt, filepath.FromSlash(page.Rel))
	if withSummary {
		if s := summarize(text, cfg); s != "" {
			text = serializePage(page.Meta, spliceFirstSection(page.Body, "\n## Summary\n\n"+s+"\n\n## "))
		}
	} else if old, ok := readFileTry(dest); ok {
		// Keep an LLM summary written by an earlier flush of the same session.
		if m := summarySection(old); m != "" {
			text = serializePage(page.Meta, spliceFirstSection(page.Body, m+"\n## "))
		}
	}
	mkdirAll(filepath.Dir(dest))
	writeFile(dest, text)
	markCompiled(file)
	return &page
}

// flushStale compiles every spool that changed since it was last compiled (except skip). It returns how many.
func flushStale(loc *Loc, cfg Config, skip string) int {
	n := 0
	for _, file := range listSpools(loc) {
		if file == skip || isCompiled(file) {
			continue
		}
		if writeSessionPage(loc, cfg, file, true) != nil {
			n++
		}
	}
	return n
}
