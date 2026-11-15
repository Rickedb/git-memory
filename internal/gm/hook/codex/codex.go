// Package codex is OpenAI Codex CLI as the hooks see it: its event names, its hook file, the object its hooks print.
package codex

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// toolMatcher limits the tool hooks to the tools that change something (a regex over the tool's name).
const toolMatcher = "Bash|apply_patch|Edit|Write"

// events are the hook events it registers.
var events = []agent.Event{
	{Name: "SessionStart", Kind: agent.Start},
	{Name: "UserPromptSubmit", Kind: agent.Prompt},
	{Name: "PostToolUse", Kind: agent.Tool, Matcher: toolMatcher},
	{Name: "Stop", Kind: agent.Stop},
	{Name: "PreCompact", Kind: agent.PreCompact},
	{Name: "SessionEnd", Kind: agent.End},
}

// Agent is Codex. Its hooks live in the project's .codex/hooks.json, which is git-excluded. Codex runs a project's hooks
// only when the project's .codex layer is trusted, and only after the user has reviewed each hook (/hooks).
var Agent = agent.Agent{
	Name:      "codex",
	Label:     "Codex",
	Events:    events,
	HookFiles: []string{".codex/hooks.json"},
	Recall:    true,
	Context:   agent.SpecificContext,
	Command:   command,
	Install:   install,
	Note:      "Codex runs a hook only after you trust it: open /hooks in Codex and review git-memory's hooks (the project's .codex folder must be trusted too).",
	// No instructions file: Codex's AGENTS.md is the project's own and tracked. The session briefing says how to read the memories.
}

// command runs the launcher from the repository root: Codex runs hooks in the session's directory, which may be a subfolder.
func command(launcher, event string) string {
	return `"$(git rev-parse --show-toplevel)/` + launcher + `" agent hook codex ` + event
}

// install adds one group per event to .codex/hooks.json, replacing ours from earlier runs and keeping the user's.
func install(file string, command func(event string) string) {
	agent.InstallGroups(file, events, command, func(c string) core.Obj {
		return core.Obj{"type": "command", "command": c, "timeout": 30}
	})
}
