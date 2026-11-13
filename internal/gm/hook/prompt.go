package hook

import (
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
	pages := filterPages(core.ListProjectPages(s.p), func(pg *core.Page) bool { return !core.Contains(saved, pg.Repo) })
	return s.agent.Context(agent.Prompt, buildRecall(pages, s.cfg, core.Clean(text, 600)))
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
