package command

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

type healthRow struct{ status, label, detail string } // status: ok | warn | error | info

// countCommands is how many generated slash commands each agent has in the project.
func countCommands(root string, a agent.Agent) (n int) {
	f, ok := commandFormats[a.Name]
	if !ok {
		return 0
	}
	for _, def := range commands {
		if isGenerated(filepath.Join(root, filepath.FromSlash(f.path(def.Name)))) {
			n++
		}
	}
	return n
}

// folderShown is a folder of the memory repository as the doctor writes it: "." for the repository's top.
func folderShown(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
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

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// checkMemory looks at the memory repository, wherever the command runs.
func checkMemory(mem string, add func(status, label, detail string)) {
	switch {
	case core.IsMemoryRepo(mem):
		branch, ok := core.GitTry(mem, "symbolic-ref", "--short", "HEAD")
		if !ok {
			branch = "?"
		}
		state := "no commits yet"
		if n, ok := core.GitTry(mem, "rev-list", "--count", "HEAD"); ok {
			state = plural(atoi(n), "commit", "commits")
		}
		add("ok", "Memory repository", fmt.Sprintf("%s (branch %s, %s)", mem, branch, state))

		if remotes, _ := core.GitTry(mem, "remote"); remotes == "" {
			add("info", "Remote", "none: memory stays on this machine (git-memory remote add origin <url> to back it up)")
		} else {
			name, _, _ := strings.Cut(remotes, "\n")
			url, _ := core.GitTry(mem, "remote", "get-url", name)
			add("ok", "Remote", name+" → "+url)
		}
		if st, _ := core.GitTry(mem, "status", "--porcelain"); st == "" {
			add("ok", "Uncommitted changes", "none")
		} else {
			add("warn", "Uncommitted changes", plural(len(strings.Split(st, "\n")), "file", "files")+" (git-memory status; git-memory add / commit)")
		}
	case core.DirIsEmpty(mem):
		add("warn", "Memory repository", "none at "+mem+": run git-memory init in the folder that should have one")
	default:
		add("error", "Memory repository", mem+" exists, is not empty and is not a git repository (git-memory init, or move it aside)")
	}
}

// checkProject looks at the project containing cwd and how it is wired to the memory repository.
func checkProject(p *core.Project, add func(status, label, detail string)) {
	rel := func(f string) string { r, _ := filepath.Rel(p.Root, f); return core.Slash(r) }

	pages := core.ListProjectPages(p)
	var own, sessions, global int
	for _, pg := range pages {
		switch {
		case pg.Scope == "global":
			global++
		case pg.Kind == "session":
			sessions++
		default:
			own++
		}
	}
	add("ok", "Project folder", folderShown(p.DirRel())+" (the project's memories: memories/, core/, scopes/, sessions/)")
	scopes := p.Scopes()
	switch {
	case p.Scope != "":
		note := "local to this checkout; the project's other scopes are not read"
		if !core.Exists(filepath.Join(p.Dir(), "scopes", p.Scope)) {
			note += "; its folder is made with its first memory"
		}
		add("ok", "Scope", p.Scope+" ("+note+")")
	case len(scopes) > 0:
		add("info", "Scope", "none chosen: every scope is read ("+strings.Join(scopes, ", ")+"; git-memory agent scope <name> chooses one)")
	}
	add("ok", "Write folder", folderShown(p.WriteRel())+" (where new memories go)")
	add("ok", "Read folders", strings.Join(p.ReadPaths(), " "))
	if p.AutoCommitOn {
		add("ok", "Auto commit", "on ("+core.RootFile+"): every change under "+folderShown(p.DirRel())+" is committed when a session starts, a turn ends or a session ends")
	}
	add("ok", "Pages", fmt.Sprintf("%s and %s in this project, %d global", plural(own, "memory", "memories"), plural(sessions, "session", "sessions"), global))

	type hookCount struct {
		agent agent.Agent
		got   int
	}
	var counts []hookCount
	wired := 0 // our hook entries, over all agents
	for _, a := range hook.Agents {
		var ours []string
		for _, f := range a.HookFiles {
			ours = append(ours, agent.HookCommands(filepath.Join(p.Root, filepath.FromSlash(f)))...)
		}
		wired += len(ours)
		counts = append(counts, hookCount{a, len(ours)})
	}
	for _, c := range counts {
		label, want := c.agent.Label+" hooks", len(c.agent.Events)
		switch {
		case c.got == 0 && wired > 0: // the project is wired for another agent: this one was not asked for
			add("info", label, "not set up (git-memory agent init "+c.agent.Name+")")
		case c.got == 0:
			add("warn", label, "not installed (git-memory agent init "+c.agent.Name+")")
		case c.got < want:
			add("warn", label, fmt.Sprintf("%d of %d events (git-memory agent init %s refreshes them)", c.got, want, c.agent.Name))
		default:
			add("ok", label, fmt.Sprintf("%d events", c.got))
		}
	}

	total := len(commands)
	var parts []string
	installed, complete := 0, false
	for _, a := range hook.Agents {
		if _, ok := commandFormats[a.Name]; !ok {
			continue
		}
		n := countCommands(p.Root, a)
		installed += n
		complete = complete || n == total
		parts = append(parts, fmt.Sprintf("%s %d/%d", a.Label, n, total))
	}
	detail := strings.Join(parts, ", ")
	switch {
	case installed == 0:
		add("info", "Slash commands", "not installed (git-memory agent init <agent>)")
	case complete:
		add("ok", "Slash commands", detail)
	default:
		add("warn", "Slash commands", detail)
	}

	launcher := filepath.Join(p.Data, core.WrapperName())
	if !core.Exists(launcher) {
		if wired > 0 { // hooks that call a launcher that is gone
			add("error", "Hook launcher", "missing: run git-memory agent init <agent>")
		} else {
			add("info", "Hook launcher", "not installed (git-memory agent init <agent>)")
		}
	} else {
		add("ok", "Hook launcher", rel(launcher))
		// The binary the launcher runs can vanish when git-memory is uninstalled or moved.
		if text, ok := core.ReadFileTry(launcher); ok {
			if m := execLine.FindStringSubmatch(text); m != nil {
				if core.Exists(m[1]) {
					add("ok", "Binary used by hooks", m[1])
				} else {
					add("error", "Binary used by hooks", fmt.Sprintf("not found: %s (re-run git-memory agent init <agent>)", m[1]))
				}
			}
		}
	}

	var newest time.Time
	for _, f := range hook.ListSpools(p.Spool) {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	if newest.IsZero() {
		add("info", "Agent activity", "no hook events yet: start an agent session in this project")
	} else {
		add("ok", "Agent activity", "last hook event "+ago(newest))
	}

	if text, ok := core.ReadFileTry(filepath.Join(p.Data, "errors.log")); ok && strings.TrimSpace(text) != "" {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		add("warn", "Hook errors", fmt.Sprintf("%d logged; latest: %s", len(lines), core.RuneSlice(lines[len(lines)-1], 160)))
	} else {
		add("ok", "Hook errors", "none")
	}
}

// checkSources lists the memories the agents keep for themselves, where they are found. root is "" outside a project.
func checkSources(root string, add func(status, label, detail string)) {
	found := false
	for _, s := range agentSources(root) {
		if root == "" && s.Name != "copilot-user" {
			continue // the others only exist per project
		}
		if s.Dir == "" {
			add("info", s.Label, "none found")
			continue
		}
		files := s.Files()
		shown := files
		if len(shown) > 5 {
			shown = shown[:5]
		}
		list := strings.Join(shown, ", ")
		if len(files) > len(shown) {
			list += ", …"
		}
		if len(files) == 0 {
			add("info", s.Label, s.Dir+" (empty)")
			continue
		}
		found = true
		add("ok", s.Label, fmt.Sprintf("%s (%s: %s)", s.Dir, plural(len(files), "memory", "memories"), list))
	}
	if found {
		add("info", "Promote", "git-memory promote claude:<file> --to project|global copies one into the memory repository (copilot:<file>, copilot-user:<file> likewise)")
	}
}

func atoi(s string) int {
	n := 0
	fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n
}

// runDoctor prints the version and a health report. It exits 1 when something is broken, unless --report is given
// (what the /memory-status slash command uses, so a warning can never abort the block it injects).
func Doctor(flags core.Flags, version string) int {
	var rows []healthRow
	add := func(status, label, detail string) { rows = append(rows, healthRow{status, label, detail}) }

	gv, _ := core.GitTry("", "--version")
	if gv == "" {
		gv = "git: not found on PATH"
	}
	out(fmt.Sprintf("git-memory %s · %s", version, gv))
	out()

	cwd, _ := os.Getwd()
	var p *core.Project
	projectErr := core.Catch(func() { p = core.OpenProject(cwd) })
	// the memory repository is the .git-memory folder of the project you are in, else of the folder you are in
	mem := core.MemoryDirFor(cwd)
	if p != nil {
		mem = p.Mem
	}
	checkMemory(mem, add)
	base := ""
	if core.IsMemoryRepo(mem) {
		if err := core.Catch(func() { base = core.RootFolder(mem) }); err != nil {
			add("error", "Root folder", err.Error())
			projectErr = err
		} else if base == "" {
			add("ok", "Root folder", "./ (the repository's root; the project's "+core.RootFile+" names another folder)")
		} else if core.DirIsEmpty(filepath.Join(mem, filepath.FromSlash(base))) {
			add("info", "Root folder", base+" (empty or not there yet: it is made with the first memory)")
		} else {
			add("ok", "Root folder", base+" ("+core.RootFile+")")
		}
		add("ok", "Global folder", core.InRoot(base, "global")+" (memories apart from the project's own, read in every scope)")
	}
	if projectErr != nil && !strings.Contains(projectErr.Error(), core.RootFile) {
		add("info", "Project", "not inside a git project: run doctor from a project to check how it is wired")
	} else if projectErr != nil {
		add("info", "Project", "not checked until "+core.RootFile+" is fixed")
	} else {
		checkProject(p, add)
	}

	root := ""
	if p != nil {
		root = p.Root
	}
	checkSources(root, add)

	icons := map[string]string{"ok": "✓", "warn": "!", "error": "✗", "info": "·"}
	bad := false
	for _, r := range rows {
		out(icons[r.status] + " " + r.label + ": " + r.detail)
		bad = bad || r.status == "error"
	}
	if bad && !flags.Has("report") {
		return 1
	}
	return 0
}
