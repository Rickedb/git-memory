// Package gm is git-memory: serverless AI memory for coding agents, stored as markdown in git.
//
// Error convention: like encoding/json internally, the library signals failure with a
// panic carrying a `failure`; the boundaries (Main, the hook handler, tests via Catch) recover and report it. Functions
// that can fail without it being an error are explicit about it (gitTry, tryEnsure, ...).
package gm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type failure struct{ msg string }

func (f failure) Error() string { return f.msg }

func fail(format string, a ...any) { panic(failure{fmt.Sprintf(format, a...)}) }

func check(err error) {
	if err != nil {
		panic(failure{err.Error()})
	}
}

// Catch runs fn and turns a failure raised inside into an error. Other panics (bugs) propagate.
func Catch(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(failure); ok {
				err = f
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

// catchAll is for hooks: whatever goes wrong must never reach the agent.
func catchAll(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
		}
	}()
	fn()
	return nil
}

// ---- files ------------------------------------------------------------------------------------------------------

func readFileTry(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func writeFile(p, content string) {
	check(os.WriteFile(p, []byte(content), 0o644))
}

func mkdirAll(p string) { check(os.MkdirAll(p, 0o755)) }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// resolvePath is path.resolve(base, p): p wins when absolute.
func resolvePath(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(base, p))
}

func slash(p string) string { return filepath.ToSlash(p) }

// relPath is filepath.Rel, but "" (not ".") when both paths are the same; an error only across drives.
func relPath(base, target string) (string, error) {
	r, err := filepath.Rel(base, target)
	if err != nil || r == "." {
		return "", err
	}
	return r, nil
}

// ---- git --------------------------------------------------------------------------------------------------------

type gitCmd struct {
	Cwd   string
	Input *string
	Env   map[string]string
}

func runGit(c gitCmd, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.Cwd
	cmd.Env = os.Environ()
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if c.Input != nil {
		cmd.Stdin = strings.NewReader(*c.Input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// git runs git in cwd and returns trimmed stdout; a failure panics.
func git(cwd string, args ...string) string {
	out, err := runGit(gitCmd{Cwd: cwd}, args...)
	check(err)
	return out
}

// gitTry is git with allowFail: ok is false when git exited non-zero.
func gitTry(cwd string, args ...string) (string, bool) {
	out, err := runGit(gitCmd{Cwd: cwd}, args...)
	return out, err == nil
}

func gitX(c gitCmd, args ...string) string {
	out, err := runGit(c, args...)
	check(err)
	return out
}

// ---- text -------------------------------------------------------------------------------------------------------

var accents = func() map[rune]rune {
	m := map[rune]rune{}
	for base, chars := range map[rune]string{
		'a': "àáâãäåāăąǎ", 'c': "çćĉċč", 'd': "ď", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥ", 'i': "ìíîïĩīĭįǐ", 'j': "ĵ",
		'k': "ķ", 'l': "ĺļľ", 'n': "ñńņň", 'o': "òóôõöōŏőǒ", 'r': "ŕŗř", 's': "śŝşš", 't': "ţť", 'u': "ùúûüũūŭůűųǔ",
		'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range chars {
			m[r] = base
		}
	}
	return m
}()

// slugify folds accents (the stdlib has no NFKD), lowercases and joins the alphanumeric runs with dashes.
func slugify(s string, max int) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		r = unicode.ToLower(r)
		if base, ok := accents[r]; ok {
			r = base
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	slug := b.String()
	if len(slug) > max {
		slug = slug[:max]
	}
	slug = strings.TrimRight(slug, "-")
	if slug == "" {
		return "untitled"
	}
	return slug
}

func runeSlice(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func trimEnd(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

// oneLine collapses whitespace and cuts to max characters with an ellipsis.
func oneLine(s string, max int) string {
	t := strings.Join(strings.Fields(s), " ")
	if len([]rune(t)) > max {
		return trimEnd(runeSlice(t, max-1)) + "…"
	}
	return t
}

func truncate(s string, max int) string {
	if len([]rune(s)) > max {
		return trimEnd(runeSlice(s, max)) + "…"
	}
	return s
}

func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func nowISO() string { return iso(time.Now()) }

func nowMs() int64 { return time.Now().UnixMilli() }

// ---- JSON -------------------------------------------------------------------------------------------------------

// marshalJSON is json.Marshal without HTML escaping (agents and humans read these files).
func marshalJSON(v any, indent bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	check(enc.Encode(v))
	return strings.TrimRight(buf.String(), "\n")
}

type obj = map[string]any

func asObj(v any) obj {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return obj{}
}

// ---- args -------------------------------------------------------------------------------------------------------

// Flags holds --name values: a string, or true for a bare switch.
type Flags map[string]any

// Str is the string value of a flag, "" when absent or a bare switch.
func (f Flags) Str(k string) string {
	s, _ := f[k].(string)
	return s
}

// Has reports whether a flag is set (a non-empty value or a bare switch).
func (f Flags) Has(k string) bool {
	switch v := f[k].(type) {
	case nil:
		return false
	case string:
		return v != ""
	}
	return true
}

var booleanFlags = map[string]bool{
	"core": true, "json": true, "quiet": true, "help": true, "branch": true, "personal": true, "local": true,
	"shared": true, "all": true, "report": true, "no-commands": true,
}

func parseArgs(argv []string) (Flags, []string) {
	flags := Flags{}
	var pos []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			pos = append(pos, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name := a[2:]
		if eq := strings.Index(a, "="); eq > 0 {
			flags[a[2:eq]] = a[eq+1:]
		} else if booleanFlags[name] || strings.HasPrefix(name, "no-") {
			flags[name] = true
		} else if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
			flags[name] = argv[i+1]
			i++
		} else {
			flags[name] = true
		}
	}
	return flags, pos
}

// ---- JSON files -------------------------------------------------------------------------------------------------

// readJSON reads a JSON object; anything missing or malformed is an empty object.
func readJSON(file string) obj {
	b, err := os.ReadFile(file)
	if err != nil {
		return obj{}
	}
	var o obj
	if json.Unmarshal(b, &o) != nil || o == nil {
		return obj{}
	}
	return o
}

func writeJSON(file string, data any) {
	mkdirAll(filepath.Dir(file))
	writeFile(file, marshalJSON(data, true)+"\n")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
