package gm

// The agents beyond Claude Code and Copilot: Codex, Cursor and Gemini CLI. Each is wired by name, writes only its own
// files, keeps what the user already had in those files, and its hooks speak its payloads and print its output.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each agent's files: its hooks, one slash command, and its instructions file when it has one.
var agentFiles = map[string][]string{
	"claude":  {".claude/settings.local.json", ".claude/skills/memory-recall/SKILL.md", ".claude/rules/git-memory.md"},
	"copilot": {".github/hooks/git-memory.json", ".github/prompts/memory-recall.prompt.md", ".github/instructions/git-memory.instructions.md"},
	"codex":   {".codex/hooks.json", ".agents/skills/memory-recall/SKILL.md"},
	"cursor":  {".cursor/hooks.json", ".cursor/commands/memory-recall.md", ".cursor/rules/git-memory.mdc"},
	"gemini":  {".gemini/settings.json", ".gemini/commands/memory-recall.toml"},
}

func TestEveryAgentIsWiredByNameAndOnlyItsOwnFilesAreWritten(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	for name, files := range agentFiles {
		dir := e.newRepoNoMemory("only-" + name)
		out := e.gm(dir, "agent", "init", name)
		for _, f := range files {
			e.ok(e.exists(filepath.Join(dir, f)), name+" did not write "+f)
		}
		for other, otherFiles := range agentFiles {
			if other == name {
				continue
			}
			for _, f := range otherFiles {
				e.ok(!e.exists(filepath.Join(dir, f)), "agent init "+name+" wrote "+other+"'s "+f)
			}
		}
		e.eq(e.status(dir), "", name+" left something for git status")
		e.has(out, "wrote "+files[0])
	}

	// all of them at once, and again: nothing is added twice
	dir := e.newRepoNoMemory("all-agents")
	e.gm(dir, "agent", "init", "all")
	e.gm(dir, "agent", "init", "all")
	for _, files := range agentFiles {
		for _, f := range files {
			e.ok(e.exists(filepath.Join(dir, f)), "all did not write "+f)
		}
	}
	e.eq(e.status(dir), "")
	_, doc := e.doctor(dir)
	e.has(doc, "✓ Codex hooks: 6 events")
	e.has(doc, "✓ Cursor hooks: 7 events")
	e.has(doc, "✓ Gemini CLI hooks: 6 events")
	e.has(doc, "✓ Slash commands: Claude Code 9/9, Copilot 9/9, Codex 9/9, Cursor 9/9, Gemini CLI 9/9")

	// what a person has to do themselves is said once, for the agent that needs it
	only := e.newRepoNoMemory("notes")
	out := e.gm(only, "agent", "init", "codex")
	e.has(out, "note (Codex): Codex runs a hook only after you trust it")
	e.lacks(out, "note (Cursor)")
}

func TestOtherAgentsHookFilesKeepWhatTheUserHadAndAreRewrittenCleanly(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("own-hooks")

	// the user's own entries, in the three formats
	e.write(filepath.Join(dir, ".cursor/hooks.json"), `{"version":1,"hooks":{"stop":[{"command":".cursor/hooks/audit.sh"}],"afterFileEdit":[{"command":"fmt.sh"}]}}`)
	e.write(filepath.Join(dir, ".gemini/settings.json"), `{"theme":"dark","hooks":{"AfterAgent":[{"hooks":[{"type":"command","command":"./mine.sh"}]}]}}`)
	e.write(filepath.Join(dir, ".codex/hooks.json"), `{"description":"mine","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./mine.sh"}]}]}}`)
	e.gm(dir, "agent", "init", "cursor", "gemini", "codex")
	e.gm(dir, "agent", "init", "cursor", "gemini", "codex") // idempotent

	cursor := e.readJSON(filepath.Join(dir, ".cursor/hooks.json"))
	e.eq(at(cursor, "version"), float64(1))
	stops := at(cursor, "hooks", "stop").([]any)
	e.eq(len(stops), 2, "cursor: the user's stop hook and ours, once each")
	e.eq(at(stops, 0, "command"), ".cursor/hooks/audit.sh")
	e.eq(at(stops, 1, "command"), "./.git/git-memory/gm agent hook cursor stop")
	e.eq(at(cursor, "hooks", "afterFileEdit", 0, "command"), "fmt.sh", "cursor: an event we do not use is untouched")
	e.eq(at(cursor, "hooks", "postToolUse", 0, "matcher"), "Shell|Write|StrReplace|Delete|Edit|EditNotebook|ApplyPatch")

	gemini := e.readJSON(filepath.Join(dir, ".gemini/settings.json"))
	e.eq(at(gemini, "theme"), "dark", "gemini: the other settings stay")
	e.eq(len(at(gemini, "hooks", "AfterAgent").([]any)), 2)
	e.eq(at(gemini, "hooks", "AfterAgent", 0, "hooks", 0, "command"), "./mine.sh")
	e.eq(at(gemini, "hooks", "AfterAgent", 1, "hooks", 0, "command"), `"$GEMINI_PROJECT_DIR/.git/git-memory/gm" agent hook gemini AfterAgent`)
	e.eq(at(gemini, "hooks", "AfterAgent", 1, "hooks", 0, "timeout"), float64(30000), "gemini timeouts are milliseconds")
	e.eq(at(gemini, "hooks", "AfterTool", 0, "matcher"), "run_shell_command|write_file|replace")

	codex := e.readJSON(filepath.Join(dir, ".codex/hooks.json"))
	e.eq(at(codex, "description"), "mine")
	e.eq(len(at(codex, "hooks", "Stop").([]any)), 2)
	e.eq(at(codex, "hooks", "Stop", 1, "hooks", 0, "command"), `"$(git rev-parse --show-toplevel)/.git/git-memory/gm" agent hook codex Stop`)
	e.eq(at(codex, "hooks", "Stop", 1, "hooks", 0, "timeout"), float64(30), "codex timeouts are seconds")
}

