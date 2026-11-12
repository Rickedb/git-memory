package gm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// locate finds the repository containing cwd and where its local state lives.
func locate(cwd string) BaseLoc {
	root := filepath.FromSlash(git(cwd, "rev-parse", "--show-toplevel"))
	common := resolvePath(root, filepath.FromSlash(git(root, "rev-parse", "--git-common-dir")))
	data := filepath.Join(common, "git-memory")
	return BaseLoc{Root: root, Common: common, Data: data, Spool: filepath.Join(data, "spool")}
}

// projectName is the project's identity in the memory repository: the origin remote's name, else the directory name.
func projectName(loc BaseLoc) string {
	var base string
	if url, ok := gitTry(loc.Root, "remote", "get-url", "origin"); ok && url != "" {
		url = strings.TrimRight(url, "/\\")
		if i := strings.LastIndexAny(url, "/\\:"); i >= 0 {
			url = url[i+1:]
		}
		base = strings.TrimSuffix(url, ".git")
	} else {
		base = filepath.Base(loc.Root)
	}
	return slugify(base, 60)
}

// openProject resolves the project containing cwd and the memory repository it writes to.
func openProject(cwd string) *Project {
	base := locate(cwd)
	return &Project{BaseLoc: base, Slug: projectName(base), Mem: memoryDir()}
}

// excludeLocally adds an entry to .git/info/exclude: it keeps files out of `git status` without touching .gitignore.
func excludeLocally(loc BaseLoc, entry string) {
	file := filepath.Join(loc.Common, "info", "exclude")
	old, _ := readFileTry(file)
	for _, l := range strings.Split(old, "\n") {
		if l == entry {
			return
		}
	}
	mkdirAll(filepath.Dir(file))
	sep := ""
	if old != "" && !strings.HasSuffix(old, "\n") {
		sep = "\n"
	}
	writeFile(file, old+sep+entry+"\n")
}

func identityEnv(cwd string) map[string]string {
	if _, ok := gitTry(cwd, "config", "user.email"); ok {
		return nil
	}
	return map[string]string{
		"GIT_AUTHOR_NAME": "git-memory", "GIT_AUTHOR_EMAIL": "git-memory@localhost",
		"GIT_COMMITTER_NAME": "git-memory", "GIT_COMMITTER_EMAIL": "git-memory@localhost",
	}
}

func isMemoryRepo(mem string) bool { return exists(filepath.Join(mem, ".git")) }

// dirIsEmpty is true for a folder that does not exist or has nothing in it.
func dirIsEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}

// ensureMemoryRepo creates the memory repository on first use. It only ever initialises a folder that is missing or
// empty: anything else is somebody's data, and `git-memory init` (or a clone) is the explicit way to use it.
func ensureMemoryRepo(mem string) {
	if isMemoryRepo(mem) {
		return
	}
	if !dirIsEmpty(mem) {
		fail("%s exists, is not empty and is not a git repository: move it aside, or run `git-memory init` to use it as the memory repository", mem)
	}
	mkdirAll(mem)
	git(mem, "init", "-q", "-b", "main")
}

// withLock serializes writers of the memory repository with a lock directory (mkdir is atomic, also on Windows).
func withLock(mem string, fn func()) {
	if !isMemoryRepo(mem) {
		fail("%s is not a git repository", mem)
	}
	lock := filepath.Join(mem, ".git", "git-memory.lock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		err := os.Mkdir(lock, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			check(err)
		}
		if st, serr := os.Stat(lock); serr == nil && time.Since(st.ModTime()) > 30*time.Second {
			os.RemoveAll(lock)
		}
		if time.Now().After(deadline) {
			fail("could not acquire the memory repository lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer os.RemoveAll(lock)
	fn()
}

// commitPaths commits what changed under the given paths and nothing else: whatever else is staged in the repository
// (the user may be in the middle of their own work there) stays staged.
func commitPaths(mem, message string, paths ...string) bool {
	spec := append([]string{"--"}, paths...)
	git(mem, append([]string{"add", "-A"}, spec...)...)
	if git(mem, append([]string{"status", "--porcelain"}, spec...)...) == "" {
		return false
	}
	gitX(gitCmd{Cwd: mem, Env: identityEnv(mem)}, append([]string{"-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "-m", message}, spec...)...)
	return true
}

// lastCommitAgeSec is how long ago path was last committed (a huge number when never).
func lastCommitAgeSec(mem, path string) int64 {
	ts, ok := gitTry(mem, "log", "-1", "--format=%ct", "--", path)
	if !ok || ts == "" {
		return 1 << 40
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 1 << 40
	}
	return time.Now().Unix() - n
}
