package command

import (
	"path/filepath"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

// instructionsTemplate is the always-on guidance each agent loads by itself: where the project's memory lives and how to
// read it. It is the same for every agent; only the file it is written to (and its frontmatter) differs.
// § is a backtick, {GM} the CLI, {SLUG} the project's folder name in the memory repository.
const instructionsTemplate = `# Project memory (git-memory)

This project keeps its decisions, procedures, facts and past sessions in a memory repository: a separate git repository at §.git-memory/§ in the repository root (never committed to the project itself). Read it before you answer questions about earlier work, conventions or choices, and before you change something a past decision may cover.

## Layout

§§§
.git-memory/
  projects/{SLUG}/            this project
    memories/<kind>/<title>.md    kind is decision, procedure, fact or note
    core/<title>.md               pinned notes: they open every briefing
    sessions/YYYY/MM/<date>-<agent>-<id>.md   one page per agent session (a local file, kept out of git); the §## Outcome§ section says what it did
  global/                     what applies to every project (same memories/ and core/ layout)
§§§

A page is markdown with a frontmatter block (§kind§, §tier§, §tags§, §created§, §updated§) and a §# Title§. A page with §status: superseded§ is a retired decision: skip it unless asked about history.

## Reading it

The CLI is §{GM}§: it is git, run inside the memory repository, so any read-only git command works. Run it from the repository root.

- A briefing is injected when a session starts: pinned notes, the decisions in force, recent notes and sessions, each with its page path. Start there.
- Search: §{GM} grep -n -i -I -e "<keyword>" -- projects/{SLUG} global§. Try synonyms before concluding nothing is stored. Past sessions are not in git, so grep does not search them: §{GM} list sessions§ lists them, and each is a plain file under §.git-memory/§.
- Read a page: §{GM} show HEAD:<path>§, with the path from the briefing or a search hit (e.g. §projects/{SLUG}/memories/decision/use-pnpm.md§).
- Browse: §{GM} list§ (§{GM} list sessions 2026-11§ narrows the sessions to a period), §{GM} log --oneline -n 10§ for what changed lately, §{GM} doctor --report§ for where everything lives and whether it is healthy.
- Answer from what the pages say and cite each by its path. If nothing relevant is stored, say so; do not guess.

## Writing it

Store a memory only when the user asks (§/remember§, §/decide§) or explicitly agrees: write one page under §projects/{SLUG}/memories/<kind>/<title-slug>.md§ and commit just that folder with §{GM} add§ and §{GM} commit§. Never put secrets in a page, and never push, reset or rewrite the memory repository's history unless the user asks.

` + "{WHERE}"

// instructionsFile is the content of an agent's instructions file. Like the slash commands it is marked as generated, so
// init rewrites it and never overwrites a file of the user's own.
func instructionsFile(a agent.Agent, gm, slug string) string {
	text := strings.ReplaceAll(instructionsTemplate, "{WHERE}", whereNote)
	text = strings.ReplaceAll(render(text, gm), "{SLUG}", slug)
	var lines []string
	if a.InstructionsHeader != "" {
		lines = append(lines, a.InstructionsHeader)
	}
	lines = append(lines, generatedMarker, "", text, "")
	return core.JoinLines(lines)
}

// installInstructions writes each wired agent's instructions file. It returns the files written and the ones kept because
// they are the user's own.
func installInstructions(base core.BaseLoc, gm, slug string, agents []agent.Agent) (written, skipped []string) {
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
		core.WriteFile(file, instructionsFile(a, gm, slug))
		core.ExcludeLocally(base, "/"+a.Instructions)
		written = append(written, a.Instructions)
	}
	return written, skipped
}
