package gm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func defaultConfig() Config {
	return Config{
		// Dedicated memory repo (URL or local path): nil keeps memory on an orphan `branch` of the project repo itself.
		// Checkout path is relative to the project root; one inside the project is added to .git/info/exclude, never .gitignore.
		Store: StoreConfig{Path: ".git-memory"},
		// Your personal store backs the `personal` scope and, by default, `global`. nil: a local-only git repo.
		Personal: RepoRef{},
		Global:   RepoRef{},
		Scopes:   map[string]ScopeConfig{},
		// Memories start private; automatically captured session pages too.
		DefaultScope: "personal",
		SessionScope: "personal",
		Branch:       "memory",
		Remote:       "origin",
		// Commit session pages at most this often on Stop (SessionEnd/PreCompact always commit).
		DebounceCommitSec: 300,
		Briefing:          BriefingConfig{MaxChars: 6000, RecentSessions: 3, RecentMemories: 8, MaxDecisions: 30},
		Recall:            RecallConfig{Enabled: true, MaxChars: 2500, Limit: 4, MinPromptChars: 15},
		Capture: CaptureConfig{
			// Glob patterns (repo-relative) for files that are never recorded.
			Exclude:         []string{"**/.env", "**/.env.*", "**/*.pem", "**/*.key", "**/secrets/**"},
			MaxPromptChars:  1500,
			MaxOutcomeChars: 1500,
		},
		SummarizerTimeoutSec: 45,
		AutoRemember:         AutoRememberConfig{Scope: "personal", Triggers: true, MaxPerPrompt: 5},
		// Command used in injected hints; `init` fills this in.
		CLI: "git-memory",
	}
}

// merge overlays b on a. A null in b means "not set here": lower layers (e.g. the global store) still apply.
func merge(a, b obj) obj {
	out := obj{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v == nil {
			continue
		}
		if bm, ok := v.(map[string]any); ok {
			if am, ok := out[k].(map[string]any); ok {
				out[k] = merge(am, bm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func toObj(v any) obj {
	raw, err := json.Marshal(v)
	check(err)
	var o obj
	check(json.Unmarshal(raw, &o))
	return o
}

func globalDir() string {
	if d := os.Getenv("GIT_MEMORY_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".git-memory")
}

// globalConfigPath holds per-user defaults, e.g. one memory repo for every project.
func globalConfigPath() string { return filepath.Join(globalDir(), "config.json") }

// projectConfigPath is the project's setup file: committed, so teammates only have to run `git-memory init`.
func projectConfigPath(root string) string { return filepath.Join(root, "git-memory.json") }

// localConfigPath holds machine-local overrides (absolute paths, the runtime location); it lives inside .git.
func localConfigPath(data string) string { return filepath.Join(data, "config.json") }

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

// loadConfig layers, lowest to highest precedence: defaults, global, project (git-memory.json), machine-local.
func loadConfig(loc BaseLoc) Config {
	merged := toObj(defaultConfig())
	for _, l := range []struct{ file, base string }{
		{globalConfigPath(), globalDir()},
		{projectConfigPath(loc.Root), loc.Root},
		{localConfigPath(loc.Data), loc.Root},
	} {
		merged = merge(merged, resolveRepos(readJSON(l.file), l.base))
	}
	cfg := defaultConfig()
	raw, _ := json.Marshal(merged)
	_ = json.Unmarshal(raw, &cfg) // a wrongly typed setting keeps its default instead of breaking every hook
	if b := os.Getenv("GIT_MEMORY_BRANCH"); b != "" {
		cfg.Branch = b
	}
	return cfg
}

var (
	urlLike = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`) // https://…, ssh://…, file://…
	scpLike = regexp.MustCompile(`^[\w.-]+@[\w.-]+:`)         // git@github.com:org/repo.git
	winAbs  = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// isLocalPath reports whether v is a filesystem path (as opposed to a URL like git@github.com:org/repo.git).
func isLocalPath(v string) bool {
	if urlLike.MatchString(v) || scpLike.MatchString(v) {
		return false
	}
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~") || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") ||
		v == "." || v == ".." || winAbs.MatchString(v)
}

// portable is what to write into a config file for a repo or path the user typed: URLs stay as they are, local paths
// become relative to baseDir (the directory the file's paths are resolved from). Config files never hold absolute paths.
func portable(value, baseDir string) string {
	cwd, _ := os.Getwd()
	if urlLike.MatchString(value) || scpLike.MatchString(value) {
		return value
	}
	if !isLocalPath(value) && !exists(resolvePath(cwd, value)) {
		return value
	}
	target := resolvePath(cwd, expandHome(value))
	rel, err := relPath(baseDir, target)
	if err != nil {
		return slash(target) // a different drive: cannot be made relative
	}
	rel = slash(rel)
	switch {
	case rel == "":
		return "."
	case strings.HasPrefix(rel, ".."):
		return rel
	}
	return "./" + rel
}

// resolveRepos resolves the relative repo paths of a config layer against the layer's base directory.
func resolveRepos(layer obj, base string) obj {
	fix := func(v any) any {
		if s, ok := v.(string); ok && isLocalPath(s) {
			return resolvePath(base, expandHome(s))
		}
		return v
	}
	out := obj{}
	for k, v := range layer {
		out[k] = v
	}
	for _, key := range []string{"store", "personal", "global"} {
		if m, ok := out[key].(map[string]any); ok {
			if r, has := m["repo"]; has {
				c := obj{}
				for k, v := range m {
					c[k] = v
				}
				c["repo"] = fix(r)
				out[key] = c
			}
		}
	}
	if scopes, ok := out["scopes"].(map[string]any); ok {
		fixed := obj{}
		for name, v := range scopes {
			if m, ok := v.(map[string]any); ok {
				if r, has := m["repo"]; has {
					c := obj{}
					for k, x := range m {
						c[k] = x
					}
					c["repo"] = fix(r)
					fixed[name] = c
					continue
				}
			}
			fixed[name] = v
		}
		out["scopes"] = fixed
	}
	return out
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func writeJSON(file string, data any) {
	mkdirAll(filepath.Dir(file))
	writeFile(file, marshalJSON(data, true)+"\n")
}

// A config target says which layer a setting is written to: the committed project file, your per-user file, or this
// machine+project only.
const (
	targetProject  = "project"
	targetPersonal = "personal"
	targetLocal    = "local"
)

func configFile(loc BaseLoc, target string) string {
	switch target {
	case targetProject:
		return projectConfigPath(loc.Root)
	case targetPersonal:
		return globalConfigPath()
	}
	return localConfigPath(loc.Data)
}

func updateConfig(loc BaseLoc, target string, mutate func(c obj)) string {
	file := configFile(loc, target)
	conf := readJSON(file)
	mutate(conf)
	writeJSON(file, conf)
	return file
}
