# git-memory: commands and how to use them

`git-memory` is `git`, pointed at one repository: your **memory repository**, the `.git-memory` folder of the folder you are in (for a project, its root). This page lists the commands git-memory has of its own, then walks through the everyday jobs. It does not list git's commands: those are git's, and `git help` or `git-memory help` covers them.

## The commands

### `git-memory <git command> [args…]`

Runs `git <git command> [args…]` **inside the memory repository**: the `.git-memory` folder of the folder you are in. Arguments, flags, stdin, the terminal and the exit code are git's own.

```bash
git-memory status
git-memory add .
git-memory commit -m "memory: we deploy on Tuesdays"
git-memory log --oneline -n 10
git-memory grep -n -i "retry"
git-memory push
```

**git-memory does not search for the memory repository.** Be in the folder that has the `.git-memory` (for a project, its root). In any other folder the command says `no memory repository at <folder>/.git-memory: run git-memory init` and does nothing: only `init`, `clone` and `agent init` ever create one, so a mistyped folder never grows a stray one.

Paths are relative to the memory repository, not to your current directory: `git-memory add global` stages `.git-memory/global`.

### `git-memory clone <url>`

Clones an existing memory repository **into** `.git-memory` in the current folder (you do not give a destination). Options before the URL go to `git clone`.

```bash
git-memory clone git@github.com:me/memory.git
git-memory clone --depth 1 https://example.com/me/memory.git
```

It refuses if `.git-memory` already exists here and is not empty. Move that folder aside first, or work with it directly (`git-memory pull`).

### `git-memory init`

`git init`, **here**: creates the `.git-memory` folder in the current folder and makes it the memory repository (branch `main`). When the current folder is inside a git project, the new folder is kept out of the project's `git status` through `.git/info/exclude`, not your `.gitignore`. Extra arguments go to `git init`. Use it too when a `.git-memory` folder with files you want to keep already exists.

```bash
cd your-project
git-memory init                                  # ./.git-memory is now a repository (any files in it are untouched)
git-memory add . && git-memory commit -m "start tracking"
```

### `git-memory doctor [--report]`

Prints the **version** and a **health report**: where the memory repository is, how it is doing, and, when you run it inside a project, whether that project is wired and what has happened there. It is the first thing to run when something looks off, and the place to look up where this project's memory lives.

```
$ git-memory doctor
git-memory 0.0.1 · git version 2.56.0

✓ Memory repository: /home/you/shop/.git-memory (branch main, 2 commits)
✓ Remote: origin → git@github.com:me/memory.git
✓ Uncommitted changes: none
✓ Root folder: ./ (the repository's root; the project's .git-memory.json names another folder)
✓ Global folder: global (memories that apply to every project)
✓ Project folder: . (the project's memories: memories/, core/, scopes/, sessions/)
✓ Pages: 1 memory and 1 session in this project, 0 global
✓ Claude Code hooks: 7 events
✓ Copilot hooks: 6 events
✓ Codex hooks: 6 events
✓ Cursor hooks: 7 events
✓ Gemini CLI hooks: 6 events
✓ Slash commands: Claude Code 8/8, Copilot 8/8, Codex 8/8, Cursor 8/8, Gemini CLI 8/8
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
| `·` | nothing to report yet (no remote, no agent activity) |
| `!` | worth a look: no memory repository here, uncommitted changes, hooks not installed, errors logged |
| `✗` | broken: the memory folder is not a repository, the hooks point at a binary that no longer exists |

The last rows list the **agents' own memories** it found, in place and read-only: Claude Code's auto memory for this project (`~/.claude/projects/<project>/memory`, or under `$CLAUDE_CONFIG_DIR`), VS Code Copilot Chat's memory for this workspace, and its user-level memory. Each shows its folder, how many memories it holds and the first file names (Claude's own `MEMORY.md` index is not counted). Those names are what `git-memory promote` takes.

It exits with 1 when any row is `✗`, so scripts can rely on it; `--report` makes it always exit 0 (the `/memory-status` slash command uses that, so a warning can never abort it). Inside a project it checks the project root's `.git-memory`; outside one, the current folder's, and it says `· Project: not inside a git project`. It never creates anything.

### `git-memory list [repo|claude|copilot|sessions] [YYYY[-MM[-DD]]]…`

Lists **every memory there is** for the project you are in, in one place, grouped by where it lives: the memory repository (this project's and the global ones), Claude Code's own memory, and Copilot's. It only reads.

```
$ git-memory list
.git-memory · this project · 2 memories
  decision  Public APIs are REST          memories/decision/use-rest.md
  fact      The API listens on 8080       memories/fact/port.md

