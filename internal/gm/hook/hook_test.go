package hook

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

func TestTokenize(t *testing.T) {
	if got := tokenize("the parseConfig files"); !reflect.DeepEqual(got, []string{"parse", "config", "file"}) {
		t.Errorf("got %v", got)
	}
	if got := tokenize("Ünïcode naïve café classes"); !reflect.DeepEqual(got, []string{"ünïcode", "naïve", "café", "classe"}) {
		t.Errorf("unicode: %v", got)
	}
}

func TestCompileSession(t *testing.T) {
	t0 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC).UnixMilli()
	page := compileSession([]core.Event{
		{T: t0, Ev: "start", Branch: "main", Head: "abc123"},
		{T: t0 + 1000, Ev: "prompt", Text: "Add retry logic to the uploader"},
		{T: t0 + 2000, Ev: "tool", Tool: "Edit", Files: []string{"src/upload.ts"}},
		{T: t0 + 3000, Ev: "tool", Tool: "Bash", Command: "npm test", Failed: true},
		{T: t0 + 4000, Ev: "stop", Last: "Added exponential backoff."},
	}, "claude", "abcdef123456")
	if page.Rel != "sessions/2026/10/2026-10-25-claude-abcdef12.md" {
		t.Errorf("rel = %s", page.Rel)
	}
	for _, want := range []string{
		"# Add retry logic to the uploader", "## Outcome\n\nAdded exponential backoff.", "`src/upload.ts`", "`npm test` (failed)",
		"tags: [src]", "created: 2026-10-25T12:00:00.000Z", "- Agent: claude · branch `main` · from `abc123`", "## Failures\n\n1 tool call(s) failed.",
	} {
		if !strings.Contains(page.Text, want) {
			t.Errorf("missing %q in:\n%s", want, page.Text)
		}
	}
}

func TestTriggersIn(t *testing.T) {
	found := triggersIn(strings.Join([]string{
		"Please fix the login bug.",
		"remember that we deploy on Tuesdays only",
		"Remember: staging is rebuilt nightly",
		"Decision: use pnpm, never npm",
		"#remember keep PRs under 400 lines",
		"#decision public APIs are REST",
		"#fact the staging API listens on 8081",
		"#procedure run make migrate before the tests",
		// none of these are memories:
		"remember to run the tests before you finish",
		"Note: the build is red on CI",
		"fact: pasted text from an email",
		"/remember keep this one",
		"We decided to use Redis, can you wire it up?",
		"remember that",
		"#remember short",
	}, "\n"))
	var got [][2]string
	for _, c := range found {
		got = append(got, [2]string{c.Kind, c.Text})
	}
	want := [][2]string{
		{"note", "we deploy on Tuesdays only"}, {"note", "staging is rebuilt nightly"}, {"decision", "use pnpm, never npm"},
		{"note", "keep PRs under 400 lines"}, {"decision", "public APIs are REST"}, {"fact", "the staging API listens on 8081"},
		{"procedure", "run make migrate before the tests"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if found[0].Title != "we deploy on Tuesdays only" {
		t.Errorf("title = %q", found[0].Title)
	}
}

func TestSummarizeToolPayloads(t *testing.T) {
	root := t.TempDir()
	cfg := core.DefaultConfig()
	ev := func(p core.HookPayload) *core.Event { return summarizeTool(p, root, cfg) }

	// Claude: snake_case, absolute path inside the project, object input
	if e := ev(core.HookPayload{"tool_name": "Edit", "tool_input": core.Obj{"file_path": filepath.Join(root, "src", "a.ts")}}); e == nil || !reflect.DeepEqual(e.Files, []string{"src/a.ts"}) {
		t.Errorf("claude edit: %+v", e)
	}
	// excluded and outside-the-project files are dropped, and so is a call with nothing left
	if e := ev(core.HookPayload{"tool_name": "Edit", "tool_input": core.Obj{"file_path": filepath.Join(root, ".env")}}); e != nil {
		t.Errorf(".env recorded: %+v", e)
	}
	if e := ev(core.HookPayload{"tool_name": "Write", "tool_input": core.Obj{"file_path": "/etc/passwd"}}); e != nil {
		t.Errorf("outside file recorded: %+v", e)
	}
	// Copilot: camelCase, toolArgs as a JSON string, failure in toolResult.resultType
	e := ev(core.HookPayload{"toolName": "run_in_terminal", "toolArgs": `{"command":"pnpm lint token=abcdefghijkl1234"}`, "toolResult": core.Obj{"resultType": "failure"}})
	if e == nil || e.Command != "pnpm lint token=[REDACTED]" || !e.Failed {
		t.Errorf("copilot shell: %+v", e)
	}
	// apply_patch style payloads name files inside the patch text
	patch := "*** Begin Patch\n*** Update File: pkg/x.go\n*** Add File: pkg/y.go\n*** End Patch"
	if e := ev(core.HookPayload{"tool_name": "apply_patch", "tool_input": core.Obj{"input": patch}}); e == nil || !reflect.DeepEqual(e.Files, []string{"pkg/x.go", "pkg/y.go"}) {
		t.Errorf("patch: %+v", e)
	}
	if e := ev(core.HookPayload{"tool_name": "Read", "tool_input": core.Obj{"file_path": "x"}}); e != nil {
		t.Errorf("reads are not recorded: %+v", e)
	}
}

func TestSpoolDedupesDoubleFiredHooks(t *testing.T) {
	dir := t.TempDir()
	e := core.Event{Ev: "prompt", Text: "hello"}
	if !appendEvent(dir, "claude", "s1", e) {
		t.Fatal("first event dropped")
	}
	if appendEvent(dir, "claude", "s1", e) {
		t.Error("the same event within 2s must be dropped (Copilot also reads .claude/settings.json)")
	}
	if !appendEvent(dir, "claude", "s1", core.Event{Ev: "prompt", Text: "another"}) {
		t.Error("a different event is kept")
	}
	if n := len(readEvents(spoolFile(dir, "claude", "s1"))); n != 2 {
		t.Errorf("%d events", n)
	}
}
