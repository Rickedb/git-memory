// Package gemini is Gemini CLI as the hooks see it: its event names, its settings file, the object its hooks print.
package gemini

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// toolMatcher limits the tool hooks to the tools that change something (a regex over the tool's name).
const toolMatcher = "run_shell_command|write_file|replace"

// events are the hook events it registers.
var events = []agent.Event{
	{Name: "SessionStart", Kind: agent.Start},
	{Name: "BeforeAgent", Kind: agent.Prompt},
	{Name: "AfterTool", Kind: agent.Tool, Matcher: toolMatcher},
	{Name: "AfterAgent", Kind: agent.Stop},
	{Name: "PreCompress", Kind: agent.PreCompact},
	{Name: "SessionEnd", Kind: agent.End},
}

// Agent is Gemini CLI. Its hooks live in the project's .gemini/settings.json (it has no personal "local" settings file:
// the entries are git-excluded, which only helps while that file is not tracked).
var Agent = agent.Agent{
	Name:      "gemini",
	Label:     "Gemini CLI",
	Events:    events,
	HookFiles: []string{".gemini/settings.json"},
	Recall:    true,
	Context:   context,
	Command:   command,
	Install:   install,
	// No instructions file: GEMINI.md is the project's own and tracked. The session briefing says how to read the memories.
}

// context is what Gemini CLI reads from a hook's stdout: text for the session's start, or for this turn's prompt.
func context(_ agent.Kind, text string) any {
	if text == "" {
		return nil
	}
	return core.Obj{"hookSpecificOutput": core.Obj{"additionalContext": text}}
}

// command runs the launcher through $GEMINI_PROJECT_DIR, which Gemini CLI sets for its hooks.
func command(launcher, event string) string {
	return `"$GEMINI_PROJECT_DIR/` + launcher + `" agent hook gemini ` + event
}

// install adds one group per event to the settings' hooks, replacing ours from earlier runs and keeping the user's.
// Gemini CLI's timeouts are in milliseconds.
func install(file string, command func(event string) string) {
	agent.InstallGroups(file, events, command, func(c string) core.Obj {
		return core.Obj{"type": "command", "name": "git-memory", "command": c, "timeout": 30000}
	})
}
