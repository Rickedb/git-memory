package hook

import "github.com/Rickedb/git-memory/internal/gm/core"

// end is the session closing: its page is written and committed.
func (s *session) end() {
	s.record(core.Event{Ev: "end", Reason: s.reason()})
	s.writePage(func() bool { return true })
}
