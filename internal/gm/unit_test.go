package gm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRedactRemovesCommonSecrets(t *testing.T) {
	in := strings.Join([]string{
		"key sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"DATABASE_PASSWORD=hunter2hunter2",
		`API_KEY="quotedsecret99"`,
		"Authorization: Bearer abcdefghijklmnop1234567890",
		"postgres://user:s3cretpass@localhost/db",
	}, "\n")
	out := redact(in)
	for _, leaked := range []string{"abcdefghijklmnopqrstuvwxyz", "hunter2hunter2", "quotedsecret99", "abcdefghijklmnop1234567890", "s3cretpass"} {
		if strings.Contains(out, leaked) {
			t.Errorf("leaked %q in:\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "postgres://user:[REDACTED]@localhost") {
		t.Errorf("url credentials not redacted: %s", out)
	}
	if !strings.Contains(out, "Bearer [REDACTED]") {
		t.Errorf("bearer prefix lost: %s", out)
	}
}

func TestStripPrivate(t *testing.T) {
	if got := stripPrivate("a <private>x\ny</private> b"); got != "a [private omitted] b" {
		t.Errorf("got %q", got)
	}
}

// RE2 has no backreferences, so the closing tag is paired by hand; these are the cases the regexp version handled.
func TestStripHarnessContext(t *testing.T) {
	cases := map[string]string{
		"<ide_opened_file>a.ts</ide_opened_file>fix the bug":                          "fix the bug",
		"fix <system-reminder>x\ny</system-reminder> now":                             "fix  now",
		"<ide_selection>one</ide_selection> a <ide_opened_file>two</ide_opened_file>": "a",
		"<IDE_Selection>x</ide_selection>real prompt":                                 "real prompt",
		"<ide_selection>never closed, keep it":                                        "<ide_selection>never closed, keep it",
		"<ide_a>x</ide_b>y":                                                           "<ide_a>x</ide_b>y", // mismatched tags are not a block
		"<unrelated>keep</unrelated>":                                                 "<unrelated>keep</unrelated>",
		"<command-name>/x</command-name>go":                                           "go",
		"<local-command-stdout>o</local-command-stdout>":                              "",
		"<ide_x>1</ide_x><ide_x>2</ide_x>after":                                       "after",
		"  only text  ":                                                               "only text",
	}
	for in, want := range cases {
		if got := stripHarnessContext(in); got != want {
			t.Errorf("stripHarnessContext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExcludeGlobs(t *testing.T) {
	g := []string{"**/.env", "**/.env.*", "secrets/**"}
	for p, want := range map[string]bool{".env": true, "app/.env.local": true, "secrets/a/b.txt": true, "src/env.ts": false, `app\.env`: true} {
		if got := isExcluded(p, g); got != want {
			t.Errorf("isExcluded(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestPageRoundTripKeepsOrder(t *testing.T) {
	meta := newMeta().Set("id", "x").Set("kind", "decision").SetList("tags", []string{"a", "b"}).Set("empty", "")
	text := serializePage(meta, "# Title\n\nbody")
	want := "---\nid: x\nkind: decision\ntags: [a, b]\n---\n\n# Title\n\nbody\n"
	if text != want {
		t.Fatalf("serialize:\n%q\nwant\n%q", text, want)
	}
	p := parsePage(text, "memories/decision/x.md", "personal")
	if p.Kind != "decision" || p.Title != "Title" || !reflect.DeepEqual(p.Tags, []string{"a", "b"}) || p.Scope != "personal" {
		t.Errorf("bad page: %+v", p)
	}
	// rewriting in place keeps the key order and puts new keys last
	again := serializePage(p.Meta.Clone().Set("kind", "fact").Set("updated", "2026-01-01"), p.Body)
	if !strings.HasPrefix(again, "---\nid: x\nkind: fact\ntags: [a, b]\nupdated: 2026-01-01\n---") {
		t.Errorf("order changed:\n%s", again)
	}
	if got := parsePage("no frontmatter\n", "n/a.md", "p"); got.Title != "a" || got.Kind != "note" || got.Tier != "semantic" {
		t.Errorf("defaults: %+v", got)
	}
}

func TestSectionOf(t *testing.T) {
	body := "# t\n\n## Outcome\n\ndone\nmore\n\n## Prompts\n\n1. x"
	if got := sectionOf(body, "Outcome"); got != "done\nmore" {
		t.Errorf("got %q", got)
	}
	if got := sectionOf(body, "Prompts"); got != "1. x" {
		t.Errorf("last section: %q", got)
	}
	if got := sectionOf(body, "Missing"); got != "" {
		t.Errorf("missing section: %q", got)
	}
}

func TestSummarySectionSplice(t *testing.T) {
	old := "# t\n\n- Agent\n\n## Summary\n\nkept bullet\n\n## Outcome\n\ndone\n"
	m := summarySection(old)
	if m != "\n## Summary\n\nkept bullet\n" {
		t.Fatalf("section = %q", m)
	}
	body := "# t\n\n- Agent\n\n## Outcome\n\nnew\n"
	got := spliceFirstSection(body, m+"\n## ")
	if want := "# t\n\n- Agent\n\n## Summary\n\nkept bullet\n\n## Outcome\n\nnew\n"; got != want {
		t.Errorf("spliced:\n%q\nwant\n%q", got, want)
	}
	if summarySection("# t\n\n## Outcome\n") != "" || summarySection("\n## Summary\n\nlast section only") != "" {
		t.Error("a summary with no following heading is not a match (same as the original lookahead)")
	}
}

func TestTokenize(t *testing.T) {
	if got := tokenize("the parseConfig files"); !reflect.DeepEqual(got, []string{"parse", "config", "file"}) {
		t.Errorf("got %v", got)
	}
	if got := tokenize("Ünïcode naïve café classes"); !reflect.DeepEqual(got, []string{"ünïcode", "naïve", "café", "classe"}) {
		t.Errorf("unicode: %v", got)
	}
}

func TestNormalizeEvent(t *testing.T) {
	for in, want := range map[string]hookEvent{
		"SessionStart": evStart, "sessionStart": evStart, "session_start": evStart, "userPromptSubmitted": evPrompt, "UserPromptSubmit": evPrompt,
		"agentStop": evStop, "Stop": evStop, "PostToolUse": evTool, "PostToolUseFailure": evToolFail, "preCompact": evPreCompact, "SessionEnd": evEnd,
	} {
		if got, ok := normalizeEvent(in); !ok || got != want {
			t.Errorf("normalizeEvent(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := normalizeEvent("nope"); ok {
		t.Error("unknown event accepted")
	}
}

func TestCompileSession(t *testing.T) {
	t0 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC).UnixMilli()
	page := compileSession([]Event{
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

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Use Postgres, not SQLite!":  "use-postgres-not-sqlite",
		"???":                        "untitled",
		"Café déjà vu — Zürich":      "cafe-deja-vu-zurich",
		"  --Leading and trailing--": "leading-and-trailing",
		"日本語":                        "untitled",
	} {
		if got := slugify(in, 60); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slugify(strings.Repeat("ab ", 40), 10); got != "ab-ab-ab-a" || len(got) > 10 {
		t.Errorf("max length: %q", got)
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

func TestPortableAndIsLocalPath(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "proj", "app")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	for in, want := range map[string]string{
		filepath.Join(root, "proj", "memory.git"): "../memory.git",
		filepath.Join(base, "sub", "m.git"):       "./sub/m.git",
		base:                                      ".",
		"../x.git":                                "../x.git",
	} {
		if got := portable(in, base); got != want {
			t.Errorf("portable(%q) = %q, want %q", in, got, want)
		}
	}
	for _, url := range []string{"git@github.com:acme/mem.git", "https://example.com/a/b.git", "ssh://git@host/a.git", "file:///srv/m.git"} {
		if got := portable(url, base); got != url {
			t.Errorf("url %q rewritten to %q", url, got)
		}
		if isLocalPath(url) {
			t.Errorf("%q is a URL", url)
		}
	}
	for _, p := range []string{"./a", "../a", "/a", "~/a", `C:\a`, "C:/a"} {
		if !isLocalPath(p) {
			t.Errorf("%q is a path", p)
		}
	}
}

func TestLoadConfigResolvesRelativeReposAgainstTheirOwnFile(t *testing.T) {
	root := t.TempDir()
	home, proj := filepath.Join(root, "userhome"), filepath.Join(root, "work", "app")
	data := filepath.Join(proj, ".git", "git-memory")
	for _, d := range []string{home, proj, data} {
		os.MkdirAll(d, 0o755)
	}
	t.Setenv("GIT_MEMORY_HOME", home)
	write := func(p, s string) { os.WriteFile(p, []byte(s), 0o644) }
	write(filepath.Join(home, "config.json"), `{"personal":{"repo":"../personal.git"},"autoRemember":{"maxPerPrompt":2}}`)
	write(filepath.Join(proj, "git-memory.json"), `{"store":{"repo":"../team.git"},"scopes":{"pay":{"repo":"../pay.git"}},"defaultScope":"project"}`)
	write(filepath.Join(data, "config.json"), `{"defaultScope":null,"briefing":{"maxChars":99}}`)

	cfg := loadConfig(BaseLoc{Root: proj, Data: data})
	if got := *cfg.Personal.Repo; got != filepath.Join(root, "personal.git") {
		t.Errorf("personal.repo resolved against the user dir: %s", got)
	}
	if got := *cfg.Store.Repo; got != filepath.Join(root, "work", "team.git") {
		t.Errorf("store.repo: %s", got)
	}
	if got := *cfg.Scopes["pay"].Repo; got != filepath.Join(root, "work", "pay.git") {
		t.Errorf("scope repo: %s", got)
	}
	if cfg.DefaultScope != "project" {
		t.Errorf("a null in a higher layer must not erase a lower one: %q", cfg.DefaultScope)
	}
	if cfg.Briefing.MaxChars != 99 || cfg.Briefing.RecentSessions != 3 {
		t.Errorf("deep merge: %+v", cfg.Briefing)
	}
	if cfg.AutoRemember.MaxPerPrompt != 2 || !cfg.AutoRemember.Triggers || cfg.AutoRemember.Scope != "personal" {
		t.Errorf("autoRemember: %+v", cfg.AutoRemember)
	}
	if cfg.Capture.MaxPromptChars != 1500 || len(cfg.Capture.Exclude) != 5 {
		t.Errorf("defaults lost: %+v", cfg.Capture)
	}
}

func TestSummarizeToolPayloads(t *testing.T) {
	root := t.TempDir()
	cfg := defaultConfig()
	ev := func(p HookPayload) *Event { return summarizeTool(p, root, cfg) }

	// Claude: snake_case, absolute path inside the project, object input
	if e := ev(HookPayload{"tool_name": "Edit", "tool_input": obj{"file_path": filepath.Join(root, "src", "a.ts")}}); e == nil || !reflect.DeepEqual(e.Files, []string{"src/a.ts"}) {
		t.Errorf("claude edit: %+v", e)
	}
	// excluded and outside-the-project files are dropped, and so is a call with nothing left
	if e := ev(HookPayload{"tool_name": "Edit", "tool_input": obj{"file_path": filepath.Join(root, ".env")}}); e != nil {
		t.Errorf(".env recorded: %+v", e)
	}
	if e := ev(HookPayload{"tool_name": "Write", "tool_input": obj{"file_path": "/etc/passwd"}}); e != nil {
		t.Errorf("outside file recorded: %+v", e)
	}
	// Copilot: camelCase, toolArgs as a JSON string, failure in toolResult.resultType
	e := ev(HookPayload{"toolName": "run_in_terminal", "toolArgs": `{"command":"pnpm lint token=abcdefghijkl1234"}`, "toolResult": obj{"resultType": "failure"}})
	if e == nil || e.Command != "pnpm lint token=[REDACTED]" || !e.Failed {
		t.Errorf("copilot shell: %+v", e)
	}
	// apply_patch style payloads name files inside the patch text
	patch := "*** Begin Patch\n*** Update File: pkg/x.go\n*** Add File: pkg/y.go\n*** End Patch"
	if e := ev(HookPayload{"tool_name": "apply_patch", "tool_input": obj{"input": patch}}); e == nil || !reflect.DeepEqual(e.Files, []string{"pkg/x.go", "pkg/y.go"}) {
		t.Errorf("patch: %+v", e)
	}
	if e := ev(HookPayload{"tool_name": "Read", "tool_input": obj{"file_path": "x"}}); e != nil {
		t.Errorf("reads are not recorded: %+v", e)
	}
}

func TestMarshalJSONDoesNotEscapeHTML(t *testing.T) {
	if got := marshalJSON(obj{"a": "<b> & </b>"}, false); got != `{"a":"<b> & </b>"}` {
		t.Errorf("got %s", got)
	}
}

func TestSpoolDedupesDoubleFiredHooks(t *testing.T) {
	loc := &Loc{BaseLoc: BaseLoc{Spool: t.TempDir()}}
	e := Event{Ev: "prompt", Text: "hello"}
	if !appendEvent(loc, "claude", "s1", e) {
		t.Fatal("first event dropped")
	}
	if appendEvent(loc, "claude", "s1", e) {
		t.Error("the same event within 2s must be dropped (Copilot also reads .claude/settings.json)")
	}
	if !appendEvent(loc, "claude", "s1", Event{Ev: "prompt", Text: "another"}) {
		t.Error("a different event is kept")
	}
	if n := len(readEvents(spoolFile(loc, "claude", "s1"))); n != 2 {
		t.Errorf("%d events", n)
	}
}

func TestIsOursMatchesOldAndNewHookCommands(t *testing.T) {
	for _, c := range []string{
		`"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" hook claude Stop`,
		`./.git/git-memory/gm hook copilot agentStop`,
		`./.git/git-memory/gm.exe hook copilot agentStop`,
		`node "$CLAUDE_PROJECT_DIR/.git/git-memory/runtime/cli.js" hook claude Stop`,
		`node ./.git/git-memory/runtime/cli.js hook copilot Stop`,
		`node .claude/git-memory.mjs hook claude Stop`,
		`git-memory hook claude Stop`,
	} {
		if !isOurs(c) {
			t.Errorf("not recognized: %s", c)
		}
	}
	for _, c := range []string{`echo hook claude`, `./scripts/other.sh hook x`, `prettier --write`} {
		if isOurs(c) {
			t.Errorf("wrongly claimed: %s", c)
		}
	}
}
