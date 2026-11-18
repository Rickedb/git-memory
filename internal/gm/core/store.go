package core

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// locate finds the repository containing cwd and where its local state lives.
func Locate(cwd string) BaseLoc {
	top, ok := GitTry(cwd, "rev-parse", "--show-toplevel")
	if !ok || top == "" {
		Fail("%s is not inside a git repository: git-memory wires a project that is one (its hooks keep their local state in the project's .git). Run `git init` there first; no commit is needed", cwd)
	}
	root := filepath.FromSlash(top)
	common := resolvePath(root, filepath.FromSlash(Git(root, "rev-parse", "--git-common-dir")))
	data := filepath.Join(common, "git-memory")
	return BaseLoc{Root: root, Common: common, Data: data, Spool: filepath.Join(data, "spool")}
}

// openProject resolves the project containing cwd and the memory repository it writes to.
func OpenProject(cwd string) *Project {
	base := Locate(cwd)
	mem := MemoryDirFor(base.Root)
	return &Project{BaseLoc: base, Mem: mem, Base: RootFolder(mem), AutoCommitOn: AutoCommitSetting(mem), Scope: ReadScope(base.Data)}
}

// ScopeFile is where this checkout remembers the scope the user works in: <git dir>/git-memory/scope. It is local, like
// the launcher next to it: never committed, and each person (each checkout) chooses their own.
const ScopeFile = "scope"

var scopeName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// NormalizeScope checks a scope's name: lowercase words joined by dashes (it is a folder name).
func NormalizeScope(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if !scopeName.MatchString(s) {
		Fail("a scope is a name of lowercase letters, digits and dashes, like checkout or mobile-app (not %q)", v)
	}
	return s
}

// ReadScope is the scope chosen on this checkout ("" for none: every scope is read).
func ReadScope(data string) string {
	text, ok := ReadFileTry(filepath.Join(data, ScopeFile))
	if !ok || strings.TrimSpace(text) == "" {
		return ""
	}
	return NormalizeScope(text)
}

// SetScope chooses the scope this checkout works in; "" clears it.
func SetScope(data, scope string) {
	file := filepath.Join(data, ScopeFile)
	if scope == "" {
		_ = os.Remove(file)
		return
	}
	MkdirAll(data)
	WriteFile(file, NormalizeScope(scope)+"\n")
}

// ScopeOf is the scope a page of a project folder belongs to ("" for the project's own pages): scopes/<name>/....
func ScopeOf(pagePath string) string {
	if rest, ok := strings.CutPrefix(pagePath, "scopes/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		return name
	}
	return ""
}

// RootFile is the file, at the top of the project (next to the .git-memory folder, and committed with the project), that
// names the folder of the memory repository that holds the memories (memories/, scopes/, sessions/, global/): for a memory repository whose memories live in
// a folder of it (docs/memory) instead of at its top. It is `{"root": "docs/memory"}`; without the file (or without
// "root") the root folder is the repository's top. It belongs to the project, not to the memory repository, so everyone
// who clones the project gets the same layout.
const RootFile = ".git-memory.json"

// NormalizeRoot turns what the user wrote into a folder relative to the repository's root, slash-separated, "" for the
// root itself. It fails for anything that is not a folder inside the repository.
func NormalizeRoot(v string) string {
	s := strings.TrimSpace(strings.ReplaceAll(v, "\\", "/"))
	if s == "" {
		return ""
	}
	c := path.Clean(s)
	if strings.HasPrefix(c, "/") || (len(c) > 1 && c[1] == ':') || c == ".." || strings.HasPrefix(c, "../") || c == ".git" || strings.HasPrefix(c, ".git/") {
		Fail(`"root" in %s must be a folder inside the memory repository, like docs/memory (not %q)`, RootFile, v)
	}
	if c == "." {
		return ""
	}
	return c
}

// rootFilePath is where the project keeps RootFile: the folder that holds the memory repository.
func rootFilePath(mem string) string { return filepath.Join(filepath.Dir(mem), RootFile) }

// fileConfig is what the project's RootFile says.
type fileConfig struct {
	Root       string `json:"root"`
	AutoCommit bool   `json:"autoCommit"`
}

// readFileConfig reads the project's RootFile: the zero value when it is not there (or the memory repository is not).
func readFileConfig(mem string) fileConfig {
	var cfg fileConfig
	if !IsMemoryRepo(mem) {
		return cfg
	}
	b, err := os.ReadFile(rootFilePath(mem))
	if err != nil {
		return cfg
	}
	if json.Unmarshal(b, &cfg) != nil {
		Fail(`%s is not valid JSON: it should read {"root": "docs/memory", "autoCommit": true}`, RootFile)
	}
	return cfg
}

// RootFolder is the folder of the memory repository that holds the memories (see RootFile): "" for the top.
func RootFolder(mem string) string { return NormalizeRoot(readFileConfig(mem).Root) }

