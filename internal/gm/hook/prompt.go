package hook

import (
	"fmt"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// prompt records what the user asked, saves the memories the prompt itself asks for ("remember that …"), and, for an
// agent that can take it, hands back the memories related to the prompt.
func (s *session) prompt() any {
	text := core.StripHarnessContext(pickStr(s.payload, "prompt"))
	if text == "" {
		return nil // the prompt was only editor context
	}
	s.record(core.Event{Ev: "prompt", Text: core.Clean(text, s.cfg.Capture.MaxPromptChars)})
	saved := saveTriggers(s.p, s.cfg, s.agent.Name+":"+s.sid, text)
	if !s.agent.Recall {
		return nil
	}
	// what this very prompt just saved is not "related memory": the agent was only told to remember it
	// with a scope chosen, only it is read; a prompt that asks for more ("all scopes", or another scope's name) gets more
	all, others := false, []string(nil)
	if s.p.Scope != "" {
		all, others = core.ScopesAsked(text, s.p.Scopes())
	}
	pages := filterPages(core.ListProjectPagesIn(s.p, func(scope string) bool {
		return s.p.Scope == "" || all || scope == s.p.Scope || core.Contains(others, scope)
	}), func(pg *core.Page) bool { return !core.Contains(saved, pg.Repo) })
	recall := buildRecall(pages, s.cfg, core.Clean(text, 600))
	if recall != "" && (all || len(others) > 0) {
		beyond := "every scope"
		if !all {
			beyond = "scope " + strings.Join(others, ", ")
		}
		recall = fmt.Sprintf("Reading beyond the scope %q for this prompt, as asked: %s.\n%s", s.p.Scope, beyond, recall)
	}
	return s.agent.Context(agent.Prompt, recall)
}

// saveTriggers stores the memories a user asked for in their prompt ("remember that …"). A failure never reaches the agent.
func saveTriggers(p *core.Project, cfg core.Config, source, prompt string) (saved []string) {
	if err := core.CatchAll(func() {
		found := triggersIn(prompt)
		if len(found) > cfg.MaxAutoPerPrompt {
			found = found[:cfg.MaxAutoPerPrompt]
		}
		for _, c := range found {
			saved = append(saved, p.Remember(core.RememberInput{Text: c.Text, Kind: c.Kind, Title: c.Title, Tags: []string{"auto", "prompt"}, Extra: [][2]string{{"source", source}}}))
		}
	}); err != nil {
		logError(p.Root, err)
	}
	return saved
}
