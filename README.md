# git-memory

Persistent memory for AI coding agents where **everything is a git operation**. There is no server, no database and no account.

Tools like [akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory) give agents long-term memory, but they need a running service (a Rust server, SQLite, Docker) and mostly target CLI agents. `git-memory` keeps the same core loop (capture → compile into a wiki → recall) and replaces the infrastructure with the repo you already have.

| ai-memory piece | git-memory equivalent |
| --- | --- |
| Server receiving hook events | Hooks append to a local spool in `.git/git-memory/spool` |
| Git-backed markdown wiki | Git repos: your **personal store** (default), plus optional shared stores (a memory repo, or a branch of the project) |
| SQLite FTS5 index | BM25 computed in memory from the markdown files on each query (a few ms for thousands of pages) |
| Multi-machine / team sharing | `git-memory sync` = rebase every store on its remote + push |
| Aging, forgetting | `git rm` + commit; `git log` keeps the history |
| LLM summaries | Optional: `summarizer` is any shell command that reads a page on stdin |

Zero dependencies: git and the single `git-memory` binary (Linux and Windows, no runtime needed).

## Quick start

Download `git-memory_linux_amd64`, `git-memory_linux_arm64`, `git-memory_windows_amd64.exe` or `git-memory_windows_arm64.exe` from the releases page, rename it to `git-memory` (`git-memory.exe`) and put it on your `PATH`. Or build it (Go ≥ 1.22):

```bash
make build        # bin/git-memory for this machine
make release      # release/git-memory_<os>_<arch> for Linux and Windows, amd64 and arm64, plus SHA256SUMS
```

Then, in your project:

```bash
cd your-project
git-memory init
```

`init` wires the hooks of both agents. The hook files only contain the project-relative path `.git/git-memory/gm`; that launcher is what knows where the binary is (on Linux a two-line `sh` script that `exec`s the installed binary, so upgrading the binary needs no re-init; on Windows a copy of the `.exe`, so re-run `init` after upgrading). Running as a hook takes about 13 ms, because it is a static binary with no runtime to start.

That is the whole setup, and **it adds nothing to your project**: memories are *personal* by default, kept in `~/.git-memory/personal` (a local git repo created on first use), and the hook wiring is git-excluded.

| Where | What | In your project's git history? |
| --- | --- | --- |
| `~/.git-memory/personal` | Your memories, sessions included | no, outside the project |
| `.git/git-memory/` | Runtime, event spool, machine-local config | no (inside `.git`) |
| `.claude/settings.local.json`, `.github/hooks/git-memory.json` | Hook wiring for Claude Code and Copilot | no, git-excluded |
| `.claude/skills/*`, `.github/prompts/*` | The slash commands below | no, git-excluded |

Your `.gitignore` is never touched. From there, **you decide what gets wider**: promote a memory to `global` (every project of yours), to a named scope (a team, a product area), or to the project's shared store, with `git-memory promote <ref>`, the extension, or by changing the default scope (below).

### Sharing with a team (opt-in)

```bash
git-memory init --store git@github.com:acme/ai-memory.git   # a shared memory repo (or: init --branch for a branch of this repo)
git add git-memory.json && git commit -m "Add git-memory setup"
```