.git-memory · global · 1 memory
  decision  Use pnpm                      global/core/pnpm.md  — pinned

Claude Code memory (/home/you/.claude/projects/-home-you-shop/memory) · 1 memory
  decision  use-tailwind                  claude:feedback_tailwind.md  — The user prefers Tailwind over CSS modules

Copilot memory (this workspace) (none found)

Copilot memory (user) (none found)
```

Each row has the kind, the title and a **name**: the page's path in the memory repository, or `claude:<file>`, `copilot:<file>`, `copilot-user:<file>` for the agents' memories. That is exactly what `promote` and `demote` take, so a listing is a to-do list:

```bash
git-memory list                      # everything
git-memory list claude               # only Claude Code's own memory
git-memory list copilot              # Copilot's, this workspace's and yours
git-memory list repo                 # only the memory repository
git-memory list sessions             # this project's session pages, newest first
git-memory list sessions 2026-11     # only the sessions of a month (a year or a day work too)
git-memory list 2026-11-13           # a date on its own means sessions
git-memory promote claude:feedback_tailwind.md --to project
```

- Session pages are local files, kept out of git by the memory repository's `.gitignore`, and are not memories, so a plain `list` leaves them out; ask for them with `sessions`, newest first, optionally narrowed to a year, month or day (`2026`, `2026-11`, `2026-11-13`). Each row shows the first prompt of the session as its title, and its date and agent. Pages not committed yet are listed. A retired page shows `— superseded`, a pinned one `— pinned`; for the agents' memories the note is their own description.
- Run it from a project root. Outside a project it lists what does not depend on one: the current folder's `.git-memory` (global memories) and Copilot's user-level memory.
- `doctor` shows how many memories each agent has; `list` shows what they are.

### `git-memory promote <source>… --to project|scope|global`

Brings a memory into the memory repository, into **this project's folder** (`--to project`), the **scope** you work in (`--to scope`, when one is chosen: see Scopes), or the **global** one that applies to every project (`--to global`). `<source>` can be:

| Source | Example | What happens |
| --- | --- | --- |
| a page already in the memory repository | `memories/decision/use-rest.md` or just `use-rest` | **moved** to the other folder, committed as a rename so history follows it |
| one of Claude Code's own memories | `claude:feedback_tailwind.md` | **copied**; the original stays where Claude keeps it |
| one of Copilot's own memories | `copilot:build.md` (this workspace), `copilot-user:tabs.md` (yours) | **copied** |
| any markdown file | `./notes/retries.md` | **copied** |

```bash
git-memory list claude                                       # lists the agents' memories (doctor counts them)
git-memory promote claude:feedback_tailwind.md --to project  # this project only
git-memory promote claude:tailwind --to global               # a unique part of the name is enough
git-memory promote copilot-user:tabs.md --to global
git-memory promote ./notes/retries.md --to project
git-memory promote use-rest --to global                      # a page of the repository: moved
git-memory promote a.md b.md c.md --to project               # several at once
```

Each promotion prints what it did (`promoted claude:feedback_tailwind.md → memories/decision/use-tailwind.md`) and commits it.

- **Copies are converted.** Claude's `type` becomes a kind (`feedback` → `decision`, since it is guidance on how to work; `user`, `project`, `reference` → `fact`), the agent's `description` becomes the first line, secrets are redacted, and the page records where it came from (`source: claude:feedback_tailwind.md`, tag `from-claude`; `from-copilot`, `from-copilot-user`, `from-file` likewise).
- **Nothing is overwritten.** Promoting the same file twice to the same place is refused (`was already promoted`); so is moving a page onto one that exists, or into the folder it is already in. A name that matches two pages is reported as ambiguous instead of guessed.
- **Nothing outside the memory repository is changed.** The agents' folders are only read.
- **It runs inside a project**, because `project` and Claude's memory folder both depend on which project you are in. It writes into the `.git-memory` of the project's root, which must exist (`git-memory init` or `agent init`).

The Claude location is verified. The Copilot locations follow VS Code Copilot Chat's `memory-tool/memories` layout and have **not** been checked against a machine that has Copilot memories yet; `doctor` shows `none found` if yours are elsewhere.

### `git-memory demote <page>… [--to project|scope]`

The opposite of `promote --to global`: narrows a **global** memory to **this project**, by moving it from `global/` into the project's folder (committed as a rename, like a promotion). `<page>` is a page of the memory repository: its path, or a unique part of it.

```bash
git-memory demote use-rest                                  # global/memories/decision/use-rest.md → memories/decision/use-rest.md
git-memory demote global/memories/decision/use-rest.md      # the same, by full path
git-memory demote a b c                                     # several at once
```

It prints what it did (`demoted global/memories/decision/use-rest.md → memories/decision/use-rest.md`). When a name matches a global page and a project page, the global one is the one demoted. It tells you why when it cannot:

| You gave | Result |
| --- | --- |
| a page already in this project's own memories | `is already in this project's own memories: nothing is narrower than a project` |
| `claude:<file>`, a file on disk | refused: those are brought in with `promote`, not narrowed |
| a name that exists in the project's folder too | `… already exists`: nothing is overwritten |

