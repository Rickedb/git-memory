package gm

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// The agents keep memories of their own. git-memory finds them where the agents write them, in place and without any
// setting, so `doctor` can list them and `promote` can copy one into the memory repository:
//
//	claude        <claude config>/projects/<this project>/memory   (Claude Code's auto memory for this project)
//	copilot       VS Code Copilot Chat's memory for this workspace
//	copilot-user  VS Code Copilot Chat's user-level memory (follows you across workspaces)
//
// They are only ever read: nothing is written into, moved out of, or linked to those folders.

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeMemoryDir is where Claude Code keeps its auto memory for the project at root: the project path with every
// non-alphanumeric character turned into a dash, under <config dir>/projects/.
func claudeMemoryDir(root string) string {
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".claude")
	}
	return filepath.Join(cfg, "projects", nonAlnum.ReplaceAllString(root, "-"), "memory")
}

// copilotApps are the VS Code flavours whose user data folder is searched.
var copilotApps = []string{"Code", "Code - Insiders", "VSCodium"}

// findFold finds the child of dir named like name, ignoring case (extension folders differ in capitalisation).
func findFold(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// copilotMemoryDirs finds Copilot Chat's memory folders: the user-level one, and the one of the VS Code workspace that
// is this project (matched through the workspace.json VS Code keeps in each workspaceStorage folder).
func copilotMemoryDirs(root string) (user, workspace string) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", ""
	}
	for _, app := range copilotApps {
		userData := filepath.Join(cfg, app, "User")
		if user == "" {
			if d := findFold(filepath.Join(userData, "globalStorage"), "github.copilot-chat"); d != "" {
				if m := findFold(filepath.Join(d, "memory-tool"), "memories"); m != "" {
					user = m
				}
			}
		}
		if workspace != "" {
			continue
		}
		wsRoot := filepath.Join(userData, "workspaceStorage")
		entries, _ := os.ReadDir(wsRoot)
		for _, e := range entries {
			raw := readJSON(filepath.Join(wsRoot, e.Name(), "workspace.json"))
			folder, _ := raw["folder"].(string)
			if !sameFolder(folder, root) {
				continue
			}
			if d := findFold(filepath.Join(wsRoot, e.Name()), "github.copilot-chat"); d != "" {
				if m := findFold(filepath.Join(d, "memory-tool"), "memories"); m != "" {
					workspace = m
					break
				}
			}
		}
	}
	return user, workspace
}

// sameFolder compares a workspace.json `folder` (a file:// URI) with a project root.
func sameFolder(uri, root string) bool {
	if !strings.HasPrefix(uri, "file://") {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	p := filepath.FromSlash(u.Path)
	if len(p) > 2 && p[0] == filepath.Separator && p[2] == ':' { // /c:/Users/x on Windows
		p = p[1:]
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(p), filepath.Clean(root))
	}
	return filepath.Clean(p) == filepath.Clean(root)
}

// kindFromType maps the memory types agents use (Claude: user, feedback, project, reference) onto git-memory's kinds.
// Feedback is guidance on how to work, which is what a decision is here; the rest are facts.
func kindFromType(t string) string {
	switch strings.ToLower(t) {
	case "feedback":
		return "decision"
	case "user", "project", "reference":
		return "fact"
	}
	if isKind(strings.ToLower(t)) {
		return strings.ToLower(t)
	}
	return "note"
}

// agentSource is one place an agent keeps its memories.
type agentSource struct {
	Name  string // claude | copilot | copilot-user: also the prefix `promote` accepts (claude:<file>)
	Label string // for people
	Dir   string // "" when not found
}

var agentSourceNames = []string{"claude", "copilot", "copilot-user"}

func isAgentSource(name string) bool { return contains(agentSourceNames, name) }

// agentSources looks for the agents' memory folders. root is the project's working tree; with "" only the folders that
// do not depend on a project (Copilot's user-level one) can be found.
func agentSources(root string) []agentSource {
	claude := ""
	if root != "" {
		claude = claudeMemoryDir(root)
	}
	user, workspace := copilotMemoryDirs(root)
	list := []agentSource{
		{"claude", "Claude Code memory", claude},
		{"copilot", "Copilot memory (this workspace)", workspace},
		{"copilot-user", "Copilot memory (user)", user},
	}
	for i := range list {
		if list[i].Dir != "" && !exists(list[i].Dir) {
			list[i].Dir = ""
		}
	}
	return list
}

// Files lists the markdown memories in the folder, relative to it (Claude's MEMORY.md is its own index, not a memory).
func (s agentSource) Files() []string {
	var out []string
	if s.Dir == "" {
		return nil
	}
	_ = filepath.WalkDir(s.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			if rel, rerr := filepath.Rel(s.Dir, p); rerr == nil && rel != "MEMORY.md" {
				out = append(out, slash(rel))
			}
		}
		return nil
	})
	return out
}
