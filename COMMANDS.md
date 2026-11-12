# git-memory: commands and how to use them

`git-memory` is `git`, pointed at one repository: your **memory repository**, `~/.git-memory`. This page lists the commands git-memory has of its own, then walks through the everyday jobs. It does not list git's commands: those are git's, and `git help` or `git-memory help` covers them.

## The commands

### `git-memory <git command> [args…]`

Runs `git <git command> [args…]` **inside the memory repository**, from whatever directory you are in. Arguments, flags, stdin, the terminal and the exit code are git's own.

```bash
git-memory status
git-memory add .
git-memory commit -m "memory: we deploy on Tuesdays"
git-memory log --oneline -n 10
git-memory grep -n -i "retry"
git-memory push
```

If the memory repository does not exist yet, the first command creates it (`git init`), but only when `~/.git-memory` is missing or empty. If the folder holds something else, git-memory stops and tells you, instead of initialising over your files.

Paths are relative to the memory repository, not to your current directory: `git-memory add global` stages `~/.git-memory/global`.

### `git-memory clone <url>`

Clones an existing memory repository **into** `~/.git-memory` (you do not give a destination). Options before the URL go to `git clone`.

```bash
git-memory clone git@github.com:me/memory.git
git-memory clone --depth 1 https://example.com/me/memory.git
```

It refuses if `~/.git-memory` already exists and is not empty. Move that folder aside first, or work with it directly (`git-memory pull`).

### `git-memory init`

`git init` for the memory repository, on purpose. Use it when `~/.git-memory` already contains files you want to keep and track. Normally you never need it: the repository is created the first time it is needed.

```bash
git-memory init                                  # ~/.git-memory now is a repository, files untouched
git-memory add . && git-memory commit -m "start tracking"
```

### `git-memory doctor [--report]`

Prints the **version** and a **health report**: where the memory repository is, how it is doing, and, when you run it inside a project, whether that project is wired and what has happened there. It is the first thing to run when something looks off, and the place to look up where this project's memory lives.

```
$ git-memory doctor
git-memory 0.0.1 · git version 2.56.0

✓ Memory repository: /home/you/.git-memory (branch main, 2 commits)
✓ Remote: origin → git@github.com:me/memory.git
✓ Uncommitted changes: none
✓ Global folder: global (memories that apply to every project)
✓ Project folder: projects/shop
✓ Pages: 1 memory and 1 session in this project, 0 global
✓ Claude Code hooks: 7 events
✓ Copilot hooks: 6 events
✓ Slash commands: Claude Code 6/6, Copilot 6/6
✓ Hook launcher: .git/git-memory/gm
✓ Binary used by hooks: /home/you/.local/bin/git-memory
✓ Agent activity: last hook event just now
✓ Hook errors: none
✓ Claude Code memory: /home/you/.claude/projects/-home-you-shop/memory (2 memories: feedback_tailwind.md, user_role.md)
· Copilot memory (this workspace): none found
· Copilot memory (user): none found
· Promote: git-memory promote claude:<file> --to project|global copies one into the memory repository (copilot:<file>, copilot-user:<file> likewise)
```

| Mark | Meaning |
| --- | --- |
| `✓` | fine |
| `·` | nothing to report yet (no remote, repository not created, no agent activity) |
| `!` | worth a look: uncommitted changes, hooks not installed, errors logged |
| `✗` | broken: the memory folder is not a repository, the hooks point at a binary that no longer exists |

The last rows list the **agents' own memories** it found, in place and read-only: Claude Code's auto memory for this project (`~/.claude/projects/<project>/memory`, or under `$CLAUDE_CONFIG_DIR`), VS Code Copilot Chat's memory for this workspace, and its user-level memory. Each shows its folder, how many memories it holds and the first file names (Claude's own `MEMORY.md` index is not counted). Those names are what `git-memory promote` takes.

