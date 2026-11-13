// Package claude is Claude Code as the hooks see it: its event names, its settings file, the object its hooks print.
package claude

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// toolMatcher limits the tool hooks to the tools that change something.
const toolMatcher = "Bash|Edit|Write|MultiEdit|NotebookEdit"

// events are the hook events it registers.
var events = []agent.Event{
	{Name: "SessionStart", Kind: agent.Start},
	{Name: "UserPromptSubmit", Kind: agent.Prompt},
	{Name: "PostToolUse", Kind: agent.Tool, Matcher: toolMatcher},
	{Name: "PostToolUseFailure", Kind: agent.ToolFail, Matcher: toolMatcher},
	{Name: "Stop", Kind: agent.Stop},
	{Name: "PreCompact", Kind: agent.PreCompact},
	{Name: "SessionEnd", Kind: agent.End},
}

// Agent is Claude Code. Its hooks live in the project's settings (the local one, which is git-excluded and personal).
var Agent = agent.Agent{
	Name:      "claude",
	Label:     "Claude Code",
	Events:    events,
	HookFiles: []string{".claude/settings.local.json", ".claude/settings.json"},
	Recall:    true,
	Context:   context,
	Command:   command,
	Install:   install,
	// A rules file without `paths:` frontmatter is loaded at every session start.
	Instructions: ".claude/rules/git-memory.md",
}

type output struct {
	HookSpecificOutput specific `json:"hookSpecificOutput"`
}

type specific struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// context is what Claude Code reads from a hook's stdout: the text, tagged with the event it answers.
func context(kind agent.Kind, text string) any {
	if text == "" {
		return nil
	}
	name := "UserPromptSubmit"
	if kind == agent.Start {
		name = "SessionStart"
	}
	return output{HookSpecificOutput: specific{HookEventName: name, AdditionalContext: text}}
}

// command runs the launcher through $CLAUDE_PROJECT_DIR, Claude's documented way, robust to a changed working directory.
func command(launcher, event string) string {
	return `"$CLAUDE_PROJECT_DIR/` + launcher + `" agent hook claude ` + event
}

// install adds one group per event to the settings' hooks, replacing ours from earlier runs and keeping the user's.
func install(file string, command func(event string) string) {
	settings := core.ReadJSON(file)
	hooks := core.AsObj(settings["hooks"])
	for _, ev := range events {
		kept := []any{}
		for _, g := range agent.AsSlice(hooks[ev.Name]) {
			if !agent.GroupIsOurs(g) {
				kept = append(kept, g)
			}
		}
		group := core.Obj{"hooks": []any{core.Obj{"type": "command", "command": command(ev.Name), "timeout": 30}}}
		if ev.Matcher != "" {
			group["matcher"] = ev.Matcher
		}
		hooks[ev.Name] = append(kept, group)
	}
	settings["hooks"] = hooks
	core.WriteJSON(file, settings)
}