This creates the **project scope** and adds one committed file, `git-memory.json`, plus `.git-memory/` (the shared store's checkout, listed in `.git/info/exclude`). A teammate who clones the project runs plain `git-memory init`: it reads `git-memory.json`, clones the shared repo and wires their hooks. Their own memories stay personal.

```json
{
  "store": { "repo": "git@github.com:acme/ai-memory.git", "project": "my-project", "path": ".git-memory" },
  "scopes": { "payments": { "repo": "git@github.com:acme/payments-memory.git" } },
  "defaultScope": "personal"
}
```

- `store.repo`: any git URL or local path; one shared repo serves many projects, each under `projects/<project>/`. `null` means a `memory` branch of the project itself. The project scope exists only when a `store` section is present (or a store repo is set in `~/.git-memory/config.json`); a file that only lists `scopes` never creates one.
- Config layers (later wins): defaults, `~/.git-memory/config.json`, `git-memory.json`, machine-local `.git/git-memory/config.json`. Every option is in `defaultConfig` in `internal/gm/config.go`.

### Paths are always relative

Nothing git-memory writes into your project or into a config file contains an absolute path, so every one of those files can be committed or copied to another machine unchanged:

- `git-memory.json`: a local store is written relative to the project (`"repo": "../ai-memory.git"`, even if you typed an absolute path to `init --store`). URLs such as `git@github.com:acme/memory.git` are left as they are. The same goes for `scopes add --repo`, `--path`, and the per-user `~/.git-memory/config.json` (relative to its own directory, written by `scopes personal-repo` and `global-repo`). Relative paths are resolved when the config is loaded; absolute ones you write by hand still work.
- Hooks: Claude Code runs `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" hook …`, Copilot runs `./.git/git-memory/gm hook …` with `cwd` `.`.
- Slash commands and the `cli` the briefing shows: `./.git/git-memory/gm`, relative to the repository root.

The one place with machine-specific paths is the generated launcher `.git/git-memory/gm` itself (the absolute path of the binary, so agents launched without your shell's `PATH` still work, and the memory home when `GIT_MEMORY_HOME` was set). It lives under `.git`, is created by `init` on each machine, and is never committed.

## Scopes: who sees a memory, and where it lives

Every memory belongs to one scope. **New memories start in `personal`.** Reading merges all scopes that apply (each memory is tagged `{scope}` in the briefing); writing goes to one.

| Scope | Visible in | Stored in | Typical use |
| --- | --- | --- | --- |
| `personal` (default) | this project, only you | your personal store, `private/projects/<project>/` | your notes, quirks, session history |
| `global` | every project of yours | `global/` in your personal store (or in `global.repo`, e.g. an org repo) | your conventions and preferences |
| `<name>` | every project that joined it | `scopes/<name>/` in its own repo | a team, a product area, a client |
| `project` (opt-in) | this project, shared | the shared project store, `projects/<name>/` | decisions the whole team relies on |

`<project>` in the personal scope is a stable identity from the repo itself (origin remote name, else directory name), so renaming the shared store's folder never orphans your memories.

**Promote or not.** Nothing leaves `personal` unless you move it:

```bash
git-memory remember "Prefer small PRs" --kind decision          # personal (the default)
git-memory promote prefer-small-prs                              # → global (every project of yours)
git-memory promote prefer-small-prs --to payments                # → a named scope
git-memory promote personal:deploys --to project                 # → the team's shared store
git-memory move global:prefer-small-prs --to personal            # and back to private
git-memory remember "x" --scope global                           # or write straight to a scope
```

**Set the default scope** (where `remember` writes when no `--scope` is given). It is yours to choose, per machine, per user, or for a team:

```bash
git-memory scopes default global --local      # this machine + this project only (the default target)
git-memory scopes default global --personal   # all your projects (~/.git-memory/config.json)
git-memory scopes default project --shared    # the team's default, in the committed git-memory.json
```

**Decisions.** Scopes are just where a memory lives; what you put in them is up to you. For example, a team can keep its project-wide decisions in the `project` scope and its cross-project rules in a named scope. Every decision is listed in the briefing (not only the newest few), and pinned pages (`remember --core`) come first. When a decision changes, retire it instead of deleting it:

```bash
git-memory supersede use-rest --by use-graphql --reason "clients need flexible queries"
```

The old page stays in git (and findable with `search --all` / `list --all`), is linked to its replacement, and stops appearing in the briefing, recall and normal search. If the old page was pinned, its replacement is pinned too.

Other scope commands:

```bash
git-memory scopes                                 # where each scope lives, with page counts
git-memory scopes add payments --repo <url>       # join a named scope (--local / --personal to keep it to yourself)
git-memory scopes sessions project --local        # share automatic session pages (default: personal, since they contain your prompts)
git-memory scopes personal-repo <url>             # give your personal store a remote, so it follows you between machines
git-memory scopes global-repo <url>               # keep `global` in its own repo (e.g. an org-wide conventions repo)
git-memory sync                                   # rebases and pushes every store that has a remote
```

Pages are addressed by `scope:path`, e.g. `personal:memories/decision/use-pnpm.md`; a bare name works when it is unique across scopes. Your personal store is shared by all your projects on a machine; each distinct repo is cloned once, in `~/.git-memory/stores/`.

## Slash commands

`init` also installs six commands in both agents, so memory can be used from the chat without leaving it. They are plain files (Claude Code skills in `.claude/skills/`, Copilot prompt files in `.github/prompts/`), git-excluded by default like the hooks, regenerated by every `init`, and never written over a file of yours with the same name. `init --no-commands` skips them.

| Command | What it does | Claude Code can run it on its own? |
| --- | --- | --- |
| `/remember <what>` | Picks the kind, title and scope, then stores it | no (it writes) |
| `/decide [focus]` | Reviews this conversation, proposes decisions, procedures and gotchas, and saves the ones you approve | no |
| `/recall <topic>` | Searches memory and answers from it, citing references | yes (read-only, so it loads when you ask what was decided) |
| `/memories` | Scopes, health checks, and what a new session is told | no |
| `/promote <ref> [scope]` | Widens or narrows who sees a memory | no |
| `/forget <ref>` | Deletes a memory, or supersedes it with its replacement | no |

How they relate to the prompt triggers: `#remember …` and `decision: …` are instant and verbatim (no model); `/remember` and `/decide` let the model use the conversation, and `/decide` always asks you before saving. The commands call the CLI through a small wrapper script, `.git/git-memory/gm`. In Claude Code the wrapper is pre-approved for the turn that runs a command (`allowed-tools`), so there is no permission prompt; Copilot asks as usual for terminal commands. `/memories` is named that way because Claude Code has a built-in `/memory`.

**Safe to commit.** The files are the same on every machine: they call the CLI as `./.git/git-memory/gm` (relative to the repository root, where agents run their commands) and contain no home directory, binary path or memory home; those live in the wrapper script under `.git`, which is never committed. To share them with the team, `git add -f .claude/skills .github/prompts`: a teammate's `init` creates their own wrapper and regenerates byte-identical files, so nothing shows up as modified. (In a linked worktree the wrapper sits under the main repository's `.git`; the files say so for the agent.)

Copilot prompt files only run in Local chat sessions, and neither agent's docs say whether git-excluded command files are discovered; `doctor` shows how many are installed.

## Remembering from the prompt

Type the memory in your prompt and the hook stores it, with no model involved and no separate command. It works with Copilot and Claude Code, because both fire the prompt hook:

```text
remember that we deploy on Tuesdays only          → note
decision: use pnpm, never npm                     → decision
#fact the staging API listens on 8081             → fact
#procedure run make migrate before the tests      → procedure
```

Accepted markers, one memory per line: `remember that …`, `remember: …`, `#remember …` (notes); `decision: …`, `#decision …`; `#fact …`; `#procedure …`. Only these count, so ordinary prose (`Note: the build is red`, a pasted email) is never stored, and `remember to run the tests` is treated as an instruction for the task, not a memory. Secrets are redacted and `<private>` text dropped, as with every memory.

Saved memories go to your personal scope by default, tagged `auto` (review them with `git-memory list --tag auto`, then `promote` or `forget`). The hook still records your whole prompt in the session as usual.

```bash
git-memory auto                 # show the setting
git-memory auto off             # turn it off (default: on); --personal for all your projects, --shared for the team
git-memory auto scope project   # save them somewhere else, e.g. the shared project store
```

## How it works

```
 agent hooks ──► spool (.git/git-memory/spool/*.jsonl)        cheap append, ~13 ms per hook
                    │  Stop / PreCompact / SessionEnd
                    ▼
        session page  sessions/YYYY/MM/<date>-<agent>-<id>.md   committed on branch `memory`
                    │
   SessionStart ◄───┴── briefing: core notes + recent decisions + last sessions (≤ 6 KB)
   UserPromptSubmit ◄── recall: BM25 hits for this prompt          (Claude Code only, see below)
   agent CLI ◄───────── git-memory search / show / remember       (both agents)
```

- **Pages** are markdown with frontmatter (`kind`: session, decision, procedure, fact, note; `tier`: episodic, semantic, core). Browse them on GitHub like any other branch.
- **Capture records intent, not output.** Prompts, files edited, shell commands, failures and the agent's final message. Tool output is never stored.
- **Privacy.** Secrets are redacted before anything is written (API keys, tokens, JWTs, private keys, `KEY=value` assignments, URL credentials). Text inside `<private>…</private>` is dropped. `capture.exclude` globs keep files like `.env` out of the record. Set `GIT_MEMORY_DISABLE=1` to turn capture off.
- **Fail-open.** A hook error is logged to `.git/git-memory/errors.log` and the hook exits 0; it never blocks the agent.
- **Durable memory** is written with `git memory remember "..." --kind decision --title "..."`. `--core` pages are injected into every session.
- **Sync.** `git memory sync` rebases on the remote and pushes. In branch mode all machines derive the same root commit, so clones initialised independently still share history. Set `"autoPush": true` to sync in the background at session end. Sessions get unique file names, so rebases rarely conflict.

## Agent support

| | Claude Code (CLI + VS Code extension) | GitHub Copilot (VS Code, preview) |
| --- | --- | --- |
| Capture prompts, edits, commands | yes | yes |
| Briefing at session start | yes (`hookSpecificOutput.additionalContext`) | yes (`additionalContext`) |
| Per-prompt recall injection | yes | **no**: Copilot drops `userPromptSubmitted` output for config-file hooks, so recall happens when the agent runs `git memory search`, as the briefing tells it to |
| Final message captured | yes (`last_assistant_message` / transcript) | best effort via the transcript path |

No instruction files are added to the project. The session-start briefing itself tells the agent how to run `search` and `remember`, which is what Copilot relies on in place of per-prompt recall. Copilot also reads `.claude/settings*.json`; duplicate events inside the same session are de-duplicated.

## CLI

```
git-memory init | search <q> | remember <text> | show <page> | list | forget <page>
git-memory briefing | log | flush | sync | status
```

Config lives in `git-memory.json` (see `defaultConfig` in `internal/gm/config.go` for every option). To get LLM summaries without any server, set for example `"summarizer": "claude -p 'Summarize this coding session in 5 bullets'"`.

## Caveats

- The Copilot hook payloads and output shapes follow GitHub's published hooks reference. They are covered by tests that replay those payloads, but have not been run against a live VS Code Copilot session yet. Hooks are in preview there, so check `.git/git-memory/errors.log` if a session leaves no page.
- The Windows binaries are cross-compiled and vetted, and their path, process and shell handling is written for Windows, but they have not been run on Windows yet. Unverified there: how Claude Code and Copilot run the hook `command` (the files use `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm.exe" hook …` and `./.git/git-memory/gm.exe hook …`), and quoting for project paths with spaces. If hooks leave no page on Windows, check `.git/git-memory/errors.log` and run `git-memory doctor`.
- A dedicated store is cloned by `init`. On a new machine run `init` once interactively, so the clone doesn't happen inside a 30 s hook timeout.
- In branch mode the `memory` branch is checked out in `.git-memory/`, so `git checkout memory` in the main checkout is refused.
- Search is lexical (BM25 + recency + kind weighting). There are no embeddings.

## Development

Go (≥ 1.22) and git are all you need.

```bash
make build      # bin/git-memory for this machine
make test       # go vet + go test (unit tests and end-to-end tests: the real binary against real git repos in temp dirs)
make release    # release/git-memory_<os>_<arch> for Linux and Windows, amd64 and arm64, plus SHA256SUMS
```

Layout: `cmd/git-memory` is the entry point; everything else is in `internal/gm`. The end-to-end tests (`e2e_test.go`) build the binary once and drive it exactly as an agent or a person would: `init`, hooks fed on stdin, the installed launcher, and the CLI.