// AutoCommitSetting is "autoCommit" in the project's RootFile: whether the hooks commit every change under the memory
// root folder (not only the pages they write themselves). Off when the file or the key is not there.
func AutoCommitSetting(mem string) bool { return readFileConfig(mem).AutoCommit }

// AutoCommit commits whatever changed under the memory root folder when the project asks for it (see AutoCommitSetting),
// and reports whether it committed. Session pages are ignored by git, so they are never part of it. The caller holds the
// memory repository's lock.
func (p *Project) AutoCommit() bool {
	if !p.AutoCommitOn {
		return false
	}
	out := strings.TrimSpace(Git(p.Mem, "status", "--porcelain", "--", p.Spec()))
	if out == "" {
		return false
	}
	return CommitPaths(p.Mem, "memory: auto-commit "+plural(strings.Count(out, "\n")+1, "changed file", "changed files"), p.Spec())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// SetRootFolder writes the root folder into the project's RootFile (keeping anything else in it). It does not commit:
// the file is the project's, so committing it is the project's business.
func SetRootFolder(mem, folder string) {
	root := NormalizeRoot(folder)
	if root == "" {
		root = "."
	}
	file := rootFilePath(mem)
	cfg := ReadJSON(file)
	cfg["root"] = root
	WriteJSON(file, cfg)
}

// InRoot is a path inside the memory root folder, relative to the repository's top: InRoot("docs/memory", "global") is
// docs/memory/global, and InRoot("", "global") is global.
func InRoot(base, rel string) string {
	if base == "" {
		return rel
	}
	return base + "/" + rel
}

// excludeLocally adds an entry to .git/info/exclude: it keeps files out of `git status` without touching .gitignore.
func ExcludeLocally(loc BaseLoc, entry string) {
	file := filepath.Join(loc.Common, "info", "exclude")
	old, _ := ReadFileTry(file)
	for _, l := range strings.Split(old, "\n") {
		if l == entry {
			return
		}
	}
	MkdirAll(filepath.Dir(file))
	sep := ""
	if old != "" && !strings.HasSuffix(old, "\n") {
		sep = "\n"
	}
	WriteFile(file, old+sep+entry+"\n")
}

func identityEnv(cwd string) map[string]string {
	if _, ok := GitTry(cwd, "config", "user.email"); ok {
		return nil
	}
	return map[string]string{
		"GIT_AUTHOR_NAME": "git-memory", "GIT_AUTHOR_EMAIL": "git-memory@localhost",
		"GIT_COMMITTER_NAME": "git-memory", "GIT_COMMITTER_EMAIL": "git-memory@localhost",
	}
}

func IsMemoryRepo(mem string) bool { return Exists(filepath.Join(mem, ".git")) }

// dirIsEmpty is true for a folder that does not exist or has nothing in it.
func DirIsEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}

// requireMemoryRepo fails unless mem is a memory repository. Nothing but `git-memory init` (or a clone, or
// `git-memory agent init`) ever creates one, so a mistyped folder never grows a stray .git-memory.
func RequireMemoryRepo(mem string) {
	if IsMemoryRepo(mem) {
		return
	}
	if !DirIsEmpty(mem) {
		Fail("%s exists, is not empty and is not a git repository: run `git-memory init` there to use it as the memory repository, or move it aside", mem)
	}
	Fail("no memory repository at %s: run `git-memory init` in the folder that should have one", mem)
}

// initMemoryRepo creates the memory repository for `agent init`. It only ever initialises a folder that is missing or
// empty: anything else is somebody's data, and `git-memory init` is the explicit way to use it.
func InitMemoryRepo(mem string) {
	if IsMemoryRepo(mem) {
		return
	}
	if !DirIsEmpty(mem) && !onlyOurIgnore(mem) {
		Fail("%s exists, is not empty and is not a git repository: move it aside, or run `git-memory init` there to use it as the memory repository", mem)
	}
	MkdirAll(mem)
	Git(mem, "init", "-q", "-b", "main")
}

// sessionsIgnoreRule keeps the session pages out of git (relative to the root folder's own .gitignore).
const sessionsIgnoreRule = "/sessions/"

// IgnoreSessions makes the memory repository's .gitignore keep session pages out of git: they are the local record of
// each agent session (the briefing, `list sessions` and `doctor` read them from disk), not memory to share. It adds
// the rule and leaves the rest of the file alone. It reports whether it changed the file.
func IgnoreSessions(mem string) bool {
	dir := filepath.Join(mem, filepath.FromSlash(RootFolder(mem))) // the rule is relative to the folder its .gitignore is in
	MkdirAll(dir)
	file := filepath.Join(dir, ".gitignore")
	old, _ := ReadFileTry(file)
	for _, l := range strings.Split(old, "\n") {
		if strings.TrimSpace(l) == sessionsIgnoreRule {
			return false
		}
	}
	sep := ""
	if old != "" {
		sep = "\n"
		if !strings.HasSuffix(old, "\n") {
			sep = "\n\n"
		}
	}
	WriteFile(file, old+sep+ignoreText)
	return true
}

