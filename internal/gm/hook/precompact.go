package hook

import "github.com/Rickedb/git-memory/internal/gm/core"

// preCompact is the agent about to drop its context: what the session has been is written down and committed first.
func (s *session) preCompact() {
	s.record(core.Event{Ev: "precompact", Reason: s.reason()})
	s.writePage(func() bool { return true })
}
