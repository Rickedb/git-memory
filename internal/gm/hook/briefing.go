package hook

import (
	"fmt"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

func day(p *core.Page) string { return core.RuneSlice(p.Meta.Updated(), 10) }

// scopeTag marks the memories that apply to every project.
func scopeTag(p *core.Page) string {
	if p.Scope == "global" {
		return " {global}"
	}
	return ""
}

func firstLine(p *core.Page) string {
	for _, l := range strings.Split(p.Body, "\n") {
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "#") {
			return core.OneLine(l, 160)
		}
	}
	return ""
}

func filterPages(pages []*core.Page, keep func(*core.Page) bool) []*core.Page {
	var out []*core.Page
	for _, p := range pages {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

func firstN(pages []*core.Page, n int) []*core.Page {
	if len(pages) > n {
		return pages[:n]
	}
	return pages
}

// buildBriefing is the bounded context injected at session start, built from the active pages of every scope. It is
// written as plain facts, not commands. Pinned (core) pages come first, then every decision, then recent
// procedures/facts/notes and sessions.
func buildBriefing(allPages []*core.Page, cfg core.Config, p *core.Project, cli string) string {
	pages := filterPages(allPages, core.IsActive)
	if len(pages) == 0 {
		return ""
	}
	b := cfg.Briefing
	parts := []string{"# Project memory (git-memory)"}

	pinned := filterPages(pages, func(p *core.Page) bool { return p.Tier == "core" })
	if len(pinned) > 0 {
		parts = append(parts, "## Core notes")
		for _, p := range pinned {
			parts = append(parts, fmt.Sprintf("### %s%s\n%s", p.Title, scopeTag(p), core.Truncate(core.WithoutTitle(p.Body), 1200)))
		}
	}

	rest := filterPages(pages, func(p *core.Page) bool { return p.Tier != "core" && p.Kind != "session" })

	// Every decision is listed (one line each), not just the recent ones: a decision stays true until it is superseded.
	decisions := filterPages(rest, func(p *core.Page) bool { return p.Kind == "decision" })
	core.SortPagesNewest(decisions)
	if len(decisions) > 0 {
		parts = append(parts, "## Decisions in force")
		for _, p := range firstN(decisions, b.MaxDecisions) {
			parts = append(parts, fmt.Sprintf("- %s%s — %s (%s)", p.Title, scopeTag(p), firstLine(p), core.PageRef(p)))
		}
		if len(decisions) > b.MaxDecisions {
			parts = append(parts, fmt.Sprintf("- … %d more (search for them)", len(decisions)-b.MaxDecisions))
		}
	}

	others := filterPages(rest, func(p *core.Page) bool { return p.Kind != "decision" })
	core.SortPagesNewest(others)
	if others = firstN(others, b.RecentMemories); len(others) > 0 {
		parts = append(parts, "## Procedures, facts and notes (newest first)")
		for _, p := range others {
			parts = append(parts, fmt.Sprintf("- [%s]%s %s — %s (%s)", p.Kind, scopeTag(p), p.Title, firstLine(p), core.PageRef(p)))
		}
	}

	sessions := filterPages(pages, func(p *core.Page) bool { return p.Kind == "session" })
	core.SortPagesNewest(sessions)
	if sessions = firstN(sessions, b.RecentSessions); len(sessions) > 0 {
		parts = append(parts, "## Recent sessions")
		for _, p := range sessions {
			outcome, _, _ := strings.Cut(core.SectionOf(p.Body, "Outcome"), "\n")
			agent := p.Meta.Str("agent")
			if agent == "" {
				agent = "?"
			}
			line := fmt.Sprintf("- %s (%s): %s", day(p), agent, p.Title)
			if outcome != "" {
				line += " → " + core.OneLine(outcome, 200)
			}
			parts = append(parts, line+" ("+core.PageRef(p)+")")
		}
	}

	scope := ""
	if p.Scope != "" {
		scope = fmt.Sprintf(" You are working in the scope \"%s\": only its memories, the project's own and the global ones are read (the project's other scopes are in the repository and left out), and new memories go to %s. Stay in it: look beyond it only when the user asks in that request (\"all scopes\", or naming another scope), then search %s (every scope), or that scope's folder %s. To write into another scope for one request, run %s agent scope <name>, do the work, and put it back with %s agent scope %s.", p.Scope, p.ScopeRel(), strings.Join([]string{p.Rel("memories"), p.Rel("core"), p.Rel("scopes")}, " "), p.Rel("scopes/<name>"), cli, cli, p.Scope)
	}
	parts = append(parts, fmt.Sprintf("The memory is a git repository, and %[1]s is git run inside it (run from the project's root).%[5]s %[1]s grep -n -i \"<terms>\" -- %[2]s finds earlier decisions, procedures and facts (past sessions are local files kept out of git: %[1]s list sessions lists them, and each is a plain file under %[4]s/); %[1]s show HEAD:<path> prints a page; to store a new one, write a markdown page under %[3]s/ and commit it with %[1]s add / %[1]s commit.", cli, strings.Join(p.ReadPaths(), " "), core.InRoot(p.WriteRel(), "memories/<kind>"), core.MemoryFolder, scope))
	return core.Truncate(strings.Join(parts, "\n\n"), b.MaxChars)
}

// buildRecall finds the memories relevant to a single prompt (used where the agent supports prompt-time injection).
func buildRecall(pages []*core.Page, cfg core.Config, prompt string) string {
	r := cfg.Recall
	if !r.Enabled || len([]rune(strings.TrimSpace(prompt))) < r.MinPromptChars {
		return ""
	}
	var hits []searchHit
	for _, h := range search(prompt, searchOptions{Limit: r.Limit * 3, Pages: pages}) {
		if h.Matched >= min(2, h.Terms) && (h.Kind != "session" || h.Matched >= 3) {
			hits = append(hits, h)
		}
	}
	if len(hits) > r.Limit {
		hits = hits[:r.Limit]
	}
	if len(hits) == 0 {
		return ""
	}
	var lines []string
	for _, h := range hits {
		lines = append(lines, fmt.Sprintf("- [%s]%s %s (%s): %s", h.Kind, map[bool]string{true: " {global}"}[h.Scope == "global"], h.Title, h.Ref, h.Snippet))
	}
	return core.Truncate("Possibly related project memory (git-memory):\n"+strings.Join(lines, "\n"), r.MaxChars)
}