func TestSlashCommandsAreWrittenInEachAgentsOwnFormat(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("formats")
	e.gm(dir, "agent", "init", "codex", "cursor", "gemini")

	codex := e.read(filepath.Join(dir, ".agents/skills/memory-remember/SKILL.md"))
	e.ok(strings.HasPrefix(codex, "---\nname: memory-remember\ndescription: "), "a Codex skill starts with its name and description")
	e.lacks(codex, "disable-model-invocation", "that key is Claude Code's")
	e.has(codex, "./.git/git-memory/gm doctor --report")

	cursor := e.read(filepath.Join(dir, ".cursor/commands/memory-remember.md"))
	e.ok(strings.HasPrefix(cursor, "<!-- git-memory:generated"), "a Cursor command is plain markdown, with no frontmatter")
	e.has(cursor, "./.git/git-memory/gm doctor --report")

	gem := e.read(filepath.Join(dir, ".gemini/commands/memory-remember.toml"))
	e.match(gem, `(?m)^description = ".+"$`)
	e.match(gem, `(?m)^prompt = '''$`)
	e.ok(strings.HasSuffix(gem, "'''\n"), "the prompt closes the literal string")
	e.eq(strings.Count(gem, "'''"), 2, "a literal string cannot hold three quotes in a row")
	e.has(gem, "./.git/git-memory/gm doctor --report")
	e.has(gem, "git-memory:generated")

	rule := e.read(filepath.Join(dir, ".cursor/rules/git-memory.mdc"))
	e.ok(strings.HasPrefix(rule, "---\ndescription: "), "the Cursor rule has frontmatter")
	e.has(rule, "alwaysApply: true")
	e.has(rule, "# Project memory (git-memory)")

	// a command of the user's own, with the same name, is kept
	e.write(filepath.Join(dir, ".cursor/commands/memory-recall.md"), "my own recall\n")
	e.write(filepath.Join(dir, ".gemini/commands/memory-forget.toml"), "prompt = 'mine'\n")
	out := e.gm(dir, "agent", "init", "cursor", "gemini")
	e.has(out, "note: kept your own .cursor/commands/memory-recall.md")
	e.has(out, "note: kept your own .gemini/commands/memory-forget.toml")
	e.eq(e.read(filepath.Join(dir, ".cursor/commands/memory-recall.md")), "my own recall\n")
}

func TestCodexSessionIsRecordedAndBriefed(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("codex")
	proj := e.project(dir)
	e.commitPage(dir, proj+"memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

	start := e.hook(dir, "codex", "SessionStart", map[string]any{"session_id": "cx1", "source": "startup", "model": "gpt-5"})
	e.eq(at(start, "hookSpecificOutput", "hookEventName"), "SessionStart")
	e.has(ctxOf(start), "Package manager")

	prompt := e.hook(dir, "codex", "UserPromptSubmit", map[string]any{"session_id": "cx1", "turn_id": "t1", "prompt": "which package manager do we use? pnpm npm"})
	e.eq(at(prompt, "hookSpecificOutput", "hookEventName"), "UserPromptSubmit")
	e.has(ctxOf(prompt), "Package manager", "codex's prompt hook can hand the recall back")

	e.hook(dir, "codex", "PostToolUse", map[string]any{"session_id": "cx1", "tool_name": "Bash", "tool_input": map[string]any{"command": "pnpm lint"}, "tool_response": map[string]any{"is_error": true}})
	e.hook(dir, "codex", "PostToolUse", map[string]any{"session_id": "cx1", "tool_name": "apply_patch",
		"tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: src/app.ts\n@@\n-a\n+b\n*** End Patch"}})
	e.hook(dir, "codex", "Stop", map[string]any{"session_id": "cx1", "last_assistant_message": "Linted and fixed src/app.ts."})
	e.hook(dir, "codex", "SessionEnd", map[string]any{"session_id": "cx1", "reason": "other"})

	_, page := e.sessionPage("codex-cx1")
	e.has(page, "which package manager do we use?")
	e.has(page, "`pnpm lint` (failed)")
	e.has(page, "`src/app.ts`")
	e.has(page, "Linted and fixed src/app.ts.")
}

