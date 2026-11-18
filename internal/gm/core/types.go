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
	// Mem is the memory repository: <project root>/.git-memory. It is a plain git repo that the user can clone.
	Mem string
	// Base is the memory root folder: the folder of the memory repository that holds the project's memories (memories/,
	// core/, scopes/, sessions/) and global/, slash-separated and relative to the repository's root: "" (the root itself)
	// unless the user set it (see RootFile).
	Base string
	// AutoCommitOn is "autoCommit" in RootFile: every change under the root folder is committed by the hooks.
	AutoCommitOn bool
	// Scope is the part of the project the user works on (a module, say), chosen on this checkout only (see ScopeFile): ""
	// reads every scope. A scope's memories live in <root>/scopes/<scope>/.
	Scope string
}

// Dir is the project's folder in the memory repository: the memory root folder.
func (p *Project) Dir() string { return filepath.Join(p.Mem, filepath.FromSlash(p.DirRel())) }

// DirRel is Dir relative to the repository root, as page references spell it: "" when the root is the repository's top.
func (p *Project) DirRel() string { return p.Base }

// Rel is a path inside the project's folder, relative to the repository root.
func (p *Project) Rel(sub string) string { return InRoot(p.Base, sub) }

// Spec is the project's folder as a git pathspec: "." for the repository's top.
func (p *Project) Spec() string {
	if p.Base == "" {
		return "."
	}
	return p.Base
}

// ownFolders are the folders of the project's folder that hold memories: sessions are local files, kept out of git.
var ownFolders = []string{"memories", "core", "scopes"}

// OwnPaths are the project's own memory folders that exist, as pathspecs relative to the repository root: what a commit
// of the project's memories covers. Empty when there is nothing to commit yet.
func (p *Project) OwnPaths() []string {
	var out []string
	for _, f := range ownFolders {
		if Exists(filepath.Join(p.Dir(), f)) {
			out = append(out, p.Rel(f))
		}
	}
	return out
}

// ScopeRel is the active scope's folder relative to the repository root, "" when no scope is chosen.
func (p *Project) ScopeRel() string {
	if p.Scope == "" {
		return ""
	}
	return p.Rel("scopes/" + p.Scope)
}

// WriteRel is the folder new memories go to: the active scope's, else the project's.
func (p *Project) WriteRel() string {
	if p.Scope != "" {
		return p.ScopeRel()
	}
	return p.DirRel()
}

// ReadPaths are the git pathspecs that cover what this project reads: its own memories, and the global folder. With a
// scope chosen only that scope is read (the others are in the repository, and unread).
func (p *Project) ReadPaths() []string {
	if p.Scope == "" {
		return []string{p.Rel("memories"), p.Rel("core"), p.Rel("scopes"), p.GlobalRel()}
	}
	return []string{p.Rel("memories"), p.Rel("core"), p.ScopeRel(), p.GlobalRel()}
}

// GlobalDir holds the memories that apply to every project.
func (p *Project) GlobalDir() string { return filepath.Join(p.Mem, filepath.FromSlash(p.GlobalRel())) }

// GlobalRel is GlobalDir relative to the repository root.
func (p *Project) GlobalRel() string { return InRoot(p.Base, "global") }

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
