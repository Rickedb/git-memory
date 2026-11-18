package command

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
	"github.com/Rickedb/git-memory/skills"
)

// generatedMarker is the first line of every generated file; init only overwrites files that carry it, never a user's own.
const generatedMarker = "<!-- git-memory:generated (init rewrites this file; copy it under another name to customize) -->"

type commandDef struct {
	Name        string
	Description string
	Hint        string
	// Writes memories: only the user may invoke it (never the model on its own initiative).
	Writes bool
	body   string
}

// Templates use {GM} for the CLI and {AGENTS} for what agent init takes; § is a backtick, for the templates that live in Go
// raw strings (the skills folder uses real backticks).
func render(tpl, gm string) string {
	return strings.NewReplacer("§", "`", "{GM}", gm, "{AGENTS}", agentChoices()).Replace(tpl)
}

// agentChoices is what `agent init` takes, for the text it is named in: <claude|copilot|...|all>.
func agentChoices() string {
	var names []string
	for _, a := range hook.Agents {
		names = append(names, a.Name)
	}
	return "<" + strings.Join(append(names, allAgents), "|") + ">"
}

// whereNote is added to every command (skills/_where.md): the path is relative, so the agent has to know what it is
// relative to, and what to do when the CLI is not there (a fresh clone, a new machine): offer the install instead of
// stopping or improvising.
var whereNote = sharedNote("_where.md")

// scopeNote (skills/_scope.md) is added to the commands that write into a scope. A checkout works in one scope, or none;
// when the user asks for another one the agent goes there for that request and puts the checkout back as it found it.
var scopeNote = sharedNote("_scope.md")

// commandOrder is the order the commands are listed and written in. Each is a file of the skills folder.
var commandOrder = []string{
	"memory-remember", "memory-decide", "memory-recall", "memory-status",
	"memory-list", "memory-promote", "memory-refine", "memory-forget", "memory-sync",
}

// commands are the slash commands worth having in an agent chat: each one adds something the typed prompt triggers
// cannot (a model that reads the conversation, a search-and-answer loop, curation with confirmation). Their text lives
// in the skills folder at the top of the repository. Every name starts with `memory-`, so it is clear in an agent's slash
// menu what they are about, and they group together. (Not `memory` alone: Claude Code has a built-in /memory that a
// same-named skill would replace.)
var commands = loadCommands()

func sharedNote(file string) string {
	b, err := skills.FS.ReadFile(file)
	if err != nil {
		panic("git-memory: " + err.Error())
	}
	return strings.TrimSpace(string(b))
}

// loadCommands reads the command files. A file that is missing, malformed or not listed in commandOrder is a bug of the
// build, so it panics at start (the tests would catch it first).
func loadCommands() []commandDef {
	files, err := fs.Glob(skills.FS, "*.md")
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		if name := strings.TrimSuffix(f, ".md"); !strings.HasPrefix(name, "_") && !core.Contains(commandOrder, name) {
			panic("git-memory: skills/" + f + " is not in commandOrder")
		}
	}
	var out []commandDef
	for _, name := range commandOrder {
		text, err := skills.FS.ReadFile(name + ".md")
		if err != nil {
			panic("git-memory: " + err.Error())
		}
		head, body, ok := strings.Cut(strings.TrimPrefix(string(text), "---\n"), "\n---\n")
		if !ok {
			panic("git-memory: skills/" + name + ".md has no frontmatter")
		}
		def := commandDef{body: strings.TrimSpace(body)}
		scoped := false
		for _, line := range strings.Split(head, "\n") {
			key, val, _ := strings.Cut(line, ": ")
			switch key {
			case "name":
				def.Name = val
			case "description":
				def.Description = val
			case "hint":
				def.Hint = val
			case "writes":
				def.Writes = val == "true"
			case "scope":
				scoped = val == "true"
			}
		}
		if def.Name != name || def.Description == "" {
			panic("git-memory: skills/" + name + ".md needs name: " + name + " and a description")
		}
		if scoped {
			def.body += "\n\n" + scopeNote
		}
		out = append(out, def)
	}
	return out
}

// claudeSkill is the Claude Code skill (`/name`); user-only for commands that write, with the CLI pre-approved for the turn.
func claudeSkill(def commandDef, gm, allowedTool string) string {
	lines := []string{"---", "name: " + def.Name, "description: " + def.Description}
	if def.Hint != "" {
		lines = append(lines, "argument-hint: "+def.Hint)
	}
	if allowedTool != "" {
		lines = append(lines, "allowed-tools: "+allowedTool)
	}
	if def.Writes {
		lines = append(lines, "disable-model-invocation: true")
	}
	lines = append(lines, "---", generatedMarker, "")
	lines = append(lines, render(def.body, gm), "", render(whereNote, gm), "")
	return core.JoinLines(lines)
}

