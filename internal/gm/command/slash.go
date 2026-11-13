package command

import (
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
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

// Templates are written with § for a backtick (Go raw strings cannot hold one) and {GM} for the CLI.
func render(tpl, gm string) string {
	return strings.ReplaceAll(strings.ReplaceAll(tpl, "§", "`"), "{GM}", gm)
}

// whereNote is added to every command: the path is relative, so the agent has to know what it is relative to, and what to
// do when the CLI is not there (a fresh clone, a new machine): offer the install instead of stopping or improvising.
const whereNote = "The CLI is §{GM}§, relative to the repository root: run it from there (in a linked worktree it is under the main repository's §.git§). It is git, run inside the user's memory repository (§{GM} doctor§ says where it is), so any git command works; use only add, commit, mv, rm and read-only ones, and never push, reset or rewrite history unless the user asks. It is created per machine by §git-memory agent init§.\n\n" +
	"**If §{GM}§ does not exist** (\"No such file or directory\", \"command not found\"), git-memory is not set up on this machine yet. Do not work around it and do not touch the memory files by hand. Check whether the binary is installed (§command -v git-memory§, or §Get-Command git-memory§ on Windows), then tell the user what is missing and how to fix it, and ask whether you should do it for them:\n" +
	"- binary installed: §git-memory agent init§, run from the repository root, sets this project up (hooks, commands and the CLI launcher).\n" +
	"- binary not installed: install it first, then run §git-memory agent init§. On Linux: §curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh§. On Windows (PowerShell): §irm https://github.com/Rickedb/git-memory/releases/latest/download/install.ps1 | iex§. It installs into the user's home directory, needs no root, and may need a new shell before §git-memory§ is on the PATH.\n\n" +
	"Run those commands only after the user says yes; if they decline, stop. Once §{GM} doctor§ works, carry on with what the user asked. If the CLI exists but §{GM} log§ says the repository \"does not have any commits yet\", that is not an error: the memory is just empty."

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
3. Run §{GM} doctor --report§. Its §Memory repository:§ line says where the memory repository is, §Project folder:§ is this project's folder inside it and §Global folder:§ is the one for every project. Use the project folder unless the user said it applies everywhere.
4. Write the page to §<repo>/<folder>/memories/<kind>/<title-slug>.md§ (lowercase words joined by dashes), exactly in this shape:

   ---
   kind: <kind>
   tier: semantic
   tags: []
   created: <current UTC time, ISO 8601>
   ---

   # <title>

   <text>

   Use §tier: core§ only if the user asked to pin it (pinned pages open every briefing).
5. Commit just that page: §{GM} add -A -- <folder>/memories§ then §{GM} commit -m "memory: <title>" -- <folder>/memories§. Tell the user the page's path.`,
	},
	{
		Name:        "decide",
		Description: "Capture the decisions, procedures and gotchas from this conversation into project memory, after the user approves them.",
		Hint:        "[optional focus]",
		Writes:      true,
		body: `Capture what this conversation settled, so future sessions start informed. Focus: the text typed after the command, or the whole conversation if there is none.

1. Review the conversation. Collect only what will still matter later: decisions that were made (what, and why, and what was rejected), project procedures or conventions that were agreed, non-obvious facts and gotchas. Skip task progress, the code that was written, and anything temporary or obvious.
2. Show a numbered list. For each item give the kind (§decision§, §procedure§, §fact§ or §note§), a short title, one or two self-contained sentences, and whether it applies to this project (the default) or to every project. Never include secrets.
3. Ask which items to save and whether anything should change. Do not store anything before the user answers.
4. Run §{GM} doctor --report§ for the folders (§Memory repository:§, §Project folder:§, §Global folder:§). For each approved item write §<repo>/<folder>/memories/<kind>/<title-slug>.md§ as: a frontmatter block with §kind: <kind>§, §tier: semantic§, §tags: []§ and §created: <current UTC time, ISO 8601>§, then §# <title>§, a blank line and the text. Then commit them: §{GM} add -A -- <folder>/memories§ and §{GM} commit -m "memory: decisions from this session" -- <folder>/memories§. Report the paths. If nothing qualifies, say so instead of inventing items.`,
	},
	{
		Name:        "recall",
		Description: "Search this project's memory (decisions, procedures, facts, past sessions) and answer from it. Use when the user asks what was decided, agreed or done earlier about something.",
		Hint:        "[topic]",
		Writes:      false,
		body: `Look up what the project memory says about the topic the user gave (the text after the command, or the topic they asked about).

1. Run §{GM} doctor --report§ for the project and global folders (§Project folder:§, §Global folder:§). Then run §{GM} grep -n -i -I -e "<keyword>" -- <project folder> <global folder>§ for a few keywords of the topic, trying synonyms if nothing matches. Pages with §status: superseded§ are retired decisions: skip them unless the user asks about history.
2. For the most relevant hits, read the page with §{GM} show HEAD:<path>§.
3. Answer from what the pages say and cite each by its path, e.g. §projects/shop/memories/decision/use-pnpm.md§. If nothing relevant is stored, say so plainly; do not guess or fill in from general knowledge.`,
	},
	{
		Name:        "memories",
		Description: "Show what project memory holds: where it lives, how healthy it is, and what changed recently.",
		Writes:      false,
		body: `Summarize the project's memory for the user. Run §{GM} doctor --report§, §{GM} log --oneline -n 10§ and §{GM} status -sb§ (do not chain them; a repository with no commits yet makes §log§ fail, which just means nothing was committed), then report:

- the version and anything unhealthy in the checks, with the fix; where the memory repository is, and how many pages the project and global folders hold
- what was committed recently
- anything uncommitted or off (the user may be mid-edit in the repository)

Offer §/recall <topic>§ to dig into something, and §/promote§ or §/forget§ to curate.`,
	},
	{
		Name:        "list",
		Description: "List the memories of a scope (claude, copilot, personal, project, or a folder of the memory repository), or the project's sessions, and say what each one is for.",
		Hint:        "[claude|copilot|personal|project|sessions|<scope>] [date]",
		Writes:      false,
		body: `List the memories of one scope and summarize what each is for. The scope is the text typed after the command; with none, cover every scope.

1. Map the scope to a listing:
   - §claude§: §{GM} list claude§ (Claude Code's own memory for this project).
   - §copilot§: §{GM} list copilot§ (this workspace's and the user's).
   - §personal§ (or §global§): the memories that apply to every project: run §{GM} list repo§ and keep the group headed §.git-memory · global§.
   - §project§ (or §repo§): the same output, the group headed §.git-memory · this project§.
   - §sessions§: this project's session pages, the record of each agent session (not memories), newest first: §{GM} list sessions§. A date narrows it to a period: §{GM} list sessions 2026-11-13§ (a day), §2026-11§ (a month) or §2026§ (a year). If the user words it differently (today, yesterday, last month, 12 November), convert it to one of those forms; a date on its own means §sessions§.
   - anything else is a scope the user defined, a folder of the memory repository: run §{GM} doctor --report§ for the §Memory repository:§ path, then §{GM} ls-tree -r --name-only HEAD -- <scope> projects/<scope>§ (and §{GM} status -s -- <scope> projects/<scope>§ for pages not committed yet); only the pages under §memories/§ and §core/§ count. If nothing matches, say the scope does not exist and name the ones that do: §sessions§, the groups §{GM} list§ prints, plus the folders under §projects/§ and at the top of the repository.
   - no scope: run §{GM} list§ and handle each group in turn.
2. Every row is §kind  title  name  — note§. The note is the page's own description, when it has one. If it already says what the memory is for, use it. Otherwise read the page (§{GM} show HEAD:<path>§ for a committed page of the memory repository; the file itself for an uncommitted one, which the session in progress always is, under the §Memory repository:§ path from §{GM} doctor --report§; and for an agent's memory the file itself too: the group heading shows its folder, the name is a path inside it). A session's note is only its date and agent, so read the page and use its §## Outcome§ section for what the session did and write one line of your own. Read only what you need: with more than about 15 memories, read the ones whose title does not explain itself.
3. Present each group as a list: the title, its kind, and what it is for in one short sentence, i.e. when it would matter to someone working on the project (for a session: its date, and what was done or decided in it). Mark pinned and superseded ones. Never invent: if a page is empty or unclear, say so. Do not change anything.
4. Finish with the count, and offer §/recall <topic>§ to read one in depth and §/promote§ (e.g. §claude:<file>§ into the project) or §/forget§ to curate.`,
	},
	{
		Name:        "promote",
		Description: "Bring a memory into this project's folder or the global one: from the agents' own memories, any markdown file, or between the project and global.",
		Hint:        "[page or description] [project|global]",
		Writes:      true,
		body: `Promote a memory into the memory repository. Arguments: what to promote and where (§project§ is this project's folder, §global§ applies to every project). What can be promoted: a page already in the memory repository (it is moved), one of the agents' own memories (§claude:<file>§, §copilot:<file>§, §copilot-user:<file>§; it is copied, the original stays) or any markdown file (copied).

1. Run §{GM} doctor --report§: it lists the agents' own memories that were found, with their file names, and the project and global folders. If the argument is not an exact reference, find the page (§{GM} grep§ for the memory repository, the doctor listing for the agents' memories). If several match, ask which one.
2. Confirm the target with the user: promoting to §global§ puts it in every project's briefing. If the user gave no target, ask.
3. Run §{GM} promote <source> --to <project|global>§ and report the path it printed. Secrets are redacted on the way in, and promoting the same file twice is refused.`,
	},
	{
		Name:        "forget",
		Description: "Delete a memory, or retire a decision that has changed.",
		Hint:        "[page or description]",
		Writes:      true,
		body: `Remove or retire a memory. Arguments: a page path, or words describing it.

1. Run §{GM} doctor --report§ for the folders, find the page with §{GM} grep§ and read it with §{GM} show HEAD:<path>§. Confirm with the user that it is the right page.
2. Ask what they want:
   - **retire**: the decision changed. Edit the page's frontmatter: add §status: superseded§ (and §superseded_by: <path of the replacement>§ if there is one). It stays in git, but leaves briefings and recall. Recommend this for anything that was once true.
   - **delete**: it was a mistake. Run §{GM} rm <path>§. The history keeps it.
3. Do exactly that, commit with §{GM} commit -m "memory: <what you did>" -- <path>§, then report the result.`,
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

func isGenerated(file string) bool {
	text, ok := core.ReadFileTry(file)
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
func installCommands(base core.BaseLoc, gm, allowedTool string, claude, copilot bool) commandsResult {
	var result commandsResult
	put := func(rel, content, excludeEntry string) {
		file := filepath.Join(base.Root, filepath.FromSlash(rel))
		if core.Exists(file) && !isGenerated(file) {
			result.Skipped = append(result.Skipped, rel)
			return
		}
		core.MkdirAll(filepath.Dir(file))
		core.WriteFile(file, content)
		core.ExcludeLocally(base, excludeEntry)
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