func TestGeminiSessionIsRecordedAndBriefed(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("gemini")
	proj := e.project(dir)
	e.commitPage(dir, proj+"memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

	start := e.hook(dir, "gemini", "SessionStart", map[string]any{"session_id": "gm1", "source": "startup"})
	e.has(ctxOf(start), "Package manager")
	_, tagged := at(start, "hookSpecificOutput", "hookEventName").(string)
	e.ok(!tagged, "gemini's output carries no event name")

	prompt := e.hook(dir, "gemini", "BeforeAgent", map[string]any{"session_id": "gm1", "prompt": "which package manager do we use? pnpm npm"})
	e.has(ctxOf(prompt), "Package manager")

	e.hook(dir, "gemini", "AfterTool", map[string]any{"session_id": "gm1", "tool_name": "run_shell_command", "tool_input": map[string]any{"command": "pnpm test"},
		"tool_response": map[string]any{"error": "exit 1", "llmContent": "boom"}})
	e.hook(dir, "gemini", "AfterTool", map[string]any{"session_id": "gm1", "tool_name": "write_file", "tool_input": map[string]any{"file_path": "src/new.ts", "content": "x"}})
	e.hook(dir, "gemini", "AfterTool", map[string]any{"session_id": "gm1", "tool_name": "read_file", "tool_input": map[string]any{"file_path": "src/old.ts"}})
	e.hook(dir, "gemini", "AfterAgent", map[string]any{"session_id": "gm1", "prompt": "x", "prompt_response": "Tests fail in src/new.ts.", "stop_hook_active": false})
	e.hook(dir, "gemini", "PreCompress", map[string]any{"session_id": "gm1", "trigger": "auto"})
	e.hook(dir, "gemini", "SessionEnd", map[string]any{"session_id": "gm1", "reason": "exit"})

	_, page := e.sessionPage("gemini-gm1")
	e.has(page, "which package manager do we use?")
	e.has(page, "`pnpm test` (failed)")
	e.has(page, "`src/new.ts`")
	e.lacks(page, "src/old.ts", "a read changes nothing")
	e.has(page, "Tests fail in src/new.ts.")
}

func TestCursorSessionUsesConversationsAndWorkspaceRoots(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("cursor")
	proj := e.project(dir)
	e.commitPage(dir, proj+"memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

	// Cursor sends no cwd: the workspace root says which project it is
	fire := func(event string, payload map[string]any) map[string]any {
		t.Helper()
		payload["workspace_roots"] = []string{dir}
		raw, _ := json.Marshal(payload)
		other := e.mkdir("elsewhere")
		out, errOut, code := e.runIn(other, string(raw), launcher(dir), "agent", "hook", "cursor", event)
		e.eq(code, 0, errOut)
		if strings.TrimSpace(out) == "" {
			return nil
		}
		var m map[string]any
		e.eq(json.Unmarshal([]byte(out), &m), nil)
		return m
	}

	start := fire("sessionStart", map[string]any{"session_id": "s1", "conversation_id": "cv1"})
	e.has(at(start, "additional_context").(string), "Package manager")

	// it cannot hand context back from a prompt, so a prompt prints nothing
	e.eq(fire("beforeSubmitPrompt", map[string]any{"conversation_id": "cv1", "prompt": "which package manager do we use? pnpm npm"}), map[string]any(nil))
	fire("postToolUse", map[string]any{"conversation_id": "cv1", "tool_name": "Shell", "tool_input": map[string]any{"command": "pnpm build"}})
	fire("postToolUseFailure", map[string]any{"conversation_id": "cv1", "tool_name": "Shell", "tool_input": map[string]any{"command": "pnpm test"}, "error_message": "exit 1", "failure_type": "error"})
	fire("postToolUse", map[string]any{"conversation_id": "cv1", "tool_name": "StrReplace", "tool_input": map[string]any{"path": filepath.Join(dir, "src/a.ts")}})
	fire("stop", map[string]any{"conversation_id": "cv1", "status": "completed", "loop_count": 0})
	fire("sessionEnd", map[string]any{"conversation_id": "cv1", "reason": "completed"})

	_, page := e.sessionPage("cursor-cv1")
	e.has(page, "which package manager do we use?")
	e.has(page, "`pnpm build`")
	e.has(page, "`pnpm test` (failed)")
	e.has(page, "`src/a.ts`")

	// the "remember that" trigger still works where no recall can be printed
	fire("beforeSubmitPrompt", map[string]any{"conversation_id": "cv1", "prompt": "remember that releases are tagged with a v prefix"})
	e.has(e.gm(dir, "grep", "-l", "-I", "-e", "tagged with a v prefix", "--", "memories"), "memories/")
}

// ---- the memory root folder ---------------------------------------------------------------------------------------

// A memory repository can keep its memories in a folder of it, named in a file at the top of the project (.git-memory.json,
// committed with the project, not in the memory repository); without the file, the folder is the repository's top.
func TestMemoriesCanLiveInAFolderOfTheMemoryRepository(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("rooted")
	const root = "docs/memory"
	out := e.gm(dir, "agent", "init", "claude", "--root", "./docs/memory/") // spelled loosely: it means docs/memory
	e.has(out, "wrote .claude/settings.local.json")
	e.has(out, "wrote .git-memory.json")

	// the setting is a file of the project, left for the project to commit; the memory repository does not get it
	e.eq(e.readJSON(filepath.Join(dir, ".git-memory.json")), map[string]any{"root": root})
	e.ok(!e.exists(filepath.Join(e.home, ".git-memory.json")), "the memory repository got the root file")
	e.eq(strings.TrimSpace(e.git(dir, "status", "--porcelain", "--", ".git-memory.json")), "?? .git-memory.json", "the project's file is not git-excluded")
	e.git(dir, "add", ".git-memory.json") // the project's owner commits it with the project
	e.git(dir, "commit", "-qm", "memory root")

	rows, doc := e.doctor(dir)
	e.eq(projectPrefix(rows["Project folder"]), root+"/")
	e.has(rows["Global folder"], root+"/global")
	e.has(doc, "Root folder: docs/memory (.git-memory.json)")

	// the instructions name the folder in every path they teach
	rules := e.read(filepath.Join(dir, ".claude/rules/git-memory.md"))
	e.has(rules, ".git-memory/docs/memory/\n  memories/<kind>/")
	e.has(rules, "-- docs/memory/memories docs/memory/core docs/memory/scopes docs/memory/global")
	e.has(rules, "docs/memory/memories/decision/use-pnpm.md")
	e.has(rules, "The memory root of this repository is `docs/memory`")

	// the .gitignore that keeps session pages out of git sits in the root folder, and travels with it
	e.has(e.read(filepath.Join(e.home, root, ".gitignore")), "/sessions/")
	e.eq(strings.TrimSpace(e.gm(dir, "ls-files", "--", root+"/.gitignore")), root+"/.gitignore")

	// a session is written under the root folder and nothing appears at the top
	const sid = "rootsess1"
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": sid})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": sid, "prompt": "remember that the root folder holds the memories"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": sid, "last_assistant_message": "Done."})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": sid})
	sessions, _ := filepath.Glob(filepath.Join(e.home, root, "sessions", "*", "*", "*"+sid[:8]+"*.md"))
	e.eq(len(sessions), 1, "the session page is under the root folder")
	e.ok(!e.exists(filepath.Join(e.home, "memories")) && !e.exists(filepath.Join(e.home, "sessions")) && !e.exists(filepath.Join(e.home, "global")), "something was written at the top of the repository")
	e.ok(!e.exists(filepath.Join(e.home, "projects")), "a projects folder was made")
	e.eq(e.gm(dir, "status", "--porcelain"), "", "session pages are ignored, memories are committed")
	e.has(e.gm(dir, "grep", "-l", "-I", "-e", "root folder holds the memories", "--", root), root+"/memories/")

	// the next session is briefed with paths that git can use as they are
	start := e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "rootsess2"})
	e.has(ctxOf(start), "-- docs/memory/memories docs/memory/core docs/memory/scopes docs/memory/global")
	e.has(ctxOf(start), "(docs/memory/memories/")

	// list, promote and demote work on the pages where they are
	e.has(e.gm(dir, "list", "repo"), root+"/memories/")
	var page string
	for _, f := range strings.Split(strings.TrimSpace(e.gm(dir, "ls-files", "--", root+"/memories")), "\n") {
		page = f
	}
	e.ok(page != "", "no memory was saved")
	e.has(e.gm(dir, "promote", page, "--to", "global"), "→ "+root+"/global/memories/")
	e.eq(e.status(dir), "")
	e.ok(!e.exists(filepath.Join(e.home, filepath.FromSlash(page))), "promote left the page where it was")
	e.has(e.gm(dir, "list", "repo"), root+"/global/memories/")
	e.has(e.gm(dir, "demote", filepath.Base(page)), "→ "+root+"/memories/")

	// changing the root needs no init: the project's file is the setting
	e.write(filepath.Join(dir, ".git-memory.json"), `{"root": "other"}`)
	rows, _ = e.doctor(dir)
	e.eq(projectPrefix(rows["Project folder"]), "other/")
}

