package command

import (
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// instructionsTemplate is the always-on guidance each agent loads by itself: where the project's memory lives and how to
// read it. It is the same for every agent; only the file it is written to (and its frontmatter) differs.
// § is a backtick, {GM} the CLI.
const instructionsTemplate = `# Project memory (git-memory)

This project keeps its decisions, procedures, facts and past sessions in a memory repository: a separate git repository at §.git-memory/§ in the repository root (never committed to the project itself). Read it before you answer questions about earlier work, conventions or choices, and before you change something a past decision may cover.

## Layout

§§§
.git-memory/{BASE}
  memories/<kind>/<title>.md    this project's memories; kind is decision, procedure, fact or note
  core/<title>.md               pinned notes: they open every briefing
  scopes/<scope>/{memories,core}/   the same, for one part of the project (a module): only the scope you work in is read
  sessions/YYYY/MM/<date>-<agent>-<id>.md   one page per agent session (a local file, kept out of git); the §## Outcome§ section says what it did
  global/                       memories apart from the project's own, read in every scope (same memories/ and core/ layout)
§§§
{ROOTNOTE}
A page is markdown with a frontmatter block (§kind§, §tier§, §tags§, §created§, §updated§) and a §# Title§. A page with §status: superseded§ is a retired decision: skip it unless asked about history.

## Reading it

The CLI is §{GM}§: it is git, run inside the memory repository, so any read-only git command works. Run it from the repository root.

- A briefing is injected when a session starts: pinned notes, the decisions in force, recent notes and sessions, each with its page path. Start there.
- Search: §{GM} grep -n -i -I -e "<keyword>" -- {READ}§. Try synonyms before concluding nothing is stored. Past sessions are not in git, so grep does not search them: §{GM} list sessions§ lists them, and each is a plain file under §.git-memory/§.
- Read a page: §{GM} show HEAD:<path>§, with the path from the briefing or a search hit (e.g. §{BASE}memories/decision/use-pnpm.md§).
- Browse: §{GM} list§ (§{GM} list sessions 2026-11§ narrows the sessions to a period), §{GM} log --oneline -n 10§ for what changed lately, §{GM} doctor --report§ for where everything lives and whether it is healthy.
- Answer from what the pages say and cite each by its path. If nothing relevant is stored, say so; do not guess.

## Writing it

Store a memory only when the user asks (§/memory-remember§, §/memory-decide§) or explicitly agrees: write one page under §{WRITE}memories/<kind>/<title-slug>.md§ and commit just that folder with §{GM} add§ and §{GM} commit§. Never put secrets in a page, and never push, reset or rewrite the memory repository's history unless the user asks. Your commits go to your own branch of the memory repository (named after your git user name), so they never collide with a teammate's: §/memory-sync§ (or §{GM} sync§) merges the team's work into it and shares yours, and it is the only thing that pushes.

` + "{WHERE}"

// instructionsFile is the content of an agent's instructions file. Like the slash commands it is marked as generated, so
// init rewrites it and never overwrites a file of the user's own.
func instructionsFile(a agent.Agent, gm, root, scope string) string {
	text := strings.ReplaceAll(instructionsTemplate, "{WHERE}", whereNote)
	note := ""
	if root != "" {
		note = "\nThe memory root of this repository is §" + root + "§ (§" + core.RootFile + "§ in the project): the memories and global/ are inside it, so every path below starts with it, as git spells paths from the repository's top.\n"
	}
	base := ""
	if root != "" {
		base = root + "/"
	}
	read, write := base+"memories "+base+"core "+base+"scopes "+base+"global", base // write is a folder prefix: memories/<kind>/... go under it
	if scope != "" {
		read, write = base+"memories "+base+"core "+base+"scopes/"+scope+" "+base+"global", base+"scopes/"+scope+"/"
		note += "\nYou work in the scope §" + scope + "§ (chosen on this checkout; §git-memory agent scope <name>§ changes it): its memories, the project's own and the global ones are read, the other scopes are not, and new memories go to §" + write + "memories/<kind>/§. Stay in it: look beyond it only when the user asks in that request (\"all scopes\", or naming another scope), then search §" + base + "scopes§ as a whole, or that scope's folder §" + base + "scopes/<name>§. To write into another scope for one request, run §{GM} agent scope <name>§, do the work, and put it back with §{GM} agent scope " + scope + "§ (§--clear§ when none was chosen): never leave the checkout in a scope it was not in.\n"
	}
	text = strings.ReplaceAll(text, "{ROOTNOTE}", note)
	text = strings.ReplaceAll(text, "{READ}", read)
	text = strings.ReplaceAll(text, "{WRITE}", write)
	text = strings.ReplaceAll(render(text, gm), "{BASE}", base)
	var lines []string
	if a.InstructionsHeader != "" {
		lines = append(lines, a.InstructionsHeader)
	}
	lines = append(lines, generatedMarker, "", text, "")
	return core.JoinLines(lines)
}

// installInstructions writes each wired agent's instructions file. It returns the files written and the ones kept because
// they are the user's own.
func installInstructions(base core.BaseLoc, gm, root, scope string, agents []agent.Agent) (written, skipped []string) {
	for _, a := range agents {
		if a.Instructions == "" {
			continue
		}
		file := filepath.Join(base.Root, filepath.FromSlash(a.Instructions))
		if core.Exists(file) && !isGenerated(file) {
			skipped = append(skipped, a.Instructions)
			continue
		}
		core.MkdirAll(filepath.Dir(file))
		core.WriteFile(file, instructionsFile(a, gm, root, scope))
		core.ExcludeLocally(base, "/"+a.Instructions)
		written = append(written, a.Instructions)
	}
	return written, skipped
}

// withInstructions adds to agents the others that already have an instructions file of ours, so a change of scope reaches
// every agent the project is wired for.
func withInstructions(base core.BaseLoc, agents []agent.Agent) []agent.Agent {
	out := append([]agent.Agent{}, agents...)
	for _, a := range hook.Agents {
		if a.Instructions == "" || wires(agents, a.Name) {
			continue
		}
		if f := filepath.Join(base.Root, filepath.FromSlash(a.Instructions)); core.Exists(f) && isGenerated(f) {
			out = append(out, a)
		}
	}
	return out
}

func wires(agents []agent.Agent, name string) bool {
	for _, a := range agents {
		if a.Name == name {
			return true
		}
	}
	return false
}
