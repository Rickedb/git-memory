package gm

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

// A scope decides who sees a memory, and where it is stored. Memories start in `personal`.
//
//	personal  this project, only you: your personal store, folder private/projects/<project>   (the default)
//	project   this project, shared with whoever uses the project's store (opt-in: init --store / --branch)
//	global    every project: global/ in the personal store (or in `global.repo`, e.g. an org repo)
//	<name>    every project that joins <name>: scopes/<name>/ in its own repo, or the project's store
//
// Each scope is one folder in a git checkout; several scopes can share a checkout.
type Scopes struct {
	// Project is the project's own location (root, spool, ...). Its store is only part of All when ProjectEnabled.
	Project        *Loc
	ProjectEnabled bool
	All            []*Loc
	ByID           map[string]*Loc
}

var scopeName = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9_-]*$`)

var scopeLabels = map[string]string{"personal": "Personal", "project": "Project (shared)", "global": "All projects"}

func scopeLabel(id string) string {
	if l, ok := scopeLabels[id]; ok {
		return l
	}
	return id
}

// storeDir: one clone per distinct repo URL on this machine, shared by every project that uses it.
func storeDir(url string) string {
	trimmed := strings.TrimRight(url, "/\\")
	if i := strings.LastIndexAny(trimmed, "/\\:"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	name := slugify(strings.TrimSuffix(trimmed, ".git"), 60)
	sum := sha1.Sum([]byte(url))
	return filepath.Join(globalDir(), "stores", name+"-"+hex.EncodeToString(sum[:])[:8])
}

// projectEnabled: the project scope exists only when the project opted into a shared store: a `store` section in
// git-memory.json (or the machine-local config), or a store repo set in your global config.
func projectEnabled(base BaseLoc, cfg Config) bool {
	has := func(file string) bool { _, ok := readJSON(file)["store"]; return ok }
	return (cfg.Store.Repo != nil && *cfg.Store.Repo != "") || has(projectConfigPath(base.Root)) || has(localConfigPath(base.Data))
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func resolveScopes(base BaseLoc, cfg Config) *Scopes {
	project := attach(base, cfg)
	enabled := projectEnabled(base, cfg)
	personal := cfg.Personal.Repo

	make := func(scope string, url *string, folder string) *Loc {
		l := *project
		var repo, mode, lockPath string
		switch {
		case url == nil:
			repo, mode = filepath.Join(globalDir(), "personal"), modeLocal
		case samePtr(url, cfg.Store.Repo):
			repo, mode, lockPath = project.Repo, modeClone, project.LockPath // same repo as the project's store: reuse its checkout
		default:
			repo, mode = storeDir(*url), modeClone
		}
		l.Repo, l.Wt, l.Project = repo, filepath.Join(repo, filepath.FromSlash(folder)), ""
		l.External, l.IgnoreRel, l.Mode, l.Scope = mode == modeClone, "", mode, scope
		l.RepoURL = ""
		if url != nil {
			l.RepoURL = *url
		}
		l.LockPath = lockPath
		if lockPath == "" && repo != project.Repo {
			l.LockPath = filepath.Join(globalDir(), "locks", filepath.Base(repo))
		}
		return &l
	}

	var all []*Loc
	if enabled {
		all = append(all, project)
	}
	all = append(all, make("personal", personal, "private/projects/"+project.Slug))
	globalRepo := cfg.Global.Repo
	if globalRepo == nil {
		globalRepo = personal
	}
	all = append(all, make("global", globalRepo, "global"))
	for _, name := range sortedKeys(cfg.Scopes) {
		if !scopeName.MatchString(name) || isBuiltinScope(name) {
			continue
		}
		repo := cfg.Scopes[name].Repo
		if repo == nil {
			repo = cfg.Store.Repo
		}
		if repo == nil {
			repo = personal
		}
		all = append(all, make(name, repo, "scopes/"+name))
	}
	byID := map[string]*Loc{}
	for _, l := range all {
		byID[l.Scope] = l
	}
	return &Scopes{Project: project, ProjectEnabled: enabled, All: all, ByID: byID}
}

// openAll loads config and resolves the scopes for the repo containing cwd.
func openAll(cwd string) (*Loc, Config, *Scopes) {
	base := locate(cwd)
	cfg := loadConfig(base)
	scopes := resolveScopes(base, cfg)
	return scopes.Project, cfg, scopes
}

func (s *Scopes) ids() []string {
	var out []string
	for _, l := range s.All {
		out = append(out, l.Scope)
	}
	return out
}

func getScope(s *Scopes, id string) *Loc {
	if l, ok := s.ByID[id]; ok {
		return l
	}
	if id == "project" {
		fail("the project scope has no shared store yet: run `git-memory init --store <repo>` (a shared memory repo) or `git-memory init --branch` (a memory branch of this repository)")
	}
	fail(`unknown scope "%s"; available: %s`, id, strings.Join(s.ids(), ", "))
	return nil
}

var refPrefix = regexp.MustCompile(`^([^:/\\]+):(.+)$`)

// parseRef splits `scope:path` (when the prefix is a scope name) from a bare path.
func parseRef(s *Scopes, ref string) (scope, rel string) {
	if m := refPrefix.FindStringSubmatch(ref); m != nil {
		if _, ok := s.ByID[m[1]]; ok || isBuiltinScope(m[1]) {
			return m[1], m[2]
		}
	}
	return "", ref
}

// listAllPages reads the pages of every scope whose checkout exists. Reading never clones or creates anything.
func listAllPages(s *Scopes) []*Page {
	var out []*Page
	for _, l := range s.All {
		if exists(l.Wt) {
			out = append(out, listPages(l.Wt, l.Scope)...)
		}
	}
	return out
}

// tryEnsure creates a scope's checkout if needed; it returns an error message instead of failing (e.g. offline clone).
func tryEnsure(loc *Loc, cfg Config) string {
	if err := Catch(func() { ensureStore(loc, cfg) }); err != nil {
		return err.Error()
	}
	return ""
}

type checkout struct {
	Loc    *Loc
	Scopes []string
}

// checkouts returns one entry per distinct checkout, with the scopes that live in it.
func checkouts(s *Scopes) []*checkout {
	var out []*checkout
	byRepo := map[string]*checkout{}
	for _, l := range s.All {
		if e, ok := byRepo[l.Repo]; ok {
			e.Scopes = append(e.Scopes, l.Scope)
			continue
		}
		e := &checkout{Loc: l, Scopes: []string{l.Scope}}
		byRepo[l.Repo] = e
		out = append(out, e)
	}
	return out
}

type scopeSync struct {
	Scopes  []string
	OK      bool
	Message string
}

// syncAll syncs every checkout. Clone-mode stores that are not on this machine yet are cloned first (this is what makes
// a new machine pick up its personal and shared scopes). Stores without a remote are reported, not failed.
func syncAll(s *Scopes, cfg Config) []scopeSync {
	var results []scopeSync
	for _, c := range checkouts(s) {
		loc := c.Loc
		if !exists(filepath.Join(loc.Repo, ".git")) {
			if loc.Mode != modeClone {
				continue // local stores appear on first write
			}
			if msg := tryEnsure(loc, cfg); msg != "" {
				results = append(results, scopeSync{c.Scopes, false, msg})
				continue
			}
		}
		if !hasRemote(loc, cfg) {
			results = append(results, scopeSync{c.Scopes, true, "local only (no remote)"})
			continue
		}
		r := syncStore(loc, cfg)
		results = append(results, scopeSync{c.Scopes, r.OK, r.Message})
	}
	return results
}