func TestTheMemoryRootMustBeAFolderInsideTheRepository(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("badroot")
	rootFile := filepath.Join(dir, ".git-memory.json")

	// asked for on the command line: refused before anything is written
	for _, bad := range []string{"../elsewhere", "/etc", "a/../../b", ".git/x", "C:/memory"} {
		_, errOut, code := e.gmRaw(dir, "agent", "init", "claude", "--root", bad)
		e.eq(code, 1, bad)
		e.has(errOut, `"root" in .git-memory.json must be a folder inside the memory repository`, bad)
		e.ok(!e.exists(filepath.Join(dir, ".claude")) && !e.exists(rootFile), "wrote something although the root is "+bad)
	}
	_, errOut, code := e.gmRaw(dir, "agent", "init", "claude", "--root")
	e.eq(code, 1)
	e.has(errOut, "--root needs a folder")

	// written by hand into the file: doctor says so, and so does anything that would write
	e.write(rootFile, `{"root": "../elsewhere"}`)
	_, doc := e.doctor(dir)
	e.has(doc, `✗ Root folder: "root" in .git-memory.json must be a folder inside the memory repository`)
	_, errOut, code = e.gmRaw(dir, "agent", "init", "claude")
	e.eq(code, 1)
	e.ok(!e.exists(filepath.Join(dir, ".claude")), "wired the project although the root is outside the repository")
	e.write(rootFile, `{"root": `)
	_, doc = e.doctor(dir)
	e.has(doc, "✗ Root folder: .git-memory.json is not valid JSON")

	// the top, however it is spelled, and no file at all
	for _, same := range []string{`{"root": "."}`, `{"root": "./"}`, `{"root": ""}`, `{}`} {
		e.write(rootFile, same)
		rows, _ := e.doctor(dir)
		e.has(rows["Root folder"], "./", same)
		e.eq(projectPrefix(rows["Project folder"]), "", "the default folder is the repository's top")
	}
	e.ok(os.Remove(rootFile) == nil, "removing the file")
	e.gm(dir, "agent", "init", "claude")
	e.ok(e.exists(filepath.Join(e.home, ".gitignore")), "the default keeps the .gitignore at the top")
	e.lacks(e.read(filepath.Join(dir, ".claude/rules/git-memory.md")), "The memory root of this repository")

	// the file is edited by hand and committed, and a root that is not there yet is made with the first memory
	e.write(rootFile, `{"root": "team/ai", "other": 1}`)
	rows, _ := e.doctor(dir)
	e.has(rows["Root folder"], "team/ai (empty or not there yet")
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "later1", "prompt": "decision: use the team folder"})
	m, _ := filepath.Glob(filepath.Join(e.home, "team/ai/memories"))
	e.eq(len(m), 1, "the memory went to the new root")

	// --root keeps what else the file has
	e.gm(dir, "agent", "init", "claude", "--root", "docs/x")
	e.eq(e.readJSON(rootFile), map[string]any{"root": "docs/x", "other": float64(1)})
	e.gm(dir, "agent", "init", "claude", "--root", ".")
	e.eq(at(e.readJSON(rootFile), "root"), ".")
}