It exits with 1 when any row is `✗`, so scripts can rely on it; `--report` makes it always exit 0 (the `/memories` slash command uses that, so a warning can never abort it). Outside a git project it still reports the memory repository and says `· Project: not inside a git project`. The project's folder is named after its `origin` remote (`git@github.com:acme/shop.git` → `projects/shop`), or after its directory when there is no remote. It never creates anything.

### `git-memory promote <source>… --to project|global`

Brings a memory into the memory repository, into **this project's folder** (`--to project`) or the **global** one that applies to every project (`--to global`). `<source>` can be:

| Source | Example | What happens |
| --- | --- | --- |
| a page already in the memory repository | `projects/shop/memories/decision/use-rest.md` or just `use-rest` | **moved** to the other folder, committed as a rename so history follows it |
| one of Claude Code's own memories | `claude:feedback_tailwind.md` | **copied**; the original stays where Claude keeps it |
| one of Copilot's own memories | `copilot:build.md` (this workspace), `copilot-user:tabs.md` (yours) | **copied** |
| any markdown file | `./notes/retries.md` | **copied** |

```bash
git-memory doctor                                            # lists the agents' memories it found
git-memory promote claude:feedback_tailwind.md --to project  # this project only
git-memory promote claude:tailwind --to global               # a unique part of the name is enough
git-memory promote copilot-user:tabs.md --to global
git-memory promote ./notes/retries.md --to project
git-memory promote use-rest --to global                      # a page of the repository: moved
git-memory promote a.md b.md c.md --to project               # several at once
```

Each promotion prints what it did (`promoted claude:feedback_tailwind.md → projects/shop/memories/decision/use-tailwind.md`) and commits it.

- **Copies are converted.** Claude's `type` becomes a kind (`feedback` → `decision`, since it is guidance on how to work; `user`, `project`, `reference` → `fact`), the agent's `description` becomes the first line, secrets are redacted, and the page records where it came from (`source: claude:feedback_tailwind.md`, tag `from-claude`; `from-copilot`, `from-copilot-user`, `from-file` likewise).
- **Nothing is overwritten.** Promoting the same file twice to the same place is refused (`was already promoted`); so is moving a page onto one that exists, or into the folder it is already in. A name that matches two pages is reported as ambiguous instead of guessed.
- **Nothing outside the memory repository is changed.** The agents' folders are only read.
- **It runs inside a project**, because `project` and Claude's memory folder both depend on which project you are in.

The Claude location is verified. The Copilot locations follow VS Code Copilot Chat's `memory-tool/memories` layout and have **not** been checked against a machine that has Copilot memories yet; `doctor` shows `none found` if yours are elsewhere.

### `git-memory demote <page>…`

The opposite of `promote --to global`: narrows a **global** memory to **this project**, by moving it from `global/` into the project's folder (committed as a rename, like a promotion). `<page>` is a page of the memory repository: its path, or a unique part of it.

```bash
git-memory demote use-rest                                  # global/memories/decision/use-rest.md → projects/shop/memories/decision/use-rest.md
git-memory demote global/memories/decision/use-rest.md      # the same, by full path
git-memory demote a b c                                     # several at once
```

It prints what it did (`demoted global/memories/decision/use-rest.md → projects/shop/memories/decision/use-rest.md`). When a name matches a global page and a project page, the global one is the one demoted. It tells you why when it cannot:

