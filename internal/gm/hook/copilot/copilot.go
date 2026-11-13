// Package copilot is GitHub Copilot as the hooks see it: its event names, its hook file, the object its hooks print.
package copilot

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// events are the hook events it registers.
var events = []agent.Event{
	{Name: "sessionStart", Kind: agent.Start},
	{Name: "userPromptSubmitted", Kind: agent.Prompt},
	{Name: "postToolUse", Kind: agent.Tool},
	{Name: "agentStop", Kind: agent.Stop},
	{Name: "preCompact", Kind: agent.PreCompact},
	{Name: "sessionEnd", Kind: agent.End},
}

// Agent is Copilot. Its hooks live in a file of their own, which is git-excluded. Copilot has no hook for a failed
// tool call, and its prompt hook cannot hand context back, so there is no recall.
var Agent = agent.Agent{
	Name:      "copilot",
	Label:     "Copilot",
	Events:    events,
	HookFiles: []string{".github/hooks/git-memory.json"},
	Recall:    false,
	Context:   context,
	Command:   command,
	Install:   install,
	// A path-specific instructions file that applies to every file.
	Instructions:       ".github/instructions/git-memory.instructions.md",
	InstructionsHeader: "---\napplyTo: \"**\"\n---",
}

type output struct {
	AdditionalContext string `json:"additionalContext"`
}

// context is what Copilot reads from a hook's stdout.
func context(_ agent.Kind, text string) any {
	if text == "" {
		return nil
	}
	return output{AdditionalContext: text}
}

// command runs the launcher by a project-relative path: Copilot runs hooks with the working directory ".".
func command(launcher, event string) string {
	return "./" + launcher + " agent hook copilot " + event
}

// install writes the hook file: it is ours alone, so it is rewritten whole.
func install(file string, command func(event string) string) {
	hooks := core.Obj{}
	for _, ev := range events {
		hooks[ev.Name] = []any{core.Obj{"type": "command", "command": command(ev.Name), "cwd": ".", "timeoutSec": 30}}
	}
	core.WriteJSON(file, core.Obj{"version": 1, "hooks": hooks})
}