// ignoreText is what IgnoreSessions writes into a .gitignore of its own.
const ignoreText = "# git-memory: session pages are a local record of each agent session, not shared memory\n" + sessionsIgnoreRule + "\n"

// dropStrayIgnore removes the .gitignore at the top of the repository when it is only ours and was never committed:
// `git-memory init` writes it before the root folder is set, and the rule now lives in the root folder's own.
func dropStrayIgnore(mem string) {
	file := filepath.Join(mem, ".gitignore")
	if text, ok := ReadFileTry(file); ok && text == ignoreText {
		if _, tracked := GitTry(mem, "ls-files", "--error-unmatch", "--", ".gitignore"); !tracked {
			_ = os.Remove(file)
		}
	}
}

// EnsureSessionsIgnored is IgnoreSessions for a repository that exists: it also commits the .gitignore when it changed
// it or when it was never committed, so the rule travels with the repository and `status` stays clean.
func EnsureSessionsIgnored(mem string) {
	changed := IgnoreSessions(mem)
	base := RootFolder(mem)
	if base != "" {
		dropStrayIgnore(mem)
	}
	ignore := InRoot(base, ".gitignore")
	if _, tracked := GitTry(mem, "ls-files", "--error-unmatch", "--", ignore); changed || !tracked {
		CommitPaths(mem, "memory: keep session pages out of git", ignore)
	}
}

// withLock serializes writers of the memory repository with a lock directory (mkdir is atomic, also on Windows).
func WithLock(mem string, fn func()) {
	if !IsMemoryRepo(mem) {
		Fail("%s is not a git repository", mem)
	}
	lock := filepath.Join(mem, ".git", "git-memory.lock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		err := os.Mkdir(lock, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			Check(err)
		}
		if st, serr := os.Stat(lock); serr == nil && time.Since(st.ModTime()) > 30*time.Second {
			os.RemoveAll(lock)
		}
		if time.Now().After(deadline) {
			Fail("could not acquire the memory repository lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer os.RemoveAll(lock)
	fn()
}

// commitPaths commits what changed under the given paths and nothing else: whatever else is staged in the repository
// (the user may be in the middle of their own work there) stays staged.
func CommitPaths(mem, message string, paths ...string) bool {
	spec := append([]string{"--"}, paths...)
	Git(mem, append([]string{"add", "-A"}, spec...)...)
	if Git(mem, append([]string{"status", "--porcelain"}, spec...)...) == "" {
		return false
	}
	gitX(gitCmd{Cwd: mem, Env: identityEnv(mem)}, append([]string{"-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "-m", message}, spec...)...)
	return true
}

// lastCommitAgeSec is how long ago path was last committed (a huge number when never).
func LastCommitAgeSec(mem, path string) int64 {
	ts, ok := GitTry(mem, "log", "-1", "--format=%ct", "--", path)
	if !ok || ts == "" {
		return 1 << 40
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 1 << 40
	}
	return time.Now().Unix() - n
}

// Scopes lists the scopes that exist in the memory root folder.
func (p *Project) Scopes() []string {
	entries, _ := os.ReadDir(filepath.Join(p.Dir(), "scopes"))
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

var allScopesAsked = regexp.MustCompile(`(?i)\b(?:all|every|each|any)\s+(?:of\s+)?(?:the\s+)?scopes?\b|\bacross\s+(?:the\s+)?scopes\b|\b(?:without|ignore|ignoring|regardless of)\s+(?:the\s+|my\s+)?scopes?\b`)

// ScopesAsked reads a prompt for a request to look beyond the chosen scope: "all scopes", "across scopes", "ignore the
// scope", or the name of another scope that exists ("in the search scope", "scope search"). It is how a user, for one
// prompt, reads more than the scope this checkout works in.
func ScopesAsked(prompt string, existing []string) (all bool, names []string) {
	if allScopesAsked.MatchString(prompt) {
		return true, nil
	}
	for _, name := range existing {
		q := regexp.QuoteMeta(name)
		if regexp.MustCompile(`(?i)(?:\b` + q + `\s+scope\b|\bscope\s*[:=]?\s*["']?` + q + `\b)`).MatchString(prompt) {
			names = append(names, name)
		}
	}
	return false, names
}

// onlyOurIgnore is true for a folder that holds nothing but the .gitignore `git-memory init` writes before it runs git: a
// `git init` that failed (a mistyped option) leaves exactly that behind, and it is not somebody's data.
func onlyOurIgnore(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".gitignore" {
		return false
	}
	text, ok := ReadFileTry(filepath.Join(dir, ".gitignore"))
	return ok && text == ignoreText
}