| You gave | Result |
| --- | --- |
| a page already in this project's folder | `is already in this project's folder: nothing is narrower than a project` |
| a page of another project | `belongs to projects/other: demote narrows global pages` |
| `claude:<file>`, a file on disk | refused: those are brought in with `promote`, not narrowed |
| a name that exists in the project's folder too | `… already exists`: nothing is overwritten |

Like `promote`, it runs inside a project, because "this project" is where the page goes.

### `git-memory agent init [--no-claude] [--no-copilot] [--no-commands]`

Wires **the project in the current directory** to the memory repository. Run it once per project, from inside the project.

```bash
cd your-project
git-memory agent init                  # Claude Code + Copilot hooks, and the slash commands
git-memory agent init --no-copilot     # Claude Code only
git-memory agent init --no-claude      # Copilot only
git-memory agent init --no-commands    # hooks only, no slash commands
```

| Writes | What it is |
| --- | --- |
| `.claude/settings.local.json` | Claude Code hooks (skipped with `--no-claude`) |
| `.github/hooks/git-memory.json` | Copilot hooks (skipped with `--no-copilot`) |
| `.claude/skills/*`, `.github/prompts/*` | the slash commands `/remember /decide /recall /memories /promote /forget` (skipped with `--no-commands`) |
| `.git/git-memory/gm` | the launcher the hook files call (a small script that runs the installed binary) |

It adds nothing to your project's tracked files: everything is git-excluded or lives under `.git`. It also creates the memory repository if it does not exist. It is safe to re-run (after upgrading, or to refresh the commands); your own hooks, settings and same-named command files are kept. Hook entries written by earlier development builds are replaced.

### `git-memory agent hook <claude|copilot> <event>`

What the agents' hook files call. It reads the event as JSON on stdin and may print JSON for the agent. You do not run it by hand, except to test a project's wiring:

```bash
echo '{"cwd":"'$PWD'","session_id":"t1","prompt":"remember that this is a test"}' \
  | ./.git/git-memory/gm agent hook claude UserPromptSubmit
git-memory log --oneline -n 3        # the memory it saved is there
```

Events: Claude Code `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `PostToolUseFailure`, `Stop`, `PreCompact`, `SessionEnd`; Copilot `sessionStart`, `userPromptSubmitted`, `postToolUse`, `agentStop`, `preCompact`, `sessionEnd`. A hook never fails the agent: errors go to the project's `.git/git-memory/errors.log` and the exit code stays 0.

### `git-memory agent version`

```
$ git-memory agent version
git-memory 0.0.1
```

(`git-memory --version` and `git-memory version` are git's own and print git's version; `doctor` prints both.)

### Settings: there are none

No config files, no environment variables. Like git, which is "the `.git` folder", git-memory is "the `~/.git-memory` folder": that is where the memory lives, and everything else follows from it.

`agent`, `doctor`, `promote` and `demote` are the only words git-memory keeps for itself. Everything else, including `init`, `hook` and `version`, is git's.

## Everyday jobs

### Set up (first time)

```bash
cd your-project
git-memory agent init
```

That is all. There is nothing to configure and nothing for `git status` to show in the project. Start Claude Code or Copilot in the project and sessions are captured from then on.

### Use a memory repository you already have

On a new machine, or to start from a team's repository, clone it before wiring projects:

```bash
git-memory clone git@github.com:me/memory.git
cd your-project
git-memory agent init
```

### Save a memory by hand

A memory is a markdown file in the memory repository, saved with ordinary git. Put it in the project's folder (`git-memory doctor` shows it as `Project folder`) or in `global/` for every project.

```bash
# 1. write the page (any editor works; this finds the repository for you)
mkdir -p ~/.git-memory/projects/shop/memories/decision
$EDITOR ~/.git-memory/projects/shop/memories/decision/use-rest.md
```

```markdown
---
kind: decision        # decision | procedure | fact | note
tier: semantic        # use "core" to pin it into every briefing
tags: [api]
created: 2026-11-12T10:00:00Z
---

# Public APIs are REST

External clients cannot speak GraphQL.
```

```bash
# 2. save it
git-memory add projects/shop
git-memory commit -m "memory: public APIs are REST"
```

Commits use your normal git identity (`user.name` / `user.email`).

You rarely need to: type `remember that we deploy on Tuesdays` or `decision: use pnpm, never npm` in a prompt, or run `/remember` or `/decide` in the agent, and the page is written and committed for you.

### Find and read

```bash
git-memory grep -n -i "graphql"                           # search every page
git-memory grep -n -i "retry" -- projects/shop global     # only this project and the global memories
git-memory ls-files -- projects/shop                      # list this project's pages
git-memory show HEAD:projects/shop/memories/decision/use-rest.md     # print one page
git-memory log --oneline -n 10                            # what was saved recently
git-memory log -p -- projects/shop/memories               # ...with the changes
```

`grep` and `show` see what is committed. A page you wrote but have not committed yet is not found until you `add` and `commit` it.

### Make a memory apply to every project

```bash
git-memory promote use-rest --to global       # moves it from the project's folder to global/
git-memory demote use-rest                    # and back: narrows it to the current project again (same as promote --to project)
```

### Bring in what Claude Code or Copilot already remember

They keep memories of their own, and `doctor` lists them. Copy the ones worth keeping into the memory repository, where they are committed, synced and shared like any other:

```bash
git-memory doctor                                            # see what was found
git-memory promote claude:feedback_tailwind.md --to project
git-memory promote copilot-user:preferences.md --to global
```

### Retire a decision, or forget a page

A decision that changed is better retired than deleted: add `status: superseded` to its frontmatter (and a note saying what replaced it), then commit. It stays in git, but it leaves briefings and recall.

```bash
$EDITOR ~/.git-memory/global/memories/decision/use-rest.md      # add the line:  status: superseded
git-memory commit -am "memory: retire the REST decision"
```

To delete a page that was a mistake:

```bash
git-memory rm projects/shop/memories/note/oops.md
git-memory commit -m "memory: forget oops"
```

Git keeps the history either way.

### Back it up and sync between machines

The memory repository is a normal git repository, so any remote works: GitHub, GitLab, a bare repo on a server, a folder.

```bash
# machine A: connect it once, then push
git-memory remote add origin git@github.com:me/memory.git
git-memory push -u origin main

# machine B: bring it down (once), then wire the projects
git-memory clone git@github.com:me/memory.git

# afterwards, either machine
git-memory pull
git-memory push
```

The hooks commit locally and never push, so you decide when memory leaves the machine. Each project has its own folder, so two machines (or two teammates) working on different projects do not collide; if two people edit the same page, git asks to merge like it would for any file.

### Share with a team

Use one remote and have everyone clone it: `git-memory clone <url>`, then `git-memory agent init` in each project. Pages for `projects/shop` are shared by everyone who works on `shop`; `global/` is shared by everyone. Everything in the memory repository goes to whoever can read its remote, so point a shared repository at a team-only remote, and leave the remote off for notes that should stay on your machine.

### Inside the agents

| You type | What happens |
| --- | --- |
| a prompt containing `remember that …`, `decision: …`, `#fact …`, `#procedure …` | the hook saves it as a page and commits it |
| `/remember <text>` | the agent writes a page and commits it |
| `/decide` | the agent proposes what this conversation settled; saves what you approve |
| `/recall <topic>` | the agent searches the memory and answers from it |
| `/memories` | the `doctor` report, what changed recently, anything uncommitted |
| `/promote` | promotes a memory (the agents' own, a file, or a page) to the project or `global` with `git-memory promote` |
| `/forget` | retires a decision (`status: superseded`) or deletes a page |

At the start of each session the agent is briefed automatically: pinned notes, every decision in force, recent memories and sessions.

## When something is off

| Symptom | Check |
| --- | --- |
| Anything looks off | `git-memory doctor` in the project: it checks the repository, the hooks, the launcher, recent activity and logged errors |
| A session left no page | `git-memory doctor`, then `cat .git/git-memory/errors.log` in the project and `git-memory log --oneline -n 5` |
| "… is not a git repository" | `~/.git-memory` exists with other files in it. Move it aside, clone into place, or run `git-memory init` if you want to track it |
| `git-memory` is "not found" inside an agent | the hooks don't use your `PATH`; re-run `git-memory agent init` in the project to refresh `.git/git-memory/gm` |
| You want the hooks off in a project | delete `.github/hooks/git-memory.json` and the git-memory entries in `.claude/settings.local.json`; `git-memory agent init` brings them back |
| Test a hook by hand | see `git-memory agent hook` above |
| Which version? | `git-memory doctor` (first line) or `git-memory agent version` |