Like `promote`, it runs inside a project, because "this project" is where the page goes. It uses the `.git-memory` of the project's root.

### `git-memory sync [--no-push] | --continue | --abort`

Shares your memory with the team: fetches the remote, merges the default branch into your branch (named after your git user name) and pushes your branch to the default branch. Exit 0 when shared, 2 when a merge waits for its conflicts to be resolved, 1 for anything else. `--no-push` stops after the merge. See "Share with a team" below.

### `git-memory agent init <claude|copilot|codex|cursor|gemini|all>… [--root <folder>] [--scope <name>|--no-scope] [--no-commands] [--no-instructions]`

Wires **the project in the current directory** to the memory repository, for the agents you name: `claude` (Claude Code), `copilot` (GitHub Copilot), `codex` (OpenAI Codex CLI), `cursor` (Cursor), `gemini` (Gemini CLI), or `all` for every agent. **You must name one**: with none it fails with `say which agent to set up: git-memory agent init <claude|copilot|codex|cursor|gemini|all>` and writes nothing. Run it once per project, from inside the project; run it again with another agent's name to add that one later, and what is already there stays.

```bash
cd your-project
git-memory agent init claude           # Claude Code only
git-memory agent init copilot          # Copilot only
git-memory agent init codex cursor     # any combination
git-memory agent init claude copilot   # both
git-memory agent init all              # every agent
git-memory agent init claude --no-commands      # hooks and instructions, no slash commands
git-memory agent init claude --no-instructions  # hooks and slash commands, no instructions file
git-memory agent init claude --scope checkout   # this checkout works in the "checkout" scope (see Scopes)
git-memory agent init claude --root docs/memory # the memories live in docs/memory of the memory repository (see Settings)
```

