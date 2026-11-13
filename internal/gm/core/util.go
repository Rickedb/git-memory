// Package core is what git-memory is built on: the memory repository and its pages, the plain-git helpers, the
// markdown page format, secret redaction and the settings. The agent hooks (package hook) and the commands (package gm)
// both use it, and it uses neither.
//
// Error convention: like encoding/json internally, the library signals failure with a panic carrying a Failure; the
// boundaries (gm.Main, the hook handler, tests via Catch) recover and report it. Functions that can fail without it
// being an error are explicit about it (GitTry, ...).
package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

type Failure struct{ Msg string }

func (f Failure) Error() string { return f.Msg }

func Fail(format string, a ...any) { panic(Failure{fmt.Sprintf(format, a...)}) }

func Check(err error) {
	if err != nil {
		panic(Failure{err.Error()})
	}
}

// Catch runs fn and turns a failure raised inside into an error. Other panics (bugs) propagate.
func Catch(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(Failure); ok {
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
func CatchAll(fn func()) (err error) {
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

func ReadFileTry(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func WriteFile(p, content string) {
	Check(os.WriteFile(p, []byte(content), 0o644))
}

func MkdirAll(p string) { Check(os.MkdirAll(p, 0o755)) }

func Exists(p string) bool {
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

func Slash(p string) string { return filepath.ToSlash(p) }

// relPath is filepath.Rel, but "" (not ".") when both paths are the same; an error only across drives.
func RelPath(base, target string) (string, error) {
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
func Git(cwd string, args ...string) string {
	out, err := runGit(gitCmd{Cwd: cwd}, args...)
	Check(err)
	return out
}

// gitTry is git with allowFail: ok is false when git exited non-zero.
func GitTry(cwd string, args ...string) (string, bool) {
	out, err := runGit(gitCmd{Cwd: cwd}, args...)
	return out, err == nil
}

func gitX(c gitCmd, args ...string) string {
	out, err := runGit(c, args...)
	Check(err)
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
func Slugify(s string, max int) string {
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

func RuneSlice(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func TrimEnd(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

// oneLine collapses whitespace and cuts to max characters with an ellipsis.
func OneLine(s string, max int) string {
	t := strings.Join(strings.Fields(s), " ")
	if len([]rune(t)) > max {
		return TrimEnd(RuneSlice(t, max-1)) + "…"
	}
	return t
}

func Truncate(s string, max int) string {
	if len([]rune(s)) > max {
		return TrimEnd(RuneSlice(s, max)) + "…"
	}
	return s
}

func Iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func nowISO() string { return Iso(time.Now()) }

func NowMs() int64 { return time.Now().UnixMilli() }

// ---- JSON -------------------------------------------------------------------------------------------------------

// marshalJSON is json.Marshal without HTML escaping (agents and humans read these files).
func MarshalJSON(v any, indent bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	Check(enc.Encode(v))
	return strings.TrimRight(buf.String(), "\n")
}

type Obj = map[string]any

func AsObj(v any) Obj {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return Obj{}
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
	"shared": true, "all": true, "report": true, "no-commands": true, "no-instructions": true,
}

func ParseArgs(argv []string) (Flags, []string) {
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
func ReadJSON(file string) Obj {
	b, err := os.ReadFile(file)
	if err != nil {
		return Obj{}
	}
	var o Obj
	if json.Unmarshal(b, &o) != nil || o == nil {
		return Obj{}
	}
	return o
}

func WriteJSON(file string, data any) {
	MkdirAll(filepath.Dir(file))
	WriteFile(file, MarshalJSON(data, true)+"\n")
}

func Contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// wrapperName is the per-project launcher inside .git/git-memory. Hook files refer to it by a project-relative path, so
// they read the same on every machine; it is the only file that knows where the real binary lives.
func WrapperName() string {
	if runtime.GOOS == "windows" {
		return "gm.exe"
	}
	return "gm"
}