// ---- scopes: the part of a project a user works in ----------------------------------------------------------------

const scopeDecision = "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# %s\n\n%s\n"

func scopePage(title, body string) string {
	return strings.Replace(strings.Replace(scopeDecision, "%s", title, 1), "%s", body, 1)
}

// A project's memory can be split into scopes (a module of the app). All of them are in the repository, but a checkout
// reads only the one chosen on it; the choice is local.
func TestOnlyTheChosenScopeIsRead(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("scoped")
	rows, _ := e.doctor(dir)
	proj := projectPrefix(rows["Project folder"])

	e.commitPage(dir, proj+"memories/decision/project-wide.md", scopePage("Project wide rule", "Every module uses pnpm."))
	e.commitPage(dir, proj+"scopes/checkout/memories/decision/stripe.md", scopePage("Checkout uses Stripe", "Payments go through Stripe."))
	e.commitPage(dir, proj+"scopes/search/memories/decision/elastic.md", scopePage("Search uses Elastic", "Indexing goes through Elastic."))
	e.commitPage(dir, "global/memories/decision/everywhere.md", scopePage("Global rule", "Always sign commits."))

	// no scope chosen: everything is read, as before
	_, doc := e.doctor(dir)
	e.has(doc, "· Scope: none chosen: every scope is read (checkout, search;")
	start := ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "sc0"}))
	for _, want := range []string{"Project wide rule", "Checkout uses Stripe", "Search uses Elastic", "Global rule"} {
		e.has(start, want)
	}

	// choose one: it is local, never committed, and wires nothing else
	out := e.gm(dir, "agent", "init", "claude", "--scope", "checkout")
	e.has(out, ".git/git-memory/scope   (the scope this checkout works in; local, never committed)")
	e.eq(strings.TrimSpace(e.read(filepath.Join(dir, ".git", "git-memory", "scope"))), "checkout")
	e.eq(e.status(dir), "")
	e.eq(strings.TrimSpace(e.gm(dir, "status", "--porcelain")), "", "choosing a scope changed the memory repository")

	rows, doc = e.doctor(dir)
	e.has(doc, "✓ Scope: checkout (local to this checkout; the project's other scopes are not read)")
	e.eq(rows["Write folder"], proj+"scopes/checkout (where new memories go)")
	e.eq(rows["Read folders"], proj+"memories "+proj+"core "+proj+"scopes/checkout global")

	start = ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "sc1"}))
	e.has(start, "Project wide rule")
	e.has(start, "Checkout uses Stripe")
	e.has(start, "Global rule")
	e.lacks(start, "Search uses Elastic", "another scope was briefed")
	e.has(start, `You are working in the scope "checkout"`)
	e.has(start, `look beyond it only when the user asks in that request ("all scopes", or naming another scope)`)
	e.has(start, "-- "+proj+"memories "+proj+"core "+proj+"scopes/checkout global")

	// recall does not see the other scope either, and what is saved goes into the chosen one
	recall := ctxOf(e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "sc1", "prompt": "how does indexing work with elastic and search?"}))
	e.lacks(recall, "Search uses Elastic")

	// a prompt can ask for more, for that prompt only: every scope, or another one by name
	ask := func(prompt string) string {
		return ctxOf(e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "sc1", "prompt": prompt}))
	}
	wide := ask("how does indexing work with elastic and search? look across all scopes")
	e.has(wide, "Search uses Elastic")
	e.has(wide, `Reading beyond the scope "checkout" for this prompt, as asked: every scope.`)
	named := ask("how does indexing work with elastic? check the search scope")
	e.has(named, "Search uses Elastic")
	e.has(named, `as asked: scope search.`)
	e.lacks(ask("how does indexing work with elastic and search?"), "Search uses Elastic", "the next prompt is back to the chosen scope")
	e.lacks(ask("how does indexing work with elastic in the nonexistent scope?"), "Search uses Elastic", "a scope that does not exist widens nothing")
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "sc1", "prompt": "remember that refunds are issued from the dashboard"})
	saved, _ := filepath.Glob(filepath.Join(e.home, proj, "scopes", "checkout", "memories", "*", "*refunds*"))
	e.eq(len(saved), 1, "the memory goes to the scope's folder")
	e.eq(e.gm(dir, "status", "--porcelain"), "")

	// list shows the chosen scope only
	list := e.gm(dir, "list", "repo")
	e.has(list, ".git-memory · scope checkout")
	e.has(list, "Checkout uses Stripe")
	e.lacks(list, "Search uses Elastic")
	all := e.gm(dir, "list", "repo", "--all-scopes")
	e.has(all, ".git-memory · scope search")
	e.has(all, "Search uses Elastic")

	// the agents are taught the same folders
	rules := e.read(filepath.Join(dir, ".claude/rules/git-memory.md"))
	e.has(rules, "-- "+proj+"memories "+proj+"core "+proj+"scopes/checkout global")
	e.has(rules, "You work in the scope `checkout`")
	e.has(rules, "look beyond it only when the user asks in that request")
	e.has(rules, "write one page under `"+proj+"scopes/checkout/memories/")

	// a page moves between the scope, the project and global
	page := proj + "scopes/checkout/memories/decision/stripe.md"
	e.has(e.gm(dir, "promote", page, "--to", "project"), "→ "+proj+"memories/decision/stripe.md")
	e.has(e.gm(dir, "promote", proj+"memories/decision/stripe.md", "--to", "scope"), "→ "+page)
	e.has(e.gm(dir, "promote", page, "--to", "global"), "→ global/memories/decision/stripe.md")
	e.has(e.gm(dir, "demote", "stripe.md", "--to", "scope"), "→ "+page)
	e.eq(e.status(dir), "")

	// another scope can be chosen; its instructions follow, and so do other wired agents'
	e.gm(dir, "agent", "init", "copilot")
	e.gm(dir, "agent", "init", "claude", "--scope", "search")
	e.has(e.read(filepath.Join(dir, ".claude/rules/git-memory.md")), proj+"scopes/search")
	e.has(e.read(filepath.Join(dir, ".github/instructions/git-memory.instructions.md")), proj+"scopes/search", "copilot's instructions kept the old scope")
	start = ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "sc2"}))
	e.has(start, "Search uses Elastic")
	e.lacks(start, "Checkout uses Stripe")

	// and none again
	e.gm(dir, "agent", "init", "claude", "--no-scope")
	e.ok(!e.exists(filepath.Join(dir, ".git", "git-memory", "scope")), "the scope file is still there")
	start = ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "sc3"}))
	e.has(start, "Checkout uses Stripe")
	e.has(start, "Search uses Elastic")
	e.lacks(e.read(filepath.Join(dir, ".claude/rules/git-memory.md")), "You work in the scope")
	_, errOut, code := e.gmRaw(dir, "promote", page, "--to", "scope")
	e.eq(code, 1)
	e.has(errOut, "no scope is chosen on this checkout")
}