| Writes | What it is |
| --- | --- |
| `.claude/settings.local.json` | Claude Code hooks (`claude`) |
| `.github/hooks/git-memory.json` | Copilot hooks (`copilot`) |
| `.codex/hooks.json` | Codex hooks (`codex`); Codex runs them once you trust them (`/hooks`) |
| `.cursor/hooks.json` | Cursor hooks (`cursor`), merged with yours |
| `.gemini/settings.json` | Gemini CLI hooks (`gemini`), merged into your settings |
| `.claude/skills/*`, `.github/prompts/*`, `.agents/skills/*`, `.cursor/commands/*`, `.gemini/commands/*` | for each agent you named, the slash commands `/memory-remember /memory-decide /memory-recall /memory-status /memory-list /memory-promote /memory-refine /memory-forget /memory-sync` in that agent's own format: Claude Code and Codex skills, Copilot prompt files, Cursor markdown commands, Gemini CLI TOML commands (skipped with `--no-commands`) |
| `.claude/rules/git-memory.md`, `.github/instructions/git-memory.instructions.md`, `.cursor/rules/git-memory.mdc` | for Claude Code, Copilot and Cursor, its always-loaded instructions: the memory repository's folder layout and how to read it (`grep`, `show`, `list`, …) (skipped with `--no-instructions`). Codex and Gemini CLI get none: their `AGENTS.md` and `GEMINI.md` are the project's own, tracked files, so the session briefing carries the same guidance |
| `.git/git-memory/gm` | the launcher the hook files call (a small script that runs the installed binary) |

An agent that is not wired is not an error: `doctor` shows `· Claude Code hooks: not set up (git-memory agent init claude)` while another agent is wired. `--no-claude` and `--no-copilot`, the flags this argument replaced, are refused with a message saying so.

It adds nothing to your project's tracked files: everything is git-excluded or lives under `.git`. It also creates the project's `.git-memory` (git-excluded too) if there is none, and never touches one that is there, or a folder of that name that holds other files. It is safe to re-run (after upgrading, or to refresh the commands); your own hooks, settings and same-named command files are kept. Hook entries written by earlier development builds are replaced.

### `git-memory agent scope [<name>|--clear]`

Shows, chooses or clears the scope this checkout works in (see Scopes under Settings), without wiring anything: `git-memory agent scope checkout`, `git-memory agent scope` (what is chosen, and which scopes exist), `git-memory agent scope --clear`. It is the same choice as `agent init <agent> --scope <name>`; either way the choice is local to the checkout, and the instructions file of every agent the project is wired for is rewritten to name it. The scope is a name of lowercase letters, digits and dashes.

### `git-memory agent hook <claude|copilot|codex|cursor|gemini> <event>`

What the agents' hook files call. It reads the event as JSON on stdin and may print JSON for the agent. You do not run it by hand, except to test a project's wiring:

```bash
echo '{"cwd":"'$PWD'","session_id":"t1","prompt":"remember that this is a test"}' \
  | ./.git/git-memory/gm agent hook claude UserPromptSubmit
git-memory log --oneline -n 3        # the memory it saved is there
```

Events: Claude Code `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `PostToolUseFailure`, `Stop`, `PreCompact`, `SessionEnd`; Copilot `sessionStart`, `userPromptSubmitted`, `postToolUse`, `agentStop`, `preCompact`, `sessionEnd`; Codex `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `Stop`, `PreCompact`, `SessionEnd`; Cursor `sessionStart`, `beforeSubmitPrompt`, `postToolUse`, `postToolUseFailure`, `stop`, `preCompact`, `sessionEnd`; Gemini CLI `SessionStart`, `BeforeAgent`, `AfterTool`, `AfterAgent`, `PreCompress`, `SessionEnd`. A hook never fails the agent: errors go to the project's `.git/git-memory/errors.log` and the exit code stays 0.

### `git-memory agent version`

```
$ git-memory agent version
git-memory 0.0.1
```

(`git-memory --version` and `git-memory version` are git's own and print git's version; `doctor` prints both.)

### Settings: one file, in the memory repository

No config files in your project, no environment variables, no per-user settings. Like git, which is "the `.git` folder", git-memory is "the `.git-memory` folder": the one in the folder you are in is where the memory lives, and everything else follows from it. The agent hooks use the one in the project's root.

