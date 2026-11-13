package hook

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// launcherCLI is how the project's own files (briefing, slash commands) name the CLI: the launcher `agent init` writes.
func launcherCLI() string { return "./.git/git-memory/" + core.WrapperName() }

func currentGit(root string) (branch, head string) {
	branch, _ = core.GitTry(root, "rev-parse", "--abbrev-ref", "HEAD")
	head, _ = core.GitTry(root, "rev-parse", "--short", "HEAD")
	return
}

// start opens the session in the spool, finalizes the pages of sessions that ended without a goodbye, and briefs the
// agent from what the project remembers.
func (s *session) start() any {
	branch, head := currentGit(s.p.Root)
	s.record(core.Event{Ev: "start", Source: pickStr(s.payload, "source"), Branch: branch, Head: head})
	if initial := core.StripHarnessContext(pickStr(s.payload, "initial_prompt", "initialPrompt")); initial != "" {
		s.record(core.Event{Ev: "prompt", Text: core.Clean(initial, s.cfg.Capture.MaxPromptChars)})
	}
	core.RequireMemoryRepo(s.p.Mem)
	core.WithLock(s.p.Mem, func() {
		if flushStale(s.p, s.file) > 0 {
			core.CommitPaths(s.p.Mem, "memory: finalize earlier sessions", s.p.DirRel())
		}
	})
	purgeOld(s.p.Spool, 14)
	return s.agent.Context(agent.Start, buildBriefing(core.ListProjectPages(s.p), s.cfg, s.p.DirRel(), launcherCLI()))
}
