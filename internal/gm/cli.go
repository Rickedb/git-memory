package gm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const helpText = `git-memory: serverless AI memory, stored as markdown in git

  init [--store <repo-url|path> | --branch] [--project <name>] [--path <dir>] [--no-claude] [--no-copilot] [--no-commands]
                         set up this project. Plain 'init' is personal-only: it wires the agent hooks (git-excluded)
                         and adds nothing to your project. --store / --branch also opt into a project store shared
                         with your team (writes git-memory.json; commit it). Teammates then run plain 'init'. It also installs
                         slash commands for Claude Code and Copilot: /remember /decide /recall /memories /promote /forget.
  search <query> [--scope S] [--kind K] [--tag T] [--limit N] [--all] [--json]
  remember <text> [--scope S] [--kind decision|procedure|fact|note] [--title T] [--tag a,b] [--core]
  show <ref>             print a page (a 'scope:path' reference, or a path/id/unique substring)
  list [--scope S] [--kind K] [--tag T] [--all]   list pages, newest first (--all includes superseded ones)
  forget <ref>           delete a page (history keeps it)
  promote <ref> [--to <scope>]   widen who sees a memory (default: global)
  move <ref> --to <scope>        move a page to any scope
  supersede <ref> [--by <ref>] [--reason R]   retire a decision: kept in git, but out of the briefing, recall and search
  briefing               print the context a new session would receive
  log [git log args]     history of the default scope's store
  flush                  compile and commit pending session spools
  sync [--quiet]         rebase every memory store on its remote and push
  status
  doctor [--report]      check hooks, launcher, stores and recent agent activity (--report: always exit 0)

Scopes: who sees a memory, and where it is stored. Memories start in 'personal'.
  personal  this project, only you (default)       project  this project, shared via the project store
  global    every project                          <name>   every project that joined <name>
  scopes                          list scopes and where they live
  scopes add <name> [--repo R]    join a named scope (--shared: committed git-memory.json is the default; --personal: all
                                  your projects; --local: this machine only)
  scopes remove <name>
  scopes default <scope>          where 'remember' writes by default (--personal: all your projects, --shared: team default)
  scopes sessions <personal|project>  where automatic session pages go
  scopes personal-repo <repo|none>    give your personal store a remote (none: local-only)
  scopes global-repo <repo|none>      store the global scope in its own repo (none: the personal store)

Auto-remember: type the memory in your prompt and the hook stores it (tagged 'auto'; no model involved):
  remember that we deploy on Tuesdays      decision: use pnpm, never npm      #fact the API listens on 8080
  also: remember: …  #remember …  #decision …  #procedure …   (the /remember-style forms are real slash commands, see init)
  auto                            show the settings
  auto on | off                   turn prompt triggers on or off (default: on)
  auto scope <scope>              where they are saved (default: personal)
  (settings: --local (default) | --personal | --shared, like 'scopes default')

  hook <claude|copilot> <event>   hook entry point (reads the event payload on stdin)
`

func out(a ...any) { fmt.Println(a...) }

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice != 0
}

func readStdinJSON() HookPayload {
	if isTTY(os.Stdin) {
		return HookPayload{}
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return HookPayload{}
	}
	var p HookPayload
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return HookPayload{}
	}
	return p
}

func runHook(agent, event string) {
	payload := readStdinJSON()
	err := catchAll(func() {
		if agent != "claude" && agent != "copilot" {
			fail(`unknown agent "%s"`, agent)
		}
		if o := handleHook(agent, event, payload); o != nil {
			fmt.Println(marshalJSON(o, false))
		}
	})
	if err != nil {
		// Hooks must never break the agent: log and exit cleanly.
		cwd := pickStr(payload, "cwd")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		logError(cwd, err)
	}
}

func tagOf(scope string) string { return "{" + scope + "} " }

func relOrAbs(root, file string) string {
	if r, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(r, "..") {
		return slash(r)
	}
	return file
}

