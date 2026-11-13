package core

import (
	"path/filepath"
	"strings"
)

// BaseLoc is where the current project's repo and its local, never-committed state live.
type BaseLoc struct {
	Root   string // the project's working tree
	Common string // the project's .git (shared by linked worktrees)
	Data   string // <Common>/git-memory: the launcher, the event spool, errors.log
	Spool  string
}

// Project is the project a hook or `agent init` runs for, plus the memory repository its pages live in.
type Project struct {
	BaseLoc
	// Slug identifies the project inside the memory repository: the origin remote's name, else the directory name.
	Slug string
	// Mem is the memory repository: <project root>/.git-memory. It is a plain git repo that the user can clone.
	Mem string
}

// Dir is this project's folder in the memory repository.
func (p *Project) Dir() string { return filepath.Join(p.Mem, "projects", p.Slug) }

// DirRel is Dir relative to the repository root, as git pathspecs and page references spell it.
func (p *Project) DirRel() string { return "projects/" + p.Slug }

// GlobalDir holds the memories that apply to every project.
func (p *Project) GlobalDir() string { return filepath.Join(p.Mem, "global") }

const GlobalRel = "global"

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

func NewMeta() *Meta { return &Meta{vals: map[string]metaVal{}} }

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

// Updated is meta.updated || meta.created.
func (m *Meta) Updated() string {
	if s := m.Str("updated"); s != "" {
		return s
	}
	return m.Str("created")
}

type Page struct {
	// Scope is "project" or "global": where the page was read from.
	Scope string
	// Status: "active" pages are briefed and recalled; "superseded" ones are kept for history only.
	Status string
	// Path is relative to the folder the page was listed from; Repo is relative to the memory repository root.
	Path  string
	Repo  string
	Meta  *Meta
	Body  string
	Title string
	Kind  string
	Tier  string
	Tags  []string
}

var kinds = []string{"decision", "procedure", "fact", "note"}

func IsKind(k string) bool { return Contains(kinds, k) }

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

type HookPayload = map[string]any

func JoinLines(lines []string) string { return strings.Join(lines, "\n") }

// Clone copies the frontmatter, keeping its key order.
func (m *Meta) Clone() *Meta {
	c := NewMeta()
	for _, k := range m.keys {
		c.put(k, m.vals[k])
	}
	return c
}