func TestAScopeIsANameAndIsChosenPerCheckout(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	a := e.newRepoNoMemory("scope-a")
	for _, bad := range []string{"Check Out", "a/b", "../x", "-x", "x_y"} {
		_, errOut, code := e.gmRaw(a, "agent", "init", "claude", "--scope", bad)
		e.eq(code, 1, bad)
		e.has(errOut, "a scope is a name of lowercase letters, digits and dashes", bad)
		e.ok(!e.exists(filepath.Join(a, ".claude")), "wired the project although the scope is "+bad)
	}
	_, errOut, code := e.gmRaw(a, "agent", "init", "claude", "--scope")
	e.eq(code, 1)
	e.has(errOut, "--scope needs a name")
	_, errOut, code = e.gmRaw(a, "agent", "init", "claude", "--scope", "x", "--no-scope")
	e.eq(code, 1)
	e.has(errOut, "choose one")

	// the name is normalised, and a scope that has no folder yet is made with its first memory
	e.gm(a, "agent", "init", "claude", "--scope", "Mobile-App")
	e.home = filepath.Join(a, ".git-memory")
	rows, doc := e.doctor(a)
	e.has(doc, "Scope: mobile-app (local to this checkout; the project's other scopes are not read; its folder is made with its first memory)")
	e.hook(a, "claude", "UserPromptSubmit", map[string]any{"session_id": "n1", "prompt": "decision: the mobile app caches for a day"})
	m, _ := filepath.Glob(filepath.Join(e.home, projectPrefix(rows["Project folder"]), "scopes", "mobile-app", "memories", "*", "*"))
	e.eq(len(m), 1)

	// a second checkout of the same memory repository chooses by itself: the first one's choice is not shared
	clone := e.newRepoNoMemory("scope-b")
	e.gm(clone, "clone", e.home)
	e.gm(clone, "agent", "init", "claude")
	e.ok(!e.exists(filepath.Join(clone, ".git", "git-memory", "scope")), "a scope travelled with the memory repository")
}

