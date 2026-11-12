package gm

import (
	"fmt"
	"strings"
)

func day(p *Page) string { return runeSlice(p.Meta.Updated(), 10) }

// scopeTag marks the memories that apply to every project.
func scopeTag(p *Page) string {
	if p.Scope == "global" {
		return " {global}"
	}
	return ""
}

func firstLine(p *Page) string {
	for _, l := range strings.Split(p.Body, "\n") {
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "#") {
			return oneLine(l, 160)
		}
	}
	return ""
}

func filterPages(pages []*Page, keep func(*Page) bool) []*Page {
	var out []*Page
	for _, p := range pages {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

func firstN(pages []*Page, n int) []*Page {
	if len(pages) > n {
		return pages[:n]
	}
	return pages
}

// buildBriefing is the bounded context injected at session start, built from the active pages of every scope. It is
// written as plain facts, not commands. Pinned (core) pages come first, then every decision, then recent
// procedures/facts/notes and sessions.
func buildBriefing(allPages []*Page, cfg Config, project, cli string) string {
	pages := filterPages(allPages, isActive)
	if len(pages) == 0 {
		return ""
	}
	b := cfg.Briefing
	parts := []string{"# Project memory (git-memory)"}

	core := filterPages(pages, func(p *Page) bool { return p.Tier == "core" })
	if len(core) > 0 {
		parts = append(parts, "## Core notes")
		for _, p := range core {
			parts = append(parts, fmt.Sprintf("### %s%s\n%s", p.Title, scopeTag(p), truncate(withoutTitle(p.Body), 1200)))
		}
	}

	rest := filterPages(pages, func(p *Page) bool { return p.Tier != "core" && p.Kind != "session" })

	// Every decision is listed (one line each), not just the recent ones: a decision stays true until it is superseded.
	decisions := filterPages(rest, func(p *Page) bool { return p.Kind == "decision" })
	sortPagesNewest(decisions)
	if len(decisions) > 0 {
		parts = append(parts, "## Decisions in force")
		for _, p := range firstN(decisions, b.MaxDecisions) {
			parts = append(parts, fmt.Sprintf("- %s%s — %s (%s)", p.Title, scopeTag(p), firstLine(p), pageRef(p)))
		}
		if len(decisions) > b.MaxDecisions {
			parts = append(parts, fmt.Sprintf("- … %d more (search for them)", len(decisions)-b.MaxDecisions))
		}
	}

	others := filterPages(rest, func(p *Page) bool { return p.Kind != "decision" })
	sortPagesNewest(others)
	if others = firstN(others, b.RecentMemories); len(others) > 0 {
		parts = append(parts, "## Procedures, facts and notes (newest first)")
		for _, p := range others {
			parts = append(parts, fmt.Sprintf("- [%s]%s %s — %s (%s)", p.Kind, scopeTag(p), p.Title, firstLine(p), pageRef(p)))
		}
	}

	sessions := filterPages(pages, func(p *Page) bool { return p.Kind == "session" })
	sortPagesNewest(sessions)
	if sessions = firstN(sessions, b.RecentSessions); len(sessions) > 0 {
		parts = append(parts, "## Recent sessions")
		for _, p := range sessions {
			outcome, _, _ := strings.Cut(sectionOf(p.Body, "Outcome"), "\n")
			agent := p.Meta.Str("agent")
			if agent == "" {
				agent = "?"
			}
			line := fmt.Sprintf("- %s (%s): %s", day(p), agent, p.Title)
			if outcome != "" {
				line += " → " + oneLine(outcome, 200)
			}
			parts = append(parts, line+" ("+pageRef(p)+")")
		}
	}

	parts = append(parts, fmt.Sprintf("The memory is a git repository, and %[1]s is git run inside it (run from the project's root). %[1]s grep -n -i \"<terms>\" -- %[2]s %[3]s finds earlier decisions and sessions; %[1]s show HEAD:<path> prints a page; to store a new one, write a markdown page under %[2]s/memories/<kind>/ and commit it with %[1]s add / %[1]s commit.", cli, project, globalRel))
	return truncate(strings.Join(parts, "\n\n"), b.MaxChars)
}

// buildRecall finds the memories relevant to a single prompt (used where the agent supports prompt-time injection).
func buildRecall(pages []*Page, cfg Config, prompt string) string {
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
	return truncate("Possibly related project memory (git-memory):\n"+strings.Join(lines, "\n"), r.MaxChars)
}
