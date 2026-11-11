package gm

import (
	"path/filepath"
	"strings"
)

// generatedMarker is the first line of every generated file; init only overwrites files that carry it, never a user's own.
const generatedMarker = "<!-- git-memory:generated (init rewrites this file; copy it under another name to customize) -->"

type commandDef struct {
	Name        string
	Description string
	Hint        string
	// Writes memories: only the user may invoke it (never the model on its own initiative).
	Writes bool
	// Inject: Claude Code runs these shell lines before the model sees the command (arg-less, read-only commands only).
	Inject []struct{ Title, Cmd string }
	body   string
}

// Templates are written with § for a backtick (Go raw strings cannot hold one), {GM} for the CLI and {SCOPES} for the
// scopes note.
func render(tpl, gm string) string {
	s := strings.ReplaceAll(tpl, "{SCOPES}", scopesNote)
	s = strings.ReplaceAll(s, "§", "`")
	return strings.ReplaceAll(s, "{GM}", gm)
}

const scopesNote = "Scopes: §personal§ (only me, this project; the default), §global§ (all my projects), §project§ (shared with the team, if the project has a shared store) or a named scope. §scopes§ lists the ones available here."

// whereNote is added to every command: the path is relative, so the agent has to know what it is relative to.
const whereNote = "The CLI is §{GM}§, relative to the repository root: run it from there (in a linked worktree it is under the main repository's §.git§). It is created per machine by §git-memory init§ (VS Code: \"Git Memory: Set Up\"). If that file does not exist, git-memory is not set up on this machine yet: tell the user to run that and stop, instead of trying to work around it."

// commands are the slash commands worth having in an agent chat: each one adds something the typed prompt triggers
// cannot (a model that reads the conversation, a search-and-answer loop, curation with confirmation).
// `memories`, not `memory`: Claude Code has a built-in /memory that a same-named skill would replace.
var commands = []commandDef{
	{
		Name:        "remember",
		Description: "Store something in this project's memory: a decision, procedure, fact or note.",
		Hint:        "[what to remember]",
		Writes:      true,
		body: `Store what the user wants remembered: the text they typed after the command, or what they point at ("the thing we just discussed").

1. Pick the kind: §decision§ (a choice, with the reason), §procedure§ (how to do something in this project), §fact§ (something true about it) or §note§ (anything else).
2. Write it as one or two self-contained sentences that make sense without this conversation. Never include secrets or credentials. Choose a short title.
3. Pick the scope. {SCOPES} Use §personal§ unless the user said otherwise ("for the team" or "shared" means §project§, "everywhere" means §global§).
4. Run §{GM} remember "<text>" --kind <kind> --title "<title>" --scope <scope>§ and tell the user the reference it printed.`,
	},
	{
		Name:        "decide",
		Description: "Capture the decisions, procedures and gotchas from this conversation into project memory, after the user approves them.",
		Hint:        "[optional focus]",
		Writes:      true,
		body: `Capture what this conversation settled, so future sessions start informed. Focus: the text typed after the command, or the whole conversation if there is none.

1. Review the conversation. Collect only what will still matter later: decisions that were made (what, and why, and what was rejected), project procedures or conventions that were agreed, non-obvious facts and gotchas. Skip task progress, the code that was written, and anything temporary or obvious.
2. Show a numbered list. For each item give the kind (§decision§, §procedure§, §fact§ or §note§), a short title, one or two self-contained sentences, and the scope it would go to (§personal§ by default). {SCOPES} Never include secrets.
3. Ask which items to save and whether any scope should change. Do not store anything before the user answers.
4. For each approved item run §{GM} remember "<text>" --kind <kind> --title "<title>" --scope <scope>§. Report the references. If nothing qualifies, say so instead of inventing items.`,
	},
	{
		Name:        "recall",
		Description: "Search this project's memory (decisions, procedures, facts, past sessions) and answer from it. Use when the user asks what was decided, agreed or done earlier about something.",
		Hint:        "[topic]",
		Writes:      false,
		body: `Look up what the project memory says about the topic the user gave (the text after the command, or the topic they asked about).

1. Run §{GM} search "<the topic as a few keywords>" --limit 8§. Add §--all§ only if the user asks about superseded (retired) decisions.
2. For the most relevant hits, run §{GM} show <reference>§ to read the page.
3. Answer from what the pages say and cite each by its reference, e.g. §personal:memories/decision/use-pnpm.md§. If nothing relevant is stored, say so plainly; do not guess or fill in from general knowledge.`,
	},
	{
		Name:        "memories",
		Description: "Show what project memory holds: scopes, health, and what a new session is told.",
		Writes:      false,
		Inject: []struct{ Title, Cmd string }{
			{"Scopes", "scopes"},
			{"Health", "doctor --report"},
			{"What a new session is told", "briefing"},
		},
		body: `Summarize the project's memory for the user from the output above (or, if it is missing, by running §{GM} scopes§, §{GM} doctor§ and §{GM} briefing§):

- where each scope lives and how many pages it holds
- anything unhealthy in the checks (hooks, runtime, stores), with the fix
- in two or three lines, what a new session would be told

Offer §/recall <topic>§ to dig into something, and §/promote§ or §/forget§ to curate.`,
	},
	{
		Name:        "promote",
		Description: "Change who can see a memory: promote it to global, a named scope or the shared project store, or make it personal again.",
		Hint:        "[reference or description] [scope]",
		Writes:      true,
		body: `Change the scope of an existing memory. Arguments: a memory (a reference such as §personal:memories/decision/use-pnpm.md§, or words describing it) and optionally the target scope.

1. If it is not a full reference, find it with §{GM} search "<words>"§. If several match, ask which one.
2. Target scope: the one given, otherwise §global§. {SCOPES} Promoting to §project§ shares it with the team, so confirm first.
3. Run §{GM} promote <reference> --to <scope>§ (or §{GM} move <reference> --to <scope>§ to make it narrower), and report the new reference.`,
	},
	{
		Name:        "forget",
		Description: "Delete a memory, or retire a decision that has changed (superseding it with its replacement).",
		Hint:        "[reference or description]",
		Writes:      true,
		body: `Remove or retire a memory. Arguments: a reference such as §personal:memories/decision/use-pnpm.md§, or words describing it.

1. Find it with §{GM} search "<words>"§ and read it with §{GM} show <reference>§. Confirm with the user that it is the right page.
2. Ask what they want:
   - **supersede**: the decision changed. Run §{GM} supersede <reference> --by <replacement reference> --reason "<why>"§ (omit §--by§ for no replacement). It stays in git and shows with §--all§, but leaves briefings, recall and search. Recommend this for anything that was once true.
   - **forget**: it was a mistake. Run §{GM} forget <reference>§. The page is deleted (git history keeps it).
3. Do exactly that, then report the result.`,
	},
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
	for _, i := range def.Inject {
		lines = append(lines, "## "+i.Title, "!`"+gm+" "+i.Cmd+"`", "")
	}
	lines = append(lines, render(def.body, gm), "", render(whereNote, gm), "")
	return joinLines(lines)
}