The one setting is **the memory root folder**: the folder *inside* the memory repository that holds the project's memories (`memories/`, `core/`, `scopes/`, `sessions/`) and `global/`. By default it is the repository's top. A memory repository whose memories live in a folder of it names that folder in `.git-memory.json`, a file at the top of the **project** (next to the `.git-memory` folder, not inside it) that is **committed with the project**, so everyone who clones the project gets the same layout:

```json
{ "root": "docs/memory" }
```

```bash
git-memory agent init claude --root docs/memory    # writes the project's .git-memory.json (keeping anything else in it), then wires the project; you commit the file with the project
git-memory agent init claude --root .              # back to the repository's top
```

You can also write the file by hand and commit it, which is how you change the root without `agent init`. A clone of a project that has it needs nothing else; `git-memory` never commits it for you, because it is the project's file, not the memory repository's. The value must be a folder inside the repository: no file, no `root`, `.`, `./` and an empty value mean the top, and `..`, absolute paths and `.git` are refused (`doctor` shows `✗ Root folder: …`, and `agent init` fails before writing anything). Everything follows the setting: where the hooks write sessions and memories, the `.gitignore` that keeps sessions out of git (it goes into the root folder), `list`, `promote`, `demote`, the briefing, and the paths the agents are taught. Page paths are git's own and include the folder (`docs/memory/memories/decision/rest.md`), so `show HEAD:<path>` and `grep -- <path>` work as they are. Moving the setting does not move existing pages: `git-memory mv` them. Re-run `agent init` after changing it, so the instructions file names the new folder. `doctor` shows it as the `Root folder` row.

**`autoCommit`** is a second key of the same file: `{ "root": "docs/memory", "autoCommit": true }`. By default the hooks commit only the pages they write themselves (a remembered page, the memories a session leaves). With `autoCommit` on, they also commit **every change under the memory root folder** (the pages you edit by hand, `global/`, a page the agent refined) when a session starts, a turn ends or a session ends, as `memory: auto-commit N changed files`. Session pages stay out of it (git ignores them), and nothing outside the root folder is touched. It is off without the key; `doctor` shows an `Auto commit` row when it is on. Only the hooks commit: a change made while no agent session runs waits for the next one.

`agent`, `doctor`, `list`, `promote` and `demote` are the only words git-memory keeps for itself. Everything else, including `init`, `hook` and `version`, is git's.

### Scopes: the part of a project you work in

Two different things are called by similar names, and they are set in different places. The **root folder** (above) is where the memories sit inside the memory repository, and it is shared: it is in the project's committed `.git-memory.json`. A **scope** is a part of a project (a module of an app: `checkout`, `search`, `mobile-app`) whose memories sit in their own folder, and **choosing the scope you work in is local**: it is a file under the project's `.git/git-memory/`, never committed, so each person and each checkout chooses theirs.

```
(the memory root folder)
  memories/…  core/…                  the project's own, for every scope
  scopes/checkout/{memories,core}/…   one scope
  scopes/search/{memories,core}/…     another
```

All scopes are in the repository, downloaded and shared like any page. What a scope changes is what a checkout **reads**:

```bash
git-memory agent scope checkout                   # this checkout works in "checkout" (or: agent init <agent> --scope checkout)
git-memory agent scope                            # which one is chosen
git-memory agent scope --clear                    # back to reading every scope (or: agent init <agent> --no-scope)
```

With a scope chosen, the briefing, the recall on each prompt, `list`, `doctor`'s page counts and the folders the agents are told to `grep` cover the project's own memories, that scope's and the global ones: the project's other scopes are left out. New memories (`remember that …`, `/memory-remember`) go into the scope's folder; `promote --to project` lifts one to the whole project, `promote --to scope` (and `demote --to scope`) brings one into the scope, `promote --to global` shares it with every project. With no scope chosen, nothing changes: every scope is read, and new memories go to the project's folder. **Staying in the scope is the rule; reading beyond it is something you ask for.** For one request, say so in the prompt: "across all scopes", "every scope", "ignore the scope", or name another scope that exists ("in the search scope", "scope: search"). The per-prompt recall (Claude Code, Codex, Gemini CLI) then also searches every scope, or that one, for that prompt only, and says so; the next prompt is back to your scope. The agents are told the same rule in the briefing and the instructions, so one that searches for itself (Copilot, Cursor) widens its `grep` to the project folder or to that scope's folder when you ask. To read everything for good, `git-memory agent scope --clear`; `git-memory list repo --all-scopes` lists every scope once. Session pages are the project's, not a scope's. A scope is a name of lowercase letters, digits and dashes; its folder is made with its first memory. Because `git-memory <git command>` is plain git, a `git-memory grep` with no folder still searches all of them, which is on purpose.

