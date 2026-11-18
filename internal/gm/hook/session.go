package hook

import (
	"os"
	"regexp"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

var unsafeSid = regexp.MustCompile(`[^\w.-]`)

// session is the one agent session an event belongs to, with everything an event's handler works with.
type session struct {
	agent   agent.Agent
	payload core.HookPayload
	p       *core.Project
	cfg     core.Config
	sid     string
	file    string // the session's spool file
}

// handle is the single entry point for every agent/event pair. It returns the object the hook prints on stdout, or nil.
func handle(ag agent.Agent, rawEvent string, payload core.HookPayload) any {
	if rawEvent == "" {
		rawEvent = pickStr(payload, "hook_event_name", "hookEventName")
	}
	kind, ok := ag.Kind(rawEvent)
	if !ok {
		return nil
	}

	cwd := workDir(payload)
	s := &session{agent: ag, payload: payload, p: core.OpenProject(cwd), cfg: core.DefaultConfig(), sid: "unknown"}
	if v := pick(payload, "conversation_id", "session_id", "sessionId"); v != nil { // Cursor names its sessions conversations
		s.sid = unsafeSid.ReplaceAllString(toString(v), "_")
	}
	s.file = spoolFile(s.p.Spool, ag.Name, s.sid)

	switch kind {
	case agent.Start:
		return s.start()
	case agent.Prompt:
		return s.prompt()
	case agent.Tool, agent.ToolFail:
		s.tool(kind == agent.ToolFail)
	case agent.Stop:
		s.stop()
	case agent.PreCompact:
		s.preCompact()
	case agent.End:
		s.end()
	}
	return nil
}

// workDir is the folder the agent works in: the payload's cwd, or its first workspace root (Cursor sends no cwd), or
// where the hook was started.
func workDir(payload core.HookPayload) string {
	if cwd := pickStr(payload, "cwd"); cwd != "" {
		return cwd
	}
	if roots, ok := pick(payload, "workspace_roots", "workspaceRoots").([]any); ok && len(roots) > 0 {
		if r, _ := roots[0].(string); r != "" {
			return r
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// record appends an event to the session's spool.
func (s *session) record(e core.Event) { appendEvent(s.p.Spool, s.agent.Name, s.sid, e) }

// reason is why a session is being compacted or has ended, as the agent says it.
func (s *session) reason() string { return pickStr(s.payload, "reason", "trigger") }

// writePage compiles the spool into the session's page, under the memory repository's lock. commit says whether to
// commit it now.
func (s *session) writePage(commit func() bool) {
	core.RequireMemoryRepo(s.p.Mem)
	core.WithLock(s.p.Mem, func() {
		core.EnsureSessionsIgnored(s.p.Mem)
		writeSessionPage(s.p, s.file)
		if commit() {
			if paths := s.p.OwnPaths(); len(paths) > 0 {
				core.CommitPaths(s.p.Mem, "memory: session "+core.RuneSlice(s.sid, 8)+" ("+s.agent.Name+")", paths...)
			}
		}
		s.p.AutoCommit() // autoCommit: anything else that changed under the memory (global, hand edits, other pages)
	})
}