// copilotPrompt is the GitHub Copilot prompt file (`/name` in a Local chat session).
func copilotPrompt(def commandDef, gm string) string {
	lines := []string{"---", "description: " + def.Description}
	if def.Hint != "" {
		lines = append(lines, "argument-hint: "+def.Hint)
	}
	lines = append(lines, "agent: agent", "---", generatedMarker, "", render(def.body, gm), "", render(whereNote, gm), "")
	return joinLines(lines)
}

func isGenerated(file string) bool {
	text, ok := readFileTry(file)
	return ok && strings.Contains(text, "git-memory:generated")
}

type commandsResult struct {
	Written []string
	// Skipped are files that already exist and are not ours; they are left untouched.
	Skipped []string
}

// installCommands installs the commands for Claude Code (.claude/skills/<name>/SKILL.md) and Copilot
// (.github/prompts/<name>.prompt.md). Like the hooks they are git-excluded, so the project stays clean. A same-named
// file of the user's own is never touched.
func installCommands(base BaseLoc, gm, allowedTool string, claude, copilot bool) commandsResult {
	var result commandsResult
	put := func(rel, content, excludeEntry string) {
		file := filepath.Join(base.Root, filepath.FromSlash(rel))
		if exists(file) && !isGenerated(file) {
			result.Skipped = append(result.Skipped, rel)
			return
		}
		mkdirAll(filepath.Dir(file))
		writeFile(file, content)
		excludeLocally(base, excludeEntry)
		result.Written = append(result.Written, rel)
	}
	for _, def := range commands {
		if claude {
			put(".claude/skills/"+def.Name+"/SKILL.md", claudeSkill(def, gm, allowedTool), "/.claude/skills/"+def.Name+"/")
		}
		if copilot {
			put(".github/prompts/"+def.Name+".prompt.md", copilotPrompt(def, gm), "/.github/prompts/"+def.Name+".prompt.md")
		}
	}
	return result
}

// countCommands is how many generated commands are present, for `doctor`.
func countCommands(root string) (claude, copilot int) {
	for _, def := range commands {
		if isGenerated(filepath.Join(root, ".claude", "skills", def.Name, "SKILL.md")) {
			claude++
		}
		if isGenerated(filepath.Join(root, ".github", "prompts", def.Name+".prompt.md")) {
			copilot++
		}
	}
	return
}