Choosing a scope also rewrites the instructions file of every agent the project is already wired for, so they all name it. `doctor` shows the `Scope`, `Write folder` and `Read folders` rows.

## Everyday jobs

### Set up (first time)

```bash
cd your-project
git-memory agent init claude    # or copilot, or all
```

That is all: it creates the project's `.git-memory`. There is nothing to configure and nothing for `git status` to show in the project. Start your agent in the project and sessions are captured from then on.

### Use a memory repository you already have

On a new machine, or to start from a team's repository, clone it into the project before wiring it:

```bash
cd your-project
git-memory clone git@github.com:me/memory.git      # into ./.git-memory
git-memory agent init claude                        # (or copilot, or all) keeps what is there, adds the hooks
```

### Save a memory by hand

A memory is a markdown file in the memory repository, saved with ordinary git. Put it in the project's folder (`git-memory doctor` shows it as `Project folder`) or in `global/` for every project.

```bash
# 1. write the page (any editor works), from the project's root
mkdir -p .git-memory/memories/decision
$EDITOR .git-memory/memories/decision/use-rest.md
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
git-memory add memories
git-memory commit -m "memory: public APIs are REST"
```

Commits use your normal git identity (`user.name` / `user.email`).

You rarely need to: type `remember that we deploy on Tuesdays` or `decision: use pnpm, never npm` in a prompt, or run `/memory-remember` or `/memory-decide` in the agent, and the page is written and committed for you.

### Find and read

```bash
git-memory grep -n -i "graphql"                           # search every page
git-memory grep -n -i "retry" -- memories core scopes global     # only this project and the global memories
git-memory ls-files -- memories                           # list this project's memories
git-memory show HEAD:memories/decision/use-rest.md     # print one page
git-memory log --oneline -n 10                            # what was saved recently
git-memory log -p -- memories               # ...with the changes
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
git-memory list claude copilot                               # see what was found
git-memory promote claude:feedback_tailwind.md --to project
git-memory promote copilot-user:preferences.md --to global
```

### Retire a decision, or forget a page

A decision that changed is better retired than deleted: add `status: superseded` to its frontmatter (and a note saying what replaced it), then commit. It stays in git, but it leaves briefings and recall.

```bash
$EDITOR .git-memory/global/memories/decision/use-rest.md      # add the line:  status: superseded
git-memory commit -am "memory: retire the REST decision"
```

To delete a page that was a mistake:

```bash
git-memory rm memories/note/oops.md
git-memory commit -m "memory: forget oops"
```

Git keeps the history either way.

### Back it up and sync between machines

The memory repository is a normal git repository, so any remote works: GitHub, GitLab, a bare repo on a server, a folder.

```bash
# machine A: connect it once, wire the project, then share
git-memory remote add origin git@github.com:me/memory.git
git-memory agent init claude    # or copilot, or all
git-memory sync

# machine B: bring it down (once), into the project, then wire it
git-memory clone git@github.com:me/memory.git
git-memory agent init claude

# afterwards, either machine
git-memory sync
```

The hooks commit locally and never push, so you decide when memory leaves the machine: `git-memory sync` is what shares it (see below). Each project has its own `.git-memory`, so each one has its own remote and history.

### Share with a team

