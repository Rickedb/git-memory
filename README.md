# git-memory

Persistent memory for AI coding agents where **everything is git**. There is no server, no database, no account and no config file.

`git-memory` is `git`, pointed at one repository: the memory repository, the `.git-memory` folder of the folder you are in (as a project's repository is its `.git`).

```bash
git-memory add . && git-memory commit -m "note about retries"     # = git -C .git-memory add . && … commit …
git-memory log --oneline
git-memory grep -n -i "retry"
git-memory remote add origin git@github.com:me/memory.git && git-memory push
```

Whatever you type goes to git, unchanged, inside that repository: arguments, flags, stdin, exit codes and the terminal are git's own. Because it is git, there is nothing to learn that git does not already teach, and nothing to sync that `git push` does not already sync.

Tools like [akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory) give agents long-term memory, but they need a running service (a Rust server, SQLite, Docker). `git-memory` keeps the same core loop (capture → compile into a wiki → recall) and replaces the infrastructure with a repository you already know how to use. Around it, **agent hooks** for Claude Code and GitHub Copilot capture each session as a markdown page, brief the next session from what is stored, and save "remember that …" lines from your prompts. They write plain files and plain commits into the same repository.

## Install

From the project's GitHub releases page, once a release is published (none yet). No root or administrator rights needed, and nothing outside your home directory changes.

**Linux** (x86-64 and ARM64):

```bash
curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh
```

**Windows** (PowerShell, x86-64 and ARM64):

```powershell
irm https://github.com/Rickedb/git-memory/releases/latest/download/install.ps1 | iex
```

Each script downloads the right binary for your machine, checks it against the release's `SHA256SUMS` (and refuses to install on a mismatch), installs it, and adds its folder to your PATH:

| | Linux (`install.sh`) | Windows (`install.ps1`) |
| --- | --- | --- |
| Installs to | `~/.local/bin/git-memory` | `%LOCALAPPDATA%\Programs\git-memory\git-memory.exe` |
| PATH | a marked block in `~/.bashrc`, `~/.zshrc` or `~/.profile` by your shell, or `fish_add_path` for fish; skipped if the folder is already on your PATH | your **user** PATH (the registry, keeping any `%VARIABLES%` in it); also this session's |
| Running it again | upgrades in place; the PATH entry is added once | upgrades in place (a running copy is renamed aside, not an error) |

Options, to pick a release or a folder, or to leave your PATH alone:

```bash
curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh -s -- --version 0.0.1 --dir ~/bin --no-path
```
```powershell
& ([scriptblock]::Create((irm https://github.com/Rickedb/git-memory/releases/latest/download/install.ps1))) -Version 0.0.1 -Dir C:\tools -NoPath
```

Or by hand: download `git-memory_linux_amd64`, `git-memory_linux_arm64`, `git-memory_windows_amd64.exe` or `git-memory_windows_arm64.exe` from the releases page, rename it to `git-memory` (`git-memory.exe`) and put it on your `PATH`. Or build it (Go ≥ 1.22):

```bash
make build        # bin/git-memory for this machine
make release      # release/: git-memory_<os>_<arch> for Linux and Windows (amd64, arm64), SHA256SUMS, install.sh, install.ps1
```

The only requirement is `git`. Having `git-memory` on your `PATH` also makes `git memory add .` work: git runs any `git-<name>` program it finds as `git <name>`. There is no macOS build in the releases yet.

## Quick start

```bash
cd your-project
git-memory clone git@github.com:me/memory.git     # optional: bring an existing memory repository (into ./.git-memory)
git-memory agent init                             # wire this project's agents to it (creates ./.git-memory if there is none)
```

That is the whole setup, and **it adds nothing to your project's tracked files**: the hook wiring, the slash commands and the `.git-memory` folder itself are git-excluded (`.git/info/exclude`, not your `.gitignore`).

## The memory repository

A plain git repository, and its location is not configurable: it is the `.git-memory` folder in the folder you are in. **git-memory never searches for it**, up the tree or anywhere else: be in the folder that has it (for a project, its root) and every command runs git there. Nothing creates it by accident: `git-memory init` makes one here, `git-memory clone <url>` clones one here, and `git-memory agent init` makes the project's own; any other command in a folder without one says so and does nothing. The agent hooks use the `.git-memory` of the project's root.

```
.git-memory/
  projects/<project>/
    sessions/2026/11/2026-11-12-claude-ab12cd34.md     one page per agent session (written by the hooks)
    memories/<kind>/<title>.md                         decisions, procedures, facts, notes
    core/<title>.md                                    pinned: opens every briefing for the project
  global/
    memories/<kind>/<title>.md                         applies to every project
```

`<project>` is the project's `origin` remote name, else its directory name (`git-memory doctor` shows it). Pages are markdown with a small frontmatter, so you can browse, edit and review them anywhere git repositories go:

```markdown
---
kind: decision        # decision | procedure | fact | note   (sessions are written by the hooks)
tier: semantic        # core pins it into every briefing
tags: [api]
created: 2026-11-12T10:00:00Z
---

# Public APIs are REST

Because external clients cannot speak GraphQL.
```

Curating is git:

| You want to | Do |
| --- | --- |
| Find something | `git-memory grep -n -i "retry"` |
| Read a page | `git-memory show HEAD:projects/shop/memories/decision/rest.md` |
| Share a memory with all your projects | `git-memory promote rest --to global` (moves the page and commits) |
| Narrow a global memory to one project | `git-memory demote rest` (the opposite of the line above) |
| Bring in what Claude Code or Copilot remember | `git-memory doctor` lists them; `git-memory promote claude:<file> --to project` copies one in |
| Retire a decision that changed | add `status: superseded` to its frontmatter and commit: it stays in git but leaves briefings and recall |
| Forget a mistake | `git-memory rm <page>` and commit; history keeps it |
| Sync between machines | `git-memory remote add origin <url>`, then `git-memory push` / `git-memory pull` |
| Share with a team | clone the same remote on every machine (`git-memory clone <url>`); each project has its own folder |

## What the hooks do

```
 agent hooks ──► spool (.git/git-memory/spool/*.jsonl, in the project's .git)    cheap append, ~13 ms per hook
                    │  Stop / PreCompact / SessionEnd
                    ▼
        session page  projects/<project>/sessions/YYYY/MM/<date>-<agent>-<id>.md   committed in the memory repository
                    │
   SessionStart ◄───┴── briefing: pinned notes + every decision + recent memories and sessions (≤ 6 KB)
   UserPromptSubmit ◄── recall: BM25 hits for this prompt          (Claude Code only, see below)
   the agent ◄───────── git-memory grep / show / add / commit      (both agents, via the briefing and the slash commands)
```

- **Capture records intent, not output.** Prompts, files edited, shell commands, failures and the agent's final message. Tool output is never stored.
- **Privacy.** Secrets are redacted before anything is written (API keys, tokens, JWTs, private keys, `KEY=value` assignments, URL credentials). Text inside `<private>…</private>` is dropped. Files such as `.env`, `*.pem`, `*.key` and `secrets/**` are never recorded.
- **Remembering from the prompt.** A line like `remember that we deploy on Tuesdays` or `decision: use pnpm, never npm` is saved as a page and committed. No model is involved. Only unambiguous markers count, one memory per line:

  ```
  remember that we deploy on Tuesdays     → note        remember: staging is rebuilt nightly   → note
  decision: use pnpm, never npm           → decision    #decision public APIs are REST         → decision
  #fact the API listens on 8081           → fact        #procedure run make migrate first      → procedure
  ```

  (`remember to run the tests` is an instruction for the current task, not a memory.)
- **Commits are scoped.** A hook commits only its own folder, so whatever else you have staged in the memory repository stays staged.
- **Fail-open.** A hook error is logged to the project's `.git/git-memory/errors.log` and the hook exits 0; it never blocks the agent.

## Slash commands

`git-memory agent init` also installs seven commands for Claude Code (skills) and Copilot (prompt files), git-excluded like the hooks. They teach the agent to use git on the memory repository:

| Command | What it does |
| --- | --- |
| `/remember <text>` | writes a page for the project (or for every project) and commits it |
| `/decide` | reviews the conversation, proposes the decisions, procedures and facts worth keeping, and saves the ones you approve |
| `/recall <topic>` | `git grep`s the memory and answers from it, citing pages by path |
| `/memories` | where the memory is, what changed recently, anything uncommitted |
| `/list [scope]` | lists the memories of a scope (`claude`, `copilot`, `personal` for the global ones, `project`, or any folder of the memory repository), or the project's `sessions` (optionally of one date), and says what each one is for |
| `/promote` | promotes a memory to this project or to every project (also the agents' own memories) |
| `/forget` | retires a decision (`status: superseded`) or deletes a page |

Commands that write are user-only (the model never invokes them on its own). The files are the same on every machine: they call the CLI as `./.git/git-memory/gm` (relative to the repository root) and contain no home directory or memory location. To share them with the team, `git add -f .claude/skills .github/prompts`: a teammate's `agent init` regenerates byte-identical files, so nothing shows up as modified. A file of yours with the same name is never overwritten.

If `./.git/git-memory/gm` is missing (a fresh clone, a new machine), every command tells the agent to say what is missing, show how to install it (`install.sh` / `install.ps1`, then `git-memory agent init`) and ask before running anything. The commands use no `!` shell injection on purpose: Claude Code aborts the whole skill when an injected command fails, which is what happened on a repository with no commits yet.

## Commands

The full guide, with the everyday recipes (cloning, committing, searching, syncing), is in [COMMANDS.md](COMMANDS.md). In short:

```
git-memory <git command> [args…]     git <git command> [args…], inside the memory repository
git-memory clone <url>               clone an existing memory repository into ./.git-memory
git-memory init                      git init here: creates ./.git-memory

git-memory doctor [--report]         the version and a health report: repository, project folder, hooks, activity, errors,
                                     and the agents' own memories it finds (Claude Code, Copilot)
git-memory list [repo|claude|copilot|sessions] [date]…
                                     every memory, grouped: the memory repository, Claude Code's own, Copilot's;
                                     `sessions` lists the project's session pages, optionally of one day, month or year
git-memory promote <source>… --to project|global     copy or move memories into this project's folder or the global one
git-memory demote <page>…                            the opposite: narrow a global memory to this project

git-memory agent init [--no-claude] [--no-copilot] [--no-commands]
git-memory agent hook <claude|copilot> <event>      what the agents' hook files call
git-memory agent version
```

`agent`, `doctor`, `list`, `promote` and `demote` are the only words git-memory keeps for itself. Git has no such commands (a test checks them against the git on the machine running the tests), so nothing you type for git is ever intercepted, including `init`, `hook` and `version`, which are git's. There is nothing to configure: no config files and no environment variables.

The project's `.git/git-memory/gm` is the **launcher** the hook files call. On Linux and macOS it is a two-line `sh` script that `exec`s the installed binary (so upgrading the binary needs no re-run); on Windows it is a copy of the `.exe`, so re-run `agent init` after upgrading. It is the one file with a machine-specific path, it lives under `.git`, and it is never committed.

## Agent support

| | Claude Code (CLI + VS Code extension) | GitHub Copilot (VS Code, preview) |
| --- | --- | --- |
| Capture prompts, edits, commands | yes | yes |
| Briefing at session start | yes (`hookSpecificOutput.additionalContext`) | yes (`additionalContext`) |
| Per-prompt recall injection | yes | **no**: Copilot drops `userPromptSubmitted` output for config-file hooks, so recall happens when the agent runs `git-memory grep`, as the briefing tells it to |
| Final message captured | yes (`last_assistant_message` / transcript) | best effort via the transcript path |

No instruction files are added to the project: the session-start briefing itself tells the agent how to search and store memories, which is what Copilot relies on in place of per-prompt recall. Copilot also reads `.claude/settings*.json`; duplicate events in the same second are dropped.

## Caveats

- The Copilot hook payloads and output shapes follow GitHub's published hooks reference. They are covered by tests that replay those payloads, but have not been run against a live VS Code Copilot session yet. Hooks are in preview there, so check `.git/git-memory/errors.log` if a session leaves no page.
- The Windows binaries are cross-compiled and vetted, and their path, process and shell handling is written for Windows, but they have not been run on Windows yet (the end-to-end tests that drive the hooks skip on Windows for now). Unverified there: how Claude Code and Copilot run the hook `command` (the files use `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm.exe" agent hook …` and `./.git/git-memory/gm.exe agent hook …`), and quoting for project paths with spaces.
- Projects wired by an earlier development build call `gm hook <agent> <event>`. That form keeps working, and `git-memory agent init` replaces it. The config files, scopes and commands of those builds (`remember`, `search`, `sync`, `doctor`, …) are gone: the same things are git commands now.
- The Claude Code memory location is verified. The Copilot ones (`doctor` and `promote` read VS Code Copilot Chat's `memory-tool/memories` folders) follow its layout but have not been checked against a machine that has Copilot memories yet; if yours are elsewhere `doctor` says `none found`.
- `grep`, `show` and friends see what is committed. The hooks commit their own pages; a page you write by hand is found once you `add` and `commit` it.
- Search is lexical (`git grep` for you, BM25 + recency + kind weighting for the hooks' recall). There are no embeddings.

## Development

Go (≥ 1.22) and git are all you need.

```bash
make build      # bin/git-memory for this machine
make test       # go vet + go test (unit tests and end-to-end tests: the real binary against real git repos in temp dirs)
make release    # release/git-memory_<os>_<arch> for Linux and Windows, amd64 and arm64, plus SHA256SUMS
```

Layout: `cmd/git-memory` is the entry point, and the rest is `internal/gm`, in layers that only depend downwards:

```
internal/gm/           the command line: routing to git, the help, dispatching our own commands (and the Windows/Unix process exec)
internal/gm/command/   our own commands: doctor, list, promote, demote, agent init, the slash commands, the agents' memory folders
internal/gm/hook/      what the agents' hooks run: events, spool, session pages, briefing, recall, "remember that …" triggers
internal/gm/core/      the memory repository and pages, the plain-git helpers, redaction, settings (uses nothing else of ours)
```

The end-to-end tests (`internal/gm/e2e_test.go`) build the binary once and drive it exactly as an agent or a person would: `git-memory <git command>`, `agent init`, hooks fed on stdin, and the installed launcher. Each package also has its own unit tests.
