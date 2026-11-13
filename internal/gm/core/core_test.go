package core

import (
	"reflect"
	"strings"
	"testing"
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
		if got := StripHarnessContext(in); got != want {
			t.Errorf("stripHarnessContext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExcludeGlobs(t *testing.T) {
	g := []string{"**/.env", "**/.env.*", "secrets/**"}
	for p, want := range map[string]bool{".env": true, "app/.env.local": true, "secrets/a/b.txt": true, "src/env.ts": false, `app\.env`: true} {
		if got := IsExcluded(p, g); got != want {
			t.Errorf("isExcluded(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestPageRoundTripKeepsOrder(t *testing.T) {
	meta := NewMeta().Set("id", "x").Set("kind", "decision").SetList("tags", []string{"a", "b"}).Set("empty", "")
	text := SerializePage(meta, "# Title\n\nbody")
	want := "---\nid: x\nkind: decision\ntags: [a, b]\n---\n\n# Title\n\nbody\n"
	if text != want {
		t.Fatalf("serialize:\n%q\nwant\n%q", text, want)
	}
	p := ParsePage(text, "memories/decision/x.md", "personal")
	if p.Kind != "decision" || p.Title != "Title" || !reflect.DeepEqual(p.Tags, []string{"a", "b"}) || p.Scope != "personal" {
		t.Errorf("bad page: %+v", p)
	}
	// rewriting in place keeps the key order and puts new keys last
	again := SerializePage(p.Meta.Clone().Set("kind", "fact").Set("updated", "2026-01-01"), p.Body)
	if !strings.HasPrefix(again, "---\nid: x\nkind: fact\ntags: [a, b]\nupdated: 2026-01-01\n---") {
		t.Errorf("order changed:\n%s", again)
	}
	if got := ParsePage("no frontmatter\n", "n/a.md", "p"); got.Title != "a" || got.Kind != "note" || got.Tier != "semantic" {
		t.Errorf("defaults: %+v", got)
	}
}

func TestSectionOf(t *testing.T) {
	body := "# t\n\n## Outcome\n\ndone\nmore\n\n## Prompts\n\n1. x"
	if got := SectionOf(body, "Outcome"); got != "done\nmore" {
		t.Errorf("got %q", got)
	}
	if got := SectionOf(body, "Prompts"); got != "1. x" {
		t.Errorf("last section: %q", got)
	}
	if got := SectionOf(body, "Missing"); got != "" {
		t.Errorf("missing section: %q", got)
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
		if got := Slugify(in, 60); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slugify(strings.Repeat("ab ", 40), 10); got != "ab-ab-ab-a" || len(got) > 10 {
		t.Errorf("max length: %q", got)
	}
}

func TestMarshalJSONDoesNotEscapeHTML(t *testing.T) {
	if got := MarshalJSON(Obj{"a": "<b> & </b>"}, false); got != `{"a":"<b> & </b>"}` {
		t.Errorf("got %s", got)
	}
}