// `git-memory agent scope` chooses the scope without wiring anything: it shows, sets and clears it.
func TestAgentScopeChoosesTheScopeOnItsOwn(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("scope-cmd")
	e.gm(dir, "agent", "init", "claude", "copilot")
	e.home = filepath.Join(dir, ".git-memory")
	rows, _ := e.doctor(dir)
	proj := projectPrefix(rows["Project folder"])
	e.commitPage(dir, proj+"scopes/checkout/memories/decision/stripe.md", scopePage("Checkout uses Stripe", "Payments go through Stripe."))
	e.commitPage(dir, proj+"scopes/search/memories/decision/elastic.md", scopePage("Search uses Elastic", "Indexing goes through Elastic."))

	e.has(e.gm(dir, "agent", "scope"), "no scope chosen: every scope is read   (scopes in the project: checkout, search)")

	out := e.gm(dir, "agent", "scope", "Checkout")
	e.has(out, "scope: checkout   (local to this checkout: .git/git-memory/scope)")
	e.has(out, "wrote .claude/rules/git-memory.md")
	e.has(out, "wrote .github/instructions/git-memory.instructions.md")
	e.has(e.gm(dir, "agent", "scope"), "scope: checkout   (scopes in the project: checkout, search)")
	for _, f := range []string{".claude/rules/git-memory.md", ".github/instructions/git-memory.instructions.md"} {
		e.has(e.read(filepath.Join(dir, f)), proj+"scopes/checkout", f)
	}
	start := ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "as1"}))
	e.has(start, "Checkout uses Stripe")
	e.lacks(start, "Search uses Elastic")
	e.eq(e.status(dir), "")

	// it wires nothing that was not wired
	e.ok(!e.exists(filepath.Join(dir, ".cursor")) && !e.exists(filepath.Join(dir, ".codex")), "agent scope wired an agent")

	out = e.gm(dir, "agent", "scope", "--clear")
	e.has(out, "no scope: every scope is read")
	e.has(e.read(filepath.Join(dir, ".claude/rules/git-memory.md")), "-- "+proj+"memories "+proj+"core "+proj+"scopes "+proj+"global")
	e.ok(!e.exists(filepath.Join(dir, ".git", "git-memory", "scope")), "the scope file is still there")

	for _, args := range [][]string{{"agent", "scope", "Not A Scope"}, {"agent", "scope", "a", "b"}, {"agent", "scope", "a", "--clear"}} {
		_, errOut, code := e.gmRaw(dir, args...)
		e.eq(code, 1, strings.Join(args, " "))
		e.ok(errOut != "", "no message for "+strings.Join(args, " "))
	}
}

// `init` is git's: it has no --root. A mistyped one fails in git, and the folder it left behind does not stop `agent init`.
func TestAFailedInitDoesNotBlockAgentInit(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("failed-init")
	_, errOut, code := e.gmRaw(dir, "init", "--root", "docs/memory")
	e.ok(code != 0, "git init accepted --root")
	e.has(errOut, "unknown option")
	out := e.gm(dir, "agent", "init", "claude", "--root", "docs/memory")
	e.has(out, "wrote .git-memory.json")
	e.ok(e.exists(filepath.Join(dir, ".git-memory", ".git")), "the memory repository was not created")
	e.eq(at(e.readJSON(filepath.Join(dir, ".git-memory.json")), "root"), "docs/memory")

	// somebody's own files in that folder are still not touched
	own := e.newRepoNoMemory("own-folder")
	e.write(filepath.Join(own, ".git-memory", "notes.txt"), "mine")
	_, errOut, _ = e.gmRaw(own, "agent", "init", "claude")
	e.has(errOut+e.gm(own, "agent", "init", "claude", "--no-commands"), "is not a git repository")
}

// The memory root folder holds the project's memories directly (memories/, core/, scopes/, sessions/) and global/: there
// is no projects/<name> level, however the root is set.
func TestThereIsNoProjectsFolder(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("flat")
	e.agentInit(dir)
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "flat1"})
	e.hook(dir, "claude", "UserPromptSubmit", map[string]any{"session_id": "flat1", "prompt": "decision: the layout is flat"})
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "flat1"})

	e.ok(!e.exists(filepath.Join(e.home, "projects")), "a projects folder was made")
	m, _ := filepath.Glob(filepath.Join(e.home, "memories", "decision", "*"))
	e.eq(len(m), 1, "the memory is in memories/ at the top")
	s, _ := filepath.Glob(filepath.Join(e.home, "sessions", "*", "*", "*flat1*.md"))
	e.eq(len(s), 1, "the session page is in sessions/ at the top")
	rows, _ := e.doctor(dir)
	e.has(rows["Project folder"], ".")
	e.eq(rows["Read folders"], "memories core scopes global")
	e.eq(e.status(dir), "")
}

// A project must be a git repository (its hooks keep their local state in its .git), but it needs no commit yet.
func TestAgentInitNeedsAGitRepositoryButNoCommit(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.mkdir("gm-not-a-repo")
	e.gm(dir, "init", "-q") // the memory repository exists, the project is not a git repository
	_, errOut, code := e.gmRaw(dir, "agent", "init", "claude", "--root", "test1")
	e.eq(code, 1)
	e.has(errOut, "is not inside a git repository", "Run `git init` there first; no commit is needed")
	e.ok(!e.exists(filepath.Join(dir, ".claude")) && !e.exists(filepath.Join(dir, ".git-memory.json")), "wrote something before failing")

	e.git(dir, "init", "-q", "-b", "main") // a repository with no commit yet is enough
	out := e.gm(dir, "agent", "init", "claude", "--root", "test1")
	e.has(out, "wrote .claude/settings.local.json")
	e.eq(at(e.readJSON(filepath.Join(dir, ".git-memory.json")), "root"), "test1")
}

