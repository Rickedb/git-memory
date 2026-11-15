package gm

// The agents beyond Claude Code and Copilot: Codex, Cursor and Gemini CLI. Each is wired by name, writes only its own
// files, keeps what the user already had in those files, and its hooks speak its payloads and print its output.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Each agent's files: its hooks, one slash command, and its instructions file when it has one.
var agentFiles = map[string][]string{
	"claude":  {".claude/settings.local.json", ".claude/skills/recall/SKILL.md", ".claude/rules/git-memory.md"},
	"copilot": {".github/hooks/git-memory.json", ".github/prompts/recall.prompt.md", ".github/instructions/git-memory.instructions.md"},
	"codex":   {".codex/hooks.json", ".agents/skills/recall/SKILL.md"},
	"cursor":  {".cursor/hooks.json", ".cursor/commands/recall.md", ".cursor/rules/git-memory.mdc"},
	"gemini":  {".gemini/settings.json", ".gemini/commands/recall.toml"},
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
	e.has(doc, "✓ Slash commands: Claude Code 7/7, Copilot 7/7, Codex 7/7, Cursor 7/7, Gemini CLI 7/7")

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

	codex := e.read(filepath.Join(dir, ".agents/skills/remember/SKILL.md"))
	e.ok(strings.HasPrefix(codex, "---\nname: remember\ndescription: "), "a Codex skill starts with its name and description")
	e.lacks(codex, "disable-model-invocation", "that key is Claude Code's")
	e.has(codex, "./.git/git-memory/gm doctor --report")

	cursor := e.read(filepath.Join(dir, ".cursor/commands/remember.md"))
	e.ok(strings.HasPrefix(cursor, "<!-- git-memory:generated"), "a Cursor command is plain markdown, with no frontmatter")
	e.has(cursor, "./.git/git-memory/gm doctor --report")

	gem := e.read(filepath.Join(dir, ".gemini/commands/remember.toml"))
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
	e.write(filepath.Join(dir, ".cursor/commands/recall.md"), "my own recall\n")
	e.write(filepath.Join(dir, ".gemini/commands/forget.toml"), "prompt = 'mine'\n")
	out := e.gm(dir, "agent", "init", "cursor", "gemini")
	e.has(out, "note: kept your own .cursor/commands/recall.md")
	e.has(out, "note: kept your own .gemini/commands/forget.toml")
	e.eq(e.read(filepath.Join(dir, ".cursor/commands/recall.md")), "my own recall\n")
}

func TestCodexSessionIsRecordedAndBriefed(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	dir := e.newProject("codex")
	proj := e.project(dir)
	e.commitPage(dir, proj+"/memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

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
	e.commitPage(dir, proj+"/memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

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
	e.commitPage(dir, proj+"/memories/decision/package-manager.md", "---\nkind: decision\ntier: semantic\ntags: []\n---\n\n# Package manager\n\nWe use pnpm, never npm, in this monorepo.\n")

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
	e.has(e.gm(dir, "grep", "-l", "-I", "-e", "tagged with a v prefix", "--", "projects"), "memories/")
}
