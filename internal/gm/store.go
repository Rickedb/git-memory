package gm

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Every machine derives the same root commit (empty tree, fixed identity and date), so independently initialised
// clones share history and can sync.
var rootEnv = map[string]string{
	"GIT_AUTHOR_NAME": "git-memory", "GIT_AUTHOR_EMAIL": "git-memory@localhost",
	"GIT_COMMITTER_NAME": "git-memory", "GIT_COMMITTER_EMAIL": "git-memory@localhost",
	"GIT_AUTHOR_DATE": "2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE": "2000-01-01T00:00:00Z",
}

// locate finds where the project's repo and local state live.
func locate(cwd string) BaseLoc {
	root := filepath.FromSlash(git(cwd, "rev-parse", "--show-toplevel"))
	common := resolvePath(root, filepath.FromSlash(git(root, "rev-parse", "--git-common-dir")))
	data := filepath.Join(common, "git-memory")
	return BaseLoc{Root: root, Common: common, Data: data, Spool: filepath.Join(data, "spool")}
}

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

// attach resolves the configured store: where the checkout is and where this project's pages live.
func attach(loc BaseLoc, cfg Config) *Loc {
	s := cfg.Store
	repo := resolvePath(loc.Root, expandHome(s.Path))
	// `slug` identifies the project for the personal scope and must not change when the shared store picks a custom
	// folder name (`store.project`), or personal memories would be orphaned. It comes from the repo itself.
	slug := projectName(loc)
	l := &Loc{BaseLoc: loc, Repo: repo, Wt: repo, Slug: slug, Scope: "project", Mode: modeBranch}
	if s.Repo != nil && *s.Repo != "" {
		name := slug
		if s.Project != nil && *s.Project != "" {
			name = *s.Project
		}
		l.Project = slugify(name, 60)
		l.Wt = filepath.Join(repo, "projects", l.Project)
		l.External = true
		l.Mode = modeClone
		l.RepoURL = *s.Repo
	}
	if rel, err := relPath(loc.Root, repo); err == nil && rel != "" && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		l.IgnoreRel = slash(rel)
	}
	return l
}

// openProject loads config and resolves the store for the repo containing cwd.
func openProject(cwd string) (*Loc, Config) {
	base := locate(cwd)
	cfg := loadConfig(base)
	return attach(base, cfg), cfg
}

// excludeLocally uses .git/info/exclude: it keeps files out of `git status` without touching .gitignore.
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

func refExists(cwd, ref string) bool {
	_, ok := gitTry(cwd, "rev-parse", "--verify", "--quiet", ref)
	return ok
}

// ensureStore makes sure the memory checkout exists: a clone of the dedicated repo, or an orphan-branch worktree.
func ensureStore(loc *Loc, cfg Config) {
	if loc.IgnoreRel != "" {
		excludeLocally(loc.BaseLoc, "/"+loc.IgnoreRel+"/")
	}
	switch loc.Mode {
	case modeClone:
		ensureClone(loc)
		return
	case modeLocal:
		ensureLocal(loc)
		return
	}
	if exists(filepath.Join(loc.Repo, ".git")) {
		return
	}
	mkdirAll(filepath.Dir(loc.Repo))
	root, br := loc.Root, cfg.Branch
	git(root, "worktree", "prune")
	if !refExists(root, "refs/heads/"+br) {
		if refExists(root, "refs/remotes/"+cfg.Remote+"/"+br) {
			git(root, "branch", "--track", br, cfg.Remote+"/"+br)
		} else {
			empty := ""
			tree := gitX(gitCmd{Cwd: root, Input: &empty}, "mktree")
			commit := gitX(gitCmd{Cwd: root, Env: rootEnv}, "commit-tree", tree, "-m", "git-memory: init")
			git(root, "branch", br, commit)
		}
	}
	if entries, err := os.ReadDir(loc.Repo); err == nil {
		if len(entries) > 0 {
			fail("%s exists, is not empty and is not a git-memory checkout", loc.Repo)
		}
		os.RemoveAll(loc.Repo)
	}
	git(root, "worktree", "add", "--force", loc.Repo, br)
}

