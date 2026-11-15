// Package cursor is Cursor as the hooks see it: its event names, its hook file, the object its hooks print.
package cursor

import (
	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// toolMatcher limits the tool hooks to the tools that change something (a regex over the tool's name).
const toolMatcher = "Shell|Write|StrReplace|Delete|Edit|EditNotebook|ApplyPatch"

// events are the hook events it registers.
var events = []agent.Event{
	{Name: "sessionStart", Kind: agent.Start},
	{Name: "beforeSubmitPrompt", Kind: agent.Prompt},
	{Name: "postToolUse", Kind: agent.Tool, Matcher: toolMatcher},
	{Name: "postToolUseFailure", Kind: agent.ToolFail, Matcher: toolMatcher},
	{Name: "stop", Kind: agent.Stop},
	{Name: "preCompact", Kind: agent.PreCompact},
	{Name: "sessionEnd", Kind: agent.End},
}

// Agent is Cursor. Its hooks live in the project's .cursor/hooks.json, which is git-excluded. Cursor's prompt hook cannot
// hand context back (it can only allow or block the prompt), so there is no recall; only the session start can brief it.
var Agent = agent.Agent{
	Name:      "cursor",
	Label:     "Cursor",
	Events:    events,
	HookFiles: []string{".cursor/hooks.json"},
	Recall:    false,
	Context:   context,
	Command:   command,
	Install:   install,
	Note:      "Cursor runs a project's hooks only in a trusted workspace; reload the window if the hooks do not show up.",
	// An always-applied project rule.
	Instructions:       ".cursor/rules/git-memory.mdc",
	InstructionsHeader: "---\ndescription: Where this project's memory lives and how to read it\nalwaysApply: true\n---",
}

// context is what Cursor reads from a session start hook's stdout.
func context(kind agent.Kind, text string) any {
	if text == "" || kind != agent.Start {
		return nil
	}
	return core.Obj{"additional_context": text}
}

// command runs the launcher by a project-relative path: Cursor runs a project's hooks from the project root.
func command(launcher, event string) string {
	return "./" + launcher + " agent hook cursor " + event
}

// install adds our entries to .cursor/hooks.json (flat entries, one list per event), replacing ours from earlier runs and
// keeping the user's.
func install(file string, command func(event string) string) {
	settings := core.ReadJSON(file)
	hooks := core.AsObj(settings["hooks"])
	for _, ev := range events {
		kept := []any{}
		for _, h := range agent.AsSlice(hooks[ev.Name]) {
			if !agent.IsOurs(core.AsObj(h)["command"]) {
				kept = append(kept, h)
			}
		}
		entry := core.Obj{"command": command(ev.Name), "timeout": 30}
		if ev.Matcher != "" {
			entry["matcher"] = ev.Matcher
		}
		hooks[ev.Name] = append(kept, entry)
	}
	settings["hooks"] = hooks
	if _, ok := settings["version"]; !ok {
		settings["version"] = 1
	}
	core.WriteJSON(file, settings)
}
