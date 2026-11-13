package hook

import "github.com/Rickedb/git-memory/internal/gm/core"

// stop is the agent finishing a turn: it records the agent's last message and refreshes the session's page. The commit
// is debounced, so a long session is not a long list of commits (precompact and end always commit).
func (s *session) stop() {
	last := core.Clean(lastAssistantText(s.payload), s.cfg.Capture.MaxOutcomeChars)
	s.record(core.Event{Ev: "stop", Last: last})
	s.writePage(func() bool {
		return core.LastCommitAgeSec(s.p.Mem, s.p.DirRel()) >= int64(s.cfg.DebounceCommitSec)
	})
}