// copilotPrompt is the GitHub Copilot prompt file (`/name` in a Local chat session).
func copilotPrompt(def commandDef, gm string) string {
	lines := []string{"---", "description: " + def.Description}
	if def.Hint != "" {
		lines = append(lines, "argument-hint: "+def.Hint)
	}
	lines = append(lines, "agent: agent", "---", generatedMarker, "", render(def.body, gm), "", render(whereNote, gm), "")
	return core.JoinLines(lines)
}

// codexSkill is the Codex skill (`$name`, or picked from /skills): Codex reads a name and a description.
func codexSkill(def commandDef, gm string) string {
	lines := []string{"---", "name: " + def.Name, "description: " + def.Description, "---", generatedMarker, "", render(def.body, gm), "", render(whereNote, gm), ""}
	return core.JoinLines(lines)
}

// cursorCommand is the Cursor command (`/name`): plain markdown, no frontmatter.
func cursorCommand(def commandDef, gm string) string {
	lines := []string{generatedMarker, "", render(def.body, gm), "", render(whereNote, gm), ""}
	return core.JoinLines(lines)
}

// geminiCommand is the Gemini CLI command (`/name`): a TOML file with a description and the prompt. What the user types
// after the command is added to the prompt by Gemini CLI itself. The prompt is a literal multi-line string, which cannot
// hold three single quotes in a row.
func geminiCommand(def commandDef, gm string) string {
	const fence = "'''"
	prompt := strings.ReplaceAll(core.JoinLines([]string{render(def.body, gm), "", render(whereNote, gm)}), fence, "' ' '")
	return "# git-memory:generated (init rewrites this file; copy it under another name to customize)\n" +
		"description = " + core.MarshalJSON(def.Description, false) + "\n" +
		"prompt = " + fence + "\n" + prompt + "\n" + fence + "\n"
}

// commandFormat is where an agent keeps its slash commands and how a command is written for it. Agents with no entry
// here have no commands.
type commandFormat struct {
	path    func(name string) string // project-relative file
	exclude func(name string) string // what to add to the git exclude list
	render  func(def commandDef, gm, allowedTool string) string
}

var commandFormats = map[string]commandFormat{
	"claude": {
		path:    func(n string) string { return ".claude/skills/" + n + "/SKILL.md" },
		exclude: func(n string) string { return "/.claude/skills/" + n + "/" },
		render:  func(d commandDef, gm, allowed string) string { return claudeSkill(d, gm, allowed) },
	},
	"copilot": {
		path:    func(n string) string { return ".github/prompts/" + n + ".prompt.md" },
		exclude: func(n string) string { return "/.github/prompts/" + n + ".prompt.md" },
		render:  func(d commandDef, gm, _ string) string { return copilotPrompt(d, gm) },
	},
	"codex": {
		path:    func(n string) string { return ".agents/skills/" + n + "/SKILL.md" },
		exclude: func(n string) string { return "/.agents/skills/" + n + "/" },
		render:  func(d commandDef, gm, _ string) string { return codexSkill(d, gm) },
	},
	"cursor": {
		path:    func(n string) string { return ".cursor/commands/" + n + ".md" },
		exclude: func(n string) string { return "/.cursor/commands/" + n + ".md" },
		render:  func(d commandDef, gm, _ string) string { return cursorCommand(d, gm) },
	},
	"gemini": {
		path:    func(n string) string { return ".gemini/commands/" + n + ".toml" },
		exclude: func(n string) string { return "/.gemini/commands/" + n + ".toml" },
		render:  func(d commandDef, gm, _ string) string { return geminiCommand(d, gm) },
	},
}

func isGenerated(file string) bool {
	text, ok := core.ReadFileTry(file)
	return ok && strings.Contains(text, "git-memory:generated")
}

type commandsResult struct {
	Written []string
	// Names are the commands written (once each) and For the agents they were written for.
	Names []string
	For   []string
	// Skipped are files that already exist and are not ours; they are left untouched.
	Skipped []string
}

// installCommands installs the commands of each agent that has them (see commandFormats). Like the hooks they are
// git-excluded, so the project stays clean. A same-named file of the user's own is never touched.
func installCommands(base core.BaseLoc, gm, allowedTool string, agents []agent.Agent) commandsResult {
	var result commandsResult
	for _, def := range commands {
		for _, a := range agents {
			f, ok := commandFormats[a.Name]
			if !ok {
				continue
			}
			rel := f.path(def.Name)
			file := filepath.Join(base.Root, filepath.FromSlash(rel))
			if core.Exists(file) && !isGenerated(file) {
				result.Skipped = append(result.Skipped, rel)
				continue
			}
			core.MkdirAll(filepath.Dir(file))
			core.WriteFile(file, f.render(def, gm, allowedTool))
			core.ExcludeLocally(base, f.exclude(def.Name))
			result.Written = append(result.Written, rel)
			if !core.Contains(result.Names, def.Name) {
				result.Names = append(result.Names, def.Name)
			}
			if !core.Contains(result.For, a.Label) {
				result.For = append(result.For, a.Label)
			}
		}
	}
	return result
}
