package gm

import "strings"

type StoreConfig struct {
	// Repo is a dedicated memory repo (URL or path). nil: memory lives on an orphan branch of the project itself.
	Repo *string `json:"repo"`
	// Path is the checkout location, relative to the project root.
	Path string `json:"path"`
	// Project is the folder name inside a dedicated repo (projects/<project>). nil: derived from the project.
	Project *string `json:"project"`
}

// ScopeConfig is a named scope this project joins; its pages live in Repo (default: the project's store) under scopes/<name>/.
type ScopeConfig struct {
	Repo *string `json:"repo,omitempty"`
}

type RepoRef struct {
	Repo *string `json:"repo"`
}

type BriefingConfig struct {
	MaxChars       int `json:"maxChars"`
	RecentSessions int `json:"recentSessions"`
	RecentMemories int `json:"recentMemories"`
	MaxDecisions   int `json:"maxDecisions"`
}

type RecallConfig struct {
	Enabled        bool `json:"enabled"`
	MaxChars       int  `json:"maxChars"`
	Limit          int  `json:"limit"`
	MinPromptChars int  `json:"minPromptChars"`
}

type CaptureConfig struct {
	Exclude         []string `json:"exclude"`
	MaxPromptChars  int      `json:"maxPromptChars"`
	MaxOutcomeChars int      `json:"maxOutcomeChars"`
}

// AutoRememberConfig: memories stored straight from the prompt ("remember that we deploy on Tuesdays").
type AutoRememberConfig struct {
	Scope        string `json:"scope"`
	Triggers     bool   `json:"triggers"`
	MaxPerPrompt int    `json:"maxPerPrompt"`
}

type Config struct {
	Store                StoreConfig            `json:"store"`
	Personal             RepoRef                `json:"personal"`
	Global               RepoRef                `json:"global"`
	Scopes               map[string]ScopeConfig `json:"scopes"`
	DefaultScope         string                 `json:"defaultScope"`
	SessionScope         string                 `json:"sessionScope"`
	Branch               string                 `json:"branch"`
	Remote               string                 `json:"remote"`
	AutoPush             bool                   `json:"autoPush"`
	DebounceCommitSec    int                    `json:"debounceCommitSec"`
	Briefing             BriefingConfig         `json:"briefing"`
	Recall               RecallConfig           `json:"recall"`
	Capture              CaptureConfig          `json:"capture"`
	Summarizer           *string                `json:"summarizer"`
	SummarizerTimeoutSec int                    `json:"summarizerTimeoutSec"`
	AutoRemember         AutoRememberConfig     `json:"autoRemember"`
	CLI                  string                 `json:"cli"`
}

var builtinScopes = []string{"personal", "project", "global"}

func isBuiltinScope(s string) bool {
	for _, b := range builtinScopes {
		if b == s {
			return true
		}
	}
	return false
}

// BaseLoc is where the project's repo and local state live.
type BaseLoc struct {
	Root   string
	Common string
	Data   string
	Spool  string
}

// Store modes: how a store's checkout comes to exist.
const (
	modeBranch = "branch" // an orphan branch of the project repo, checked out as a worktree
	modeClone  = "clone"  // a clone of a dedicated memory repo
	modeLocal  = "local"  // a plain local git repo with no remote (the default personal store)
)

// Loc is a BaseLoc plus one resolved memory store (one scope's folder inside a git checkout).
type Loc struct {
	BaseLoc
	// Repo is the git checkout holding the memory: commits, sync and remotes act here.
	Repo string
	// Wt is the directory with this scope's pages (the repo root, or a folder inside a dedicated repo).
	Wt string
	// Project is the folder name used for this project inside dedicated repos; "" while memory lives on a branch.
	Project string
	// Slug is a stable project identity (origin remote name, or directory name) used for the personal scope's folder.
	Slug     string
	External bool
	// IgnoreRel is the checkout path relative to the project root when it is inside it (to git-exclude), else "".
	IgnoreRel string
	Mode      string
	// RepoURL is the remote URL/path the checkout is cloned from (clone mode).
	RepoURL string
	// Scope is which scope this store serves: personal, project, global or a name.
	Scope string
	// LockPath is the lock directory; stores shared between projects lock machine-wide. Default: <data>/lock.
	LockPath string
}

// Meta is page frontmatter. Order is preserved: pages are rewritten in place and diffs should stay small.
type Meta struct {
	keys []string
	vals map[string]metaVal
}

type metaVal struct {
	s      string
	l      []string
	isList bool
}

func newMeta() *Meta { return &Meta{vals: map[string]metaVal{}} }

func (m *Meta) put(k string, v metaVal) *Meta {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
	return m
}

func (m *Meta) Set(k, v string) *Meta { return m.put(k, metaVal{s: v}) }

func (m *Meta) SetList(k string, v []string) *Meta {
	if v == nil {
		v = []string{}
	}
	return m.put(k, metaVal{l: v, isList: true})
}

// Str is the string value of k ("" when missing or a list).
func (m *Meta) Str(k string) string { return m.vals[k].s }

func (m *Meta) List(k string) []string { return m.vals[k].l }

func (m *Meta) Has(k string) bool { _, ok := m.vals[k]; return ok }

func (m *Meta) Delete(k string) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *Meta) Clone() *Meta {
	c := newMeta()
	for _, k := range m.keys {
		c.put(k, m.vals[k])
	}
	return c
}

// Updated is meta.updated || meta.created.
func (m *Meta) Updated() string {
	if s := m.Str("updated"); s != "" {
		return s
	}
	return m.Str("created")
}

type Page struct {
	// Scope is which scope the page was read from.
	Scope string
	// Status: "active" pages bind and are recalled; "superseded" ones are kept for history only.
	Status string
	Path   string
	Meta   *Meta
	Body   string
	Title  string
	Kind   string
	Tier   string
	Tags   []string
}

var kinds = []string{"decision", "procedure", "fact", "note"}

func isKind(k string) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Event is one line of a session spool. Field order is the order in the file.
type Event struct {
	T       int64    `json:"t"`
	Ev      string   `json:"ev"`
	Source  string   `json:"source,omitempty"`
	Branch  string   `json:"branch,omitempty"`
	Head    string   `json:"head,omitempty"`
	Text    string   `json:"text,omitempty"`
	Tool    string   `json:"tool,omitempty"`
	Command string   `json:"command,omitempty"`
	Files   []string `json:"files,omitempty"`
	Failed  bool     `json:"failed,omitempty"`
	Last    string   `json:"last,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type HealthRow struct {
	Status string // ok | warn | error | info
	Label  string
	Detail string
}

type HookPayload = map[string]any

// joinLines is strings.Join(lines, "\n").
func joinLines(lines []string) string { return strings.Join(lines, "\n") }