**Everyone commits to a branch of their own.** `agent init` checks out a branch named after your git user name (`Henrique Dal Bello` → `henrique-dal-bello`), and every commit the hooks and commands make lands there, so two people never write to the same branch and a conflict is met once, when somebody syncs, not on every commit. `git-memory init` itself stays plain git and makes `main`.

**`git-memory sync`** brings your branch together with the team's and shares it:

1. it fetches the remote;
2. it merges the default branch (`origin/main`, or whatever the remote's HEAD is) into your branch;
3. it pushes your branch to the default branch (a fast-forward, since your branch now contains it). If somebody pushed in between, it merges again and retries.

It refuses while pages are uncommitted and when the memory repository has no remote. If the remote refuses the push because `main` is protected, your merged branch is ready to push as it is (`git-memory push origin <your-branch>`) and open as a pull request.

**Conflicts** (the same page changed on both sides) stop the merge half way, exit code 2, listing the files and why. Settle each file (the markers include the common ancestor; `git-memory show :1:<file>`, `:2:` and `:3:` print the ancestor, yours and theirs), `git-memory add` it, then `git-memory sync --continue`; `git-memory sync --abort` puts your branch back as it was. The `/memory-sync` command does this for you: it merges what is clearly mergeable (frontmatter dates and tags, lines both sides added, the same edit made twice) and asks you only when it is in doubt (two sides contradicting each other, a deleted page the other side changed, a pinned `core/` note changed in substance by both).

Use one remote per project and have everyone clone it into their copy of the project: `git-memory clone <url>`, then `git-memory agent init <agent>`. The memories are shared by everyone who works on the project; `global/` is shared by everyone who uses that repository. A memory repository holds one project: to keep several in one, give each its own root folder (`.git-memory.json`) and use scopes for their modules. Everything in the memory repository goes to whoever can read its remote, so point a shared repository at a team-only remote, and leave the remote off for notes that should stay on your machine.

### Inside the agents

| You type | What happens |
| --- | --- |
| a prompt containing `remember that …`, `decision: …`, `#fact …`, `#procedure …` | the hook saves it as a page and commits it |
| `/memory-remember <text>` | the agent writes a page and commits it |
| `/memory-decide` | the agent proposes what this conversation settled; saves what you approve |
| `/memory-recall <topic>` | the agent searches the memory and answers from it |
| `/memory-status` | the `doctor` report, what changed recently, anything uncommitted |
| `/memory-promote` | promotes a memory (the agents' own, a file, or a page) to the project or `global` with `git-memory promote` |
| `/memory-refine` | the agent proposes a clearer, more complete or current version of one memory; applies it (and sets `updated`) after you approve |
| `/memory-forget` | retires a decision (`status: superseded`) or deletes a page |
| `/memory-sync` | merges the team's memory into your branch, settles page conflicts (asking only when in doubt) and pushes |

At the start of each session the agent is briefed automatically: pinned notes, every decision in force, recent memories and sessions.

## When something is off

| Symptom | Check |
| --- | --- |
| Anything looks off | `git-memory doctor` in the project: it checks the repository, the hooks, the launcher, recent activity and logged errors |
| A session left no page | `git-memory doctor`, then `cat .git/git-memory/errors.log` in the project and `git-memory list sessions` (session pages are local files, kept out of git: `log` and `ls-files` do not show them) |
| "no memory repository at …" | you are not in the folder that has the `.git-memory` (nothing is searched for), or it was never made: `cd` to it, or run `git-memory init` / `git-memory agent init` here |
| "… is not a git repository" | the `.git-memory` folder exists with other files in it. Move it aside, or run `git-memory init` there if you want to track it |
| `git-memory` is "not found" inside an agent | the hooks don't use your `PATH`; re-run `git-memory agent init <agent>` in the project to refresh `.git/git-memory/gm` |
| You want the hooks off in a project | delete `.github/hooks/git-memory.json` and the git-memory entries in `.claude/settings.local.json`; `git-memory agent init <agent>` brings them back |
| Test a hook by hand | see `git-memory agent hook` above |
| Which version? | `git-memory doctor` (first line) or `git-memory agent version` |
