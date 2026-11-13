// Package agent is the contract between the hook engine (package hook) and the agents it serves (packages claude and
// copilot): what an agent calls its hook events, where its hook entries live, what its hooks print to give it context.
// Everything that differs between Claude Code and Copilot is data in one of those two packages; the engine is shared.
package agent

import (
	"regexp"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

// Kind is the agent-independent name of what happened.
type Kind string

const (
	Start      Kind = "start"
	Prompt     Kind = "prompt"
	Tool       Kind = "tool"
	ToolFail   Kind = "toolfail"
	Stop       Kind = "stop"
	PreCompact Kind = "precompact"
	End        Kind = "end"
)

// Event is one hook event as the agent names it in its hook configuration.
type Event struct {
	Name    string // as written in the agent's hook file and passed on the command line (SessionStart, sessionStart, ...)
	Kind    Kind
	Matcher string // which tools the agent should run the hook for ("" = always)
}

// Agent is everything the hooks need to know about one agent.
type Agent struct {
	Name      string   // claude | copilot: the word in `agent hook <name> <event>`, the spool file prefix, the commit message
	Label     string   // for people: Claude Code, Copilot
	Events    []Event  // the events its hook file registers
	HookFiles []string // project-relative files that can hold our entries; the first is the one `agent init` writes
	// Instructions is the project-relative file `agent init` writes with how to read the memories (always-on guidance the
	// agent loads by itself); InstructionsHeader is the frontmatter that makes the agent load it for every file ("" = none).
	Instructions       string
	InstructionsHeader string
	// Recall: its prompt hook may hand related memories back to the agent.
	Recall bool
	// Context is the object a hook prints on stdout to give the agent text (the briefing, a recall); nil when there is none.
	Context func(kind Kind, text string) any
	// Command is the command line of a hook entry. launcher is the project-relative path of the launcher script.
	Command func(launcher, event string) string
	// Install writes our hook entries into file, keeping what else is there. command builds one entry's command line.
	Install func(file string, command func(event string) string)
}

// Kind translates the agent's name of an event (case, `_` and `-` do not matter) into the shared one.
func (a Agent) Kind(raw string) (Kind, bool) {
	key := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(raw))
	for _, e := range a.Events {
		if strings.ToLower(e.Name) == key {
			return e.Kind, true
		}
	}
	return "", false
}

// oursRe recognises our own hook commands. It also matches the Node-era entry points (git-memory.mjs, cli.js), so
// re-running init replaces them.
var oursRe = regexp.MustCompile(`(?:git-memory\.mjs|[\\/]cli\.js|[\\/]gm(?:\.exe)?|\bgit-memory(?:\.exe)?)"? (?:agent )?hook `)

// IsOurs reports whether a hook entry's command is one of ours.
func IsOurs(command any) bool {
	s, ok := command.(string)
	return ok && oursRe.MatchString(s)
}

// AsSlice reads a JSON array, or nothing.
func AsSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// GroupIsOurs reports whether a Claude-style hook group ({"hooks": [{"command": ...}]}) runs one of our commands.
func GroupIsOurs(g any) bool {
	for _, h := range AsSlice(core.AsObj(g)["hooks"]) {
		if IsOurs(core.AsObj(h)["command"]) {
			return true
		}
	}
	return false
}

// HookCommands lists our hook commands in a Claude settings file or a Copilot hook file.
func HookCommands(file string) []string {
	var out []string
	for _, entries := range core.AsObj(core.ReadJSON(file)["hooks"]) {
		for _, g := range AsSlice(entries) {
			candidates := AsSlice(core.AsObj(g)["hooks"])
			if candidates == nil {
				candidates = []any{g}
			}
			for _, h := range candidates {
				if c := core.AsObj(h)["command"]; IsOurs(c) {
					out = append(out, c.(string))
				}
			}
		}
	}
	return out
}