// ensureLocal creates a plain local git repo with no remote: the zero-configuration personal store.
func ensureLocal(loc *Loc) {
	if !exists(filepath.Join(loc.Repo, ".git")) {
		mkdirAll(loc.Repo)
		git(loc.Repo, "init", "-q", "-b", "main")
	}
	mkdirAll(loc.Wt)
}

func ensureClone(loc *Loc) {
	repo := loc.RepoURL
	if repo == "" {
		fail("no repository configured for the %s scope", loc.Scope)
	}
	if !exists(filepath.Join(loc.Repo, ".git")) {
		mkdirAll(filepath.Dir(loc.Repo))
		source := repo
		if strings.HasPrefix(repo, "~/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") {
			source = resolvePath(loc.Root, expandHome(repo))
		}
		git(loc.Root, "clone", "--quiet", source, loc.Repo)
	}
	mkdirAll(loc.Wt)
}

// withLock serializes writers of one store with a lock directory (mkdir is atomic, also on Windows).
func withLock(loc *Loc, fn func()) {
	lock := loc.LockPath
	if lock == "" {
		lock = filepath.Join(loc.Data, "lock")
	}
	mkdirAll(filepath.Dir(lock))
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
			fail("git-memory: could not acquire store lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer os.RemoveAll(lock)
	fn()
}

func pathspec(loc *Loc) string {
	rel, err := filepath.Rel(loc.Repo, loc.Wt)
	if err != nil || rel == "" {
		return "."
	}
	return slash(rel)
}

// commitAll commits changes under this project's pages (other projects' folders in a shared repo are left alone).
func commitAll(loc *Loc, message string) bool {
	git(loc.Repo, "add", "-A", "--", pathspec(loc))
	if git(loc.Repo, "status", "--porcelain", "--", pathspec(loc)) == "" {
		return false
	}
	gitX(gitCmd{Cwd: loc.Repo, Env: identityEnv(loc.Repo)}, "-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "-m", message)
	return true
}

// lastCommitAgeSec is how long ago this store's pages were last committed (a huge number when never).
func lastCommitAgeSec(loc *Loc) int64 {
	ts, ok := gitTry(loc.Repo, "log", "-1", "--format=%ct", "--", pathspec(loc))
	if !ok || ts == "" {
		return 1 << 40
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 1 << 40
	}
	return time.Now().Unix() - n
}

func hasRemote(loc *Loc, cfg Config) bool {
	_, ok := gitTry(loc.Repo, "remote", "get-url", cfg.Remote)
	return ok
}

type syncResult struct {
	OK      bool
	Pulled  bool
	Message string
}

// syncStore rebases local memory commits on the remote branch, then pushes.
func syncStore(loc *Loc, cfg Config) syncResult {
	ensureStore(loc, cfg)
	if !hasRemote(loc, cfg) {
		return syncResult{Message: `no remote named "` + cfg.Remote + `"`}
	}
	cwd := loc.Repo
	branch, ok := gitTry(cwd, "symbolic-ref", "--short", "HEAD")
	if !ok {
		branch = cfg.Branch
	}
	remoteRef := "refs/remotes/" + cfg.Remote + "/" + branch
	_, fetched := gitTry(cwd, "fetch", "--quiet", cfg.Remote, branch+":"+remoteRef)
	pulled := false
	if fetched && refExists(cwd, remoteRef) {
		before, hasHead := gitTry(cwd, "rev-parse", "HEAD")
		if !hasHead {
			git(cwd, "reset", "--hard", "--quiet", remoteRef) // fresh clone of an empty repo that has since received commits
		} else if _, ok := gitXTry(gitCmd{Cwd: cwd, Env: identityEnv(cwd)}, "-c", "commit.gpgsign=false", "rebase", remoteRef); !ok {
			gitTry(cwd, "rebase", "--abort")
			return syncResult{Message: "rebase onto remote failed (conflict); resolve in " + cwd}
		}
		pulled = git(cwd, "rev-parse", "HEAD") != before
	}
	if !refExists(cwd, "HEAD") {
		return syncResult{OK: true, Pulled: pulled, Message: "nothing to sync yet"}
	}
	if _, ok := gitTry(cwd, "push", "--quiet", cfg.Remote, branch+":"+branch); !ok {
		return syncResult{Pulled: pulled, Message: "push failed (check remote permissions)"}
	}
	return syncResult{OK: true, Pulled: pulled, Message: "synced"}
}