// Asked to write into a scope the checkout is not in, the agent switches to it for that request and puts the checkout
// back as it was. The commands that write say so; the others do not need it.
func TestWritingCommandsSwitchScopeForOneRequestAndGoBack(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepoNoMemory("scope-back")
	e.gm(dir, "agent", "init", "claude", "--scope", "checkout")
	e.home = filepath.Join(dir, ".git-memory")
	for _, name := range []string{"memory-remember", "memory-decide", "memory-promote"} {
		text := e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md"))
		e.has(text, "**Another scope.**", name)
		e.has(text, "./.git/git-memory/gm agent scope <name>", name)
		e.has(text, "./.git/git-memory/gm agent scope --clear", name)
	}
	for _, name := range []string{"memory-recall", "memory-list", "memory-status", "memory-refine", "memory-forget"} {
		e.lacks(e.read(filepath.Join(dir, ".claude/skills", name, "SKILL.md")), "**Another scope.**", name)
	}
	rules := e.read(filepath.Join(dir, ".claude/rules/git-memory.md"))
	e.has(rules, "./.git/git-memory/gm agent scope <name>`, do the work, and put it back with `./.git/git-memory/gm agent scope checkout`")
	e.commitPage(dir, "memories/decision/rule.md", scopePage("A project rule", "Every module uses pnpm."))
	start := ctxOf(e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "back1"}))
	e.has(start, "put it back with ./.git/git-memory/gm agent scope checkout")

	// what the agent runs: switch, work, switch back (and clear when there was none)
	scope := filepath.Join(dir, ".git", "git-memory", "scope")
	e.gm(dir, "agent", "scope", "search")
	e.eq(strings.TrimSpace(e.read(scope)), "search")
	e.gm(dir, "agent", "scope", "checkout")
	e.eq(strings.TrimSpace(e.read(scope)), "checkout")
	e.gm(dir, "agent", "scope", "--clear")
	e.gm(dir, "agent", "scope", "search")
	e.gm(dir, "agent", "scope", "--clear")
	e.ok(!e.exists(scope), "the scope was not cleared")
}

// "autoCommit": true in the project's .git-memory.json makes the hooks commit every change under the memory root folder,
// not only the pages they write themselves; without it a hand-written page stays uncommitted.
func TestAutoCommitCommitsEveryChangeUnderTheMemory(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newRepo("autocommit")
	e.agentInit(dir)

	// off by default: a page written by hand, or into global, is left for the user
	e.write(filepath.Join(e.home, "global/memories/note/by-hand.md"), "---\nkind: note\n---\n\n# By hand\n\nWritten in an editor.\n")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "ac0"})
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": "ac0", "last_assistant_message": "Done."})
	e.has(e.gm(dir, "status", "--porcelain"), "global/", "committed without autoCommit")
	rows, doc := e.doctor(dir)
	e.lacks(doc, "Auto commit", "doctor shows a setting that is off")
	_ = rows

	// on: it is committed, whatever wrote it, when a turn or a session ends
	e.write(filepath.Join(dir, ".git-memory.json"), `{"autoCommit": true}`)
	_, doc = e.doctor(dir)
	e.has(doc, "✓ Auto commit: on (.git-memory.json)")
	e.hook(dir, "claude", "Stop", map[string]any{"session_id": "ac0", "last_assistant_message": "Done."})
	e.eq(e.gm(dir, "status", "--porcelain"), "", "the change was not committed")
	e.has(e.gm(dir, "log", "-1", "--format=%s"), "memory: auto-commit 1 changed file")
	e.has(e.gm(dir, "ls-files"), "global/memories/note/by-hand.md")

	e.write(filepath.Join(e.home, "global/memories/fact/edited.md"), "---\nkind: fact\n---\n\n# Edited\n\nOne.\n")
	e.write(filepath.Join(e.home, "global/core/pinned.md"), "---\nkind: note\ntier: core\n---\n\n# Pinned\n\nTwo.\n")
	e.hook(dir, "claude", "SessionEnd", map[string]any{"session_id": "ac0"})
	e.eq(e.gm(dir, "status", "--porcelain"), "")
	e.has(e.gm(dir, "log", "--format=%s", "-n", "3"), "memory: auto-commit 2 changed files")

	// the next session starts clean too
	e.write(filepath.Join(e.home, "global/memories/fact/before-start.md"), "---\nkind: fact\n---\n\n# Before start\n\nThree.\n")
	e.hook(dir, "claude", "SessionStart", map[string]any{"session_id": "ac1"})
	e.eq(e.gm(dir, "status", "--porcelain"), "")

	// session pages stay out of it, and only the memory root folder is covered
	e.lacks(e.gm(dir, "ls-files"), "sessions/")

	// a value that is not a boolean is refused like any other bad setting
	e.write(filepath.Join(dir, ".git-memory.json"), `{"autoCommit": "yes"}`)
	_, doc = e.doctor(dir)
	e.has(doc, ".git-memory.json is not valid JSON")
}
