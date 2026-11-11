package gm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// hookCommands lists our hook commands in a Claude settings or Copilot hook file.
func hookCommands(file string) []string {
	var out []string
	for _, entries := range asObj(readJSON(file)["hooks"]) {
		for _, g := range asSlice(entries) {
			candidates := asSlice(asObj(g)["hooks"])
			if candidates == nil {
				candidates = []any{g}
			}
			for _, h := range candidates {
				if c := asObj(h)["command"]; isOurs(c) {
					out = append(out, c.(string))
				}
			}
		}
	}
	return out
}

func ago(t time.Time) string {
	m := int(time.Since(t).Minutes() + 0.5)
	switch {
	case m < 1:
		return "just now"
	case m < 90:
		return fmt.Sprintf("%d min ago", m)
	case m < 2880:
		return fmt.Sprintf("%d h ago", int(float64(m)/60+0.5))
	}
	return fmt.Sprintf("%d days ago", int(float64(m)/1440+0.5))
}

var execLine = regexp.MustCompile(`exec '([^']+)'`)

// health checks that everything is wired up, and that an agent has actually talked to it. It backs `doctor`.
func health(loc *Loc, cfg Config, scopes *Scopes) []HealthRow {
	var out []HealthRow
	add := func(status, label, detail string) { out = append(out, HealthRow{status, label, detail}) }
	rel := func(p string) string {
		if r, err := relPath(loc.Root, p); err == nil && r != "" {
			return slash(r)
		}
		return p
	}

	projectOn := scopes == nil || scopes.ProjectEnabled
	setup := exists(projectConfigPath(loc.Root))
	switch {
	case setup:
		add("ok", "Setup file", rel(projectConfigPath(loc.Root)))
	case projectOn:
		add("warn", "Setup file", "none (memories are personal)")
	default:
		add("info", "Setup file", "none (memories are personal)")
	}

	if projectOn {
		if exists(filepath.Join(loc.Repo, ".git")) {
			add("ok", "Project store", rel(loc.Repo)+"/")
			if hasRemote(loc, cfg) {
				add("ok", "Sync remote", cfg.Remote)
			} else {
				add("warn", "Sync remote", "no remote: memory stays on this machine")
			}
		} else {
			add("error", "Project store", "missing: "+rel(loc.Repo))
		}
	} else {
		add("info", "Project scope", "not set up: share with a team via `init --store <repo>` or `init --branch`")
	}

	if scopes != nil {
		for _, s := range scopes.All {
			if s.Scope == "project" {
				continue
			}
			where := "local only (no remote)"
			if s.Mode != modeLocal {
				where = s.RepoURL
			}
			label := "Scope: " + scopeLabel(s.Scope)
			switch {
			case exists(filepath.Join(s.Repo, ".git")):
				add("ok", label, where)
			case s.Mode == modeLocal:
				add("info", label, "created on first use")
			default:
				add("warn", label, fmt.Sprintf("not cloned yet (%s): run Sync", where))
			}
		}
	}
	claudeCmds, copilotCmds := countCommands(loc.Root)
	total := len(commands)
	switch {
	case claudeCmds+copilotCmds == 0:
		add("info", "Slash commands", "not installed (git-memory init)")
	case claudeCmds == total || copilotCmds == total:
		add("ok", "Slash commands", fmt.Sprintf("Claude Code %d/%d, Copilot %d/%d", claudeCmds, total, copilotCmds, total))
	default:
		add("warn", "Slash commands", fmt.Sprintf("Claude Code %d/%d, Copilot %d/%d", claudeCmds, total, copilotCmds, total))
	}
	a := cfg.AutoRemember
	if a.Triggers {
		add("ok", "Auto-remember", "prompt triggers on → "+a.Scope+" scope")
	} else {
		add("info", "Auto-remember", "off (git-memory auto on)")
	}
	if cfg.SessionScope == "personal" {
		add("ok", "Session pages", "personal (private to you)")
	} else {
		add("ok", "Session pages", "project (shared with the store’s users)")
	}
	add("ok", "Default scope", cfg.DefaultScope+" (where remember writes)")

	launcher := filepath.Join(loc.Data, wrapperName())
	if exists(launcher) {
		add("ok", "Hook runtime", rel(launcher))
	} else {
		add("error", "Hook runtime", "missing: run Set Up")
	}

	claude := []string{}
	for _, f := range []string{".claude/settings.local.json", ".claude/settings.json"} {
		claude = append(claude, hookCommands(filepath.Join(loc.Root, filepath.FromSlash(f)))...)
	}
	copilot := hookCommands(filepath.Join(loc.Root, ".github", "hooks", "git-memory.json"))
	if len(claude) > 0 {
		add("ok", "Claude Code hooks", fmt.Sprintf("%d events", len(claude)))
	} else {
		add("warn", "Claude Code hooks", "not installed")
	}
	if len(copilot) > 0 {
		add("ok", "Copilot hooks", fmt.Sprintf("%d events", len(copilot)))
	} else {
		add("warn", "Copilot hooks", "not installed")
	}

	// The binary hooks end up running lives in the launcher (a sh script on Unix). It can vanish when git-memory is
	// uninstalled or moved.
	if text, ok := readFileTry(launcher); ok {
		if m := execLine.FindStringSubmatch(text); m != nil {
			if exists(m[1]) {
				add("ok", "Binary used by hooks", m[1])
			} else {
				add("error", "Binary used by hooks", fmt.Sprintf("not found: %s (re-run Set Up)", m[1]))
			}
		}
	}

	var newest time.Time
	for _, f := range listSpools(loc) {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	if newest.IsZero() {
		add("info", "Agent activity", "no hook events yet: start an agent session")
	} else {
		add("ok", "Agent activity", "last hook event "+ago(newest))
	}

	if text, ok := readFileTry(filepath.Join(loc.Data, "errors.log")); ok && strings.TrimSpace(text) != "" {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		add("warn", "Hook errors", fmt.Sprintf("%d logged; latest: %s", len(lines), runeSlice(lines[len(lines)-1], 160)))
	} else {
		add("ok", "Hook errors", "none")
	}
	return out
}