func runInit(flags Flags) {
	cwd, _ := os.Getwd()
	r := initProject(locate(cwd), flags)
	for _, f := range r.Written {
		out("wrote " + f)
	}
	where := relOrAbs(r.Loc.Root, r.Loc.Repo)
	if r.Scopes.ProjectEnabled {
		if r.Loc.External {
			out(fmt.Sprintf("\nproject store: %s → %s/projects/%s", *r.Cfg.Store.Repo, where, r.Loc.Project))
		} else {
			out(fmt.Sprintf("\nproject store: branch %s → %s/", r.Cfg.Branch, where))
		}
		if r.SetupIsNew {
			out("Commit git-memory.json so teammates can run `git-memory init`; the rest stays out of your git history.")
		} else {
			out("Setup read from git-memory.json.")
		}
	} else {
		out("\nMemories are personal by default (nothing was added to this project).")
		out("Share with your team: `git-memory init --store <repo>` or `--branch`. Promote one memory: `git-memory promote <ref>`.")
	}
	for _, w := range r.Warnings {
		out(fmt.Sprintf("note: scope not ready yet (%s)", w))
	}
	if len(r.Commands.Written) > 0 {
		seen := map[string]bool{}
		var names []string
		for _, f := range r.Commands.Written {
			var n string
			if strings.HasSuffix(f, ".prompt.md") {
				n = "/" + strings.TrimSuffix(filepath.Base(f), ".prompt.md")
			} else {
				n = "/" + filepath.Base(filepath.Dir(f))
			}
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		out(fmt.Sprintf("slash commands (Claude Code skills, Copilot prompt files; git-excluded): %s", strings.Join(names, " ")))
	}
	for _, f := range r.Commands.Skipped {
		out(fmt.Sprintf("note: kept your own %s (not generated by git-memory)", f))
	}
}

// targetOf: --shared → committed git-memory.json, --personal → ~/.git-memory/config.json, --local → this machine+project,
// else fallback.
func targetOf(flags Flags, fallback string) string {
	switch {
	case flags.Has("shared"):
		return targetProject
	case flags.Has("personal"):
		return targetPersonal
	case flags.Has("local"):
		return targetLocal
	}
	return fallback
}

func runScopes(scopes *Scopes, cfg Config, pos []string, flags Flags) {
	cwd, _ := os.Getwd()
	base := locate(cwd)
	var sub, arg string
	if len(pos) > 0 {
		sub = pos[0]
	}
	if len(pos) > 1 {
		arg = pos[1]
	}
	saved := func(file string) { out("updated " + relOrAbs(base.Root, file)) }

	switch sub {
	case "":
		pages := listAllPages(scopes)
		if !scopes.ProjectEnabled {
			out(fmt.Sprintf("%-10s %-16s not set up (init --store <repo> or init --branch to share with a team)", "project", scopeLabel("project")))
		}
		for _, s := range scopes.All {
			n := 0
			for _, p := range pages {
				if p.Scope == s.Scope {
					n++
				}
			}
			where := s.RepoURL
			switch s.Mode {
			case modeLocal:
				where = "local only"
			case modeBranch:
				where = "branch " + cfg.Branch
			}
			note := ""
			if !exists(s.Wt) {
				note = "  (not created yet)"
			}
			out(fmt.Sprintf("%-10s %-16s %3d pages  %s%s", s.Scope, scopeLabel(s.Scope), n, where, note))
			out(fmt.Sprintf("%s %s", strings.Repeat(" ", 10), s.Wt))
		}
		out(fmt.Sprintf("\ndefault scope: %s   session pages → %s", cfg.DefaultScope, cfg.SessionScope))
	case "add":
		if arg == "" || !scopeName.MatchString(arg) || isBuiltinScope(arg) {
			fail("usage: scopes add <name> [--repo <url>]  (letters, digits, - and _; not project/user/global)")
		}
		target := targetOf(flags, targetProject)
		// Config files hold no absolute paths: a local repo is written relative to where the file's paths resolve from.
		repo := ""
		if r := flags.S("repo"); r != "" {
			baseDir := base.Root
			if target == targetPersonal {
				baseDir = globalDir()
			}
			repo = portable(r, baseDir)
		}
		saved(updateConfig(base, target, func(c obj) {
			sc := asObj(c["scopes"])
			if repo != "" {
				sc[arg] = obj{"repo": repo}
			} else {
				sc[arg] = obj{}
			}
			c["scopes"] = sc
		}))
	case "remove":
		if arg == "" {
			fail("usage: scopes remove <name>")
		}
		saved(updateConfig(base, targetOf(flags, targetProject), func(c obj) {
			if sc, ok := c["scopes"].(map[string]any); ok {
				delete(sc, arg)
			}
		}))
	case "default":
		if arg == "" {
			fail("usage: scopes default <scope>")
		}
		getScope(scopes, arg)
		saved(updateConfig(base, targetOf(flags, targetLocal), func(c obj) { c["defaultScope"] = arg }))
	case "sessions":
		if arg != "personal" && arg != "project" {
			fail("usage: scopes sessions <personal|project>")
		}
		saved(updateConfig(base, targetOf(flags, targetLocal), func(c obj) { c["sessionScope"] = arg }))
	case "personal-repo", "global-repo":
		if arg == "" {
			fail("usage: scopes %s <repo|none>", sub)
		}
		key := "personal"
		if sub == "global-repo" {
			key = "global"
		}
		saved(updateConfig(base, targetPersonal, func(c obj) {
			m := asObj(c[key])
			if arg == "none" {
				m["repo"] = nil
			} else {
				m["repo"] = portable(arg, globalDir())
			}
			c[key] = m
		}))
		out("Run `git-memory init` (or `sync`) to create the checkout.")
	default:
		fail(`unknown scopes command "%s"`, sub)
	}
}

func runAuto(scopes *Scopes, cfg Config, pos []string, flags Flags) {
	cwd, _ := os.Getwd()
	base := locate(cwd)
	var sub, arg string
	if len(pos) > 0 {
		sub = pos[0]
	}
	if len(pos) > 1 {
		arg = pos[1]
	}
	set := func(patch func(a obj)) {
		file := updateConfig(base, targetOf(flags, targetLocal), func(c obj) {
			a := asObj(c["autoRemember"])
			patch(a)
			c["autoRemember"] = a
		})
		out("updated " + relOrAbs(base.Root, file))
	}
	a := cfg.AutoRemember

	switch sub {
	case "", "status":
		state := "off"
		if a.Triggers {
			state = "on"
		}
		out(fmt.Sprintf(`prompt triggers %s: a line like "remember that …" or "decision: …" in a prompt is saved to the %s scope, tagged 'auto'`, state, a.Scope))
		out("review what was saved with: list --tag auto")
	case "on", "off":
		set(func(x obj) { x["triggers"] = sub == "on" })
	case "scope":
		getScope(scopes, arg)
		set(func(x obj) { x["scope"] = arg })
	default:
		fail(`unknown auto command "%s" (try: auto on | off | scope <scope>)`, sub)
	}
}

func runCommand(cmd string, flags Flags, pos []string) int {
	cwd, _ := os.Getwd()
	loc, cfg, scopes := openAll(cwd)
	sessionLoc, ok := scopes.ByID[cfg.SessionScope]
	if !ok {
		sessionLoc = getScope(scopes, "personal")
	}
	prepare := func() {
		if scopes.ProjectEnabled {
			ensureStore(loc, cfg)
		}
	}
	arg0 := ""
	if len(pos) > 0 {
		arg0 = pos[0]
	}
	exit := 0

	switch cmd {
	case "search":
		prepare()
		hits := search(strings.Join(pos, " "), searchOptions{
			Limit: atoiOr(flags.S("limit"), 8), Scope: flags.S("scope"), All: flags.Has("all"),
			Kind: flags.S("kind"), Tag: flags.S("tag"), Pages: listAllPages(scopes),
		})
		if flags.Has("json") {
			if hits == nil {
				hits = []searchHit{}
			}
			out(marshalJSON(hits, true))
			return 0
		}
		if len(hits) == 0 {
			out("no matches")
			return 0
		}
		for _, h := range hits {
			sup := ""
			if h.Status != "active" {
				sup = ", superseded"
			}
			out(fmt.Sprintf("%s  [%s%s%s] %s\n    %s", h.Ref, tagOf(h.Scope), h.Kind, sup, h.Title, h.Snippet))
		}
	case "remember":
		scope := flags.S("scope")
		if scope == "" {
			scope = cfg.DefaultScope
		}
		var tags []string
		if tg := flags.S("tag"); tg != "" {
			for _, s := range strings.Split(tg, ",") {
				tags = append(tags, strings.TrimSpace(s))
			}
		}
		text := strings.Join(pos, " ")
		if text == "" && !isTTY(os.Stdin) {
			b, _ := io.ReadAll(os.Stdin)
			text = string(b)
		}
		rel := remember(getScope(scopes, scope), cfg, rememberInput{Text: text, Kind: flags.S("kind"), Title: flags.S("title"), Tags: tags, Core: flags.Has("core")})
		out("remembered " + refOf(scope, rel))
	case "show":
		r := resolvePage(scopes, cfg, arg0)
		out(readFile(filepath.Join(r.Store.Wt, filepath.FromSlash(r.Page.Path))))
	case "list":
		prepare()
		pages := filterPages(listAllPages(scopes), func(p *Page) bool {
			return (flags.S("kind") == "" || p.Kind == flags.S("kind")) &&
				(flags.S("scope") == "" || p.Scope == flags.S("scope")) &&
				(flags.S("tag") == "" || contains(p.Tags, flags.S("tag"))) &&
				(flags.Has("all") || isActive(p))
		})
		sortPagesNewest(pages)
		for _, p := range pages {
			marks := ""
			if p.Tier == "core" {
				marks += ", pinned"
			}
			if !isActive(p) {
				marks += ", superseded"
			}
			out(fmt.Sprintf("%s  [%s%s%s] %s  %s", runeSlice(p.Meta.Str("updated"), 10), tagOf(p.Scope), p.Kind, marks, p.Title, pageRef(p)))
		}
	case "forget":
		out("forgot " + forget(scopes, cfg, arg0))
	case "supersede":
		r := supersede(scopes, cfg, arg0, flags.S("by"), flags.S("reason"))
		line := "superseded " + r.Old
		if r.By != "" {
			line += " by " + r.By
			if r.Inherited {
				line += " (the replacement is now pinned)"
			}
		}
		out(line)
	case "promote":
		to := flags.S("to")
		if to == "" {
			to = "global"
		}
		out("promoted to " + move(scopes, cfg, arg0, to))
	case "move":
		to := flags.S("to")
		if to == "" {
			fail("usage: move <ref> --to <scope>")
		}
		out("moved to " + move(scopes, cfg, arg0, to))
	case "briefing":
		prepare()
		b := buildBriefing(listAllPages(scopes), cfg)
		if b == "" {
			b = "(memory is empty)"
		}
		out(b)
	case "log":
		store, ok := scopes.ByID[cfg.DefaultScope]
		if !ok {
			store = sessionLoc
		}
		ensureStore(store, cfg)
		args := []string{"log", "--oneline"}
		if len(pos) > 0 {
			args = append(args, pos...)
		} else {
			args = append(args, "-n", "20")
		}
		args = append(args, "--", ".")
		if o, ok := gitTry(store.Wt, args...); ok {
			out(o)
		} else {
			out("(no history yet)")
		}
	case "flush":
		n := 0
		withLock(sessionLoc, func() {
			ensureStore(sessionLoc, cfg)
			n = flushStale(sessionLoc, cfg, "")
			commitAll(sessionLoc, "memory: flush sessions")
		})
		out(fmt.Sprintf("compiled %d session(s)", n))
	case "sync":
		results := syncAll(scopes, cfg)
		allOK := true
		for _, r := range results {
			if !flags.Has("quiet") {
				out(strings.Join(r.Scopes, ", ") + ": " + r.Message)
			}
			allOK = allOK && r.OK
		}
		if !allOK {
			exit = 1
		}
	case "scopes":
		runScopes(scopes, cfg, pos, flags)
	case "auto":
		runAuto(scopes, cfg, pos, flags)
	case "doctor":
		icons := map[string]string{"ok": "✓", "warn": "!", "error": "✗", "info": "·"}
		rows := health(loc, cfg, scopes)
		bad := false
		for _, r := range rows {
			line := icons[r.Status] + " " + r.Label
			if r.Detail != "" {
				line += ": " + r.Detail
			}
			out(line)
			bad = bad || r.Status == "error"
		}
		if bad && !flags.Has("report") {
			exit = 1
		}
	case "status":
		prepare()
		pages := listAllPages(scopes)
		if scopes.ProjectEnabled {
			commits, ok := gitTry(loc.Wt, "rev-list", "--count", "HEAD", "--", ".")
			if !ok {
				commits = "0"
			}
			if loc.External {
				out(fmt.Sprintf("project store: %s → projects/%s  (%s commits)", loc.RepoURL, loc.Project, commits))
			} else {
				out(fmt.Sprintf("project store: branch %s  (%s commits)", cfg.Branch, commits))
			}
			out("checkout:      " + loc.Repo)
		} else {
			out("project store: none (memories are personal)")
		}
		sessions := len(filterPages(pages, func(p *Page) bool { return p.Kind == "session" }))
		out(fmt.Sprintf("pages:    %d (%d sessions)", len(pages), sessions))
		for _, s := range scopes.All {
			n := len(filterPages(pages, func(p *Page) bool { return p.Scope == s.Scope }))
			out(fmt.Sprintf("  %-10s %d", s.Scope, n))
		}
		out(fmt.Sprintf("spools:   %d pending/recent", len(listSpools(loc))))
		out(fmt.Sprintf("defaults: remember → %s, sessions → %s", cfg.DefaultScope, cfg.SessionScope))
	default:
		out(helpText)
		return 1
	}
	return exit
}

// Version is set at release time: go build -ldflags "-X git-memory/internal/gm.Version=v1.2.3".
var Version = "dev"

// Main runs the CLI and returns the process exit code.
func Main(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(failure); ok {
				fmt.Fprintf(os.Stderr, "git-memory: %s\n", f.msg)
			} else {
				fmt.Fprintf(os.Stderr, "git-memory: internal error: %v\n", r)
			}
			code = 1
		}
	}()
	if len(args) == 0 {
		out(helpText)
		return 0
	}
	cmd, rest := args[0], args[1:]
	if cmd == "version" || cmd == "--version" {
		out("git-memory " + Version)
		return 0
	}
	flags, pos := parseArgs(rest)

	switch {
	case cmd == "hook":
		var agent, event string
		if len(pos) > 0 {
			agent = pos[0]
		}
		if len(pos) > 1 {
			event = pos[1]
		}
		runHook(agent, event)
		return 0
	case cmd == "help" || flags.Has("help"):
		out(helpText)
		return 0
	case cmd == "init":
		runInit(flags)
		return 0
	}
	return runCommand(cmd, flags, pos)
}
