package hook

// tool records a tool call that changed something (an edit, a shell command), reduced to what is worth remembering.
// failed is true for the agent's failure event; the tool's own result can say so too.
func (s *session) tool(failed bool) {
	if e := summarizeTool(s.payload, s.p.Root, s.cfg); e != nil {
		if failed {
			e.Failed = true
		}
		s.record(*e)
	}
}
