---
name: memory-list
description: List the memories of a scope (claude, copilot, personal, or a folder of the memory repository), or the project's sessions, and say what each one is for.
hint: [claude|copilot|personal|sessions|<scope>] [date]
---

List the memories of one scope and summarize what each is for. The scope is the text typed after the command; with none, cover every scope.

1. Map the scope to a listing:
   - `claude`: `{GM} list claude` (Claude Code's own memory for this project).
   - `copilot`: `{GM} list copilot` (this workspace's and the user's).
   - `personal` (or `global`): the memories that apply to every project: run `{GM} list repo` and keep the group headed `.git-memory · global`.
   - `project` (or `repo`): the same output, the group headed `.git-memory · this project`.
   - `sessions`: this project's session pages, the record of each agent session (not memories), newest first: `{GM} list sessions`. A date narrows it to a period: `{GM} list sessions 2026-11-13` (a day), `2026-11` (a month) or `2026` (a year). If the user words it differently (today, yesterday, last month, 12 November), convert it to one of those forms; a date on its own means `sessions`.
   - anything else is a scope of the project, a folder under `scopes/`: run `{GM} doctor --report` for the `Memory repository:` path and the `Project folder:` (`.` is the repository's top), then `{GM} ls-tree -r --name-only HEAD -- <project folder>/scopes/<scope>` (and `{GM} status -s -- <project folder>/scopes/<scope>` for pages not committed yet); only the pages under `memories/` and `core/` count. If nothing matches, say the scope does not exist and name the ones that do: `sessions`, the groups `{GM} list` prints, plus the folders under `<project folder>/scopes/`.
   - no scope: run `{GM} list` and handle each group in turn.
   With a memory scope chosen (`{GM} doctor --report` shows the `Scope:` row), `{GM} list` shows only that scope's memories; add `--all-scopes` (`{GM} list repo --all-scopes`) only when the user asks to see every scope or names another scope.
2. Every row is `kind  title  name  — note`. The note is the page's own description, when it has one. If it already says what the memory is for, use it. Otherwise read the page (`{GM} show HEAD:<path>` for a committed page of the memory repository; the file itself for an uncommitted one, which the session in progress always is, under the `Memory repository:` path from `{GM} doctor --report`; and for an agent's memory the file itself too: the group heading shows its folder, the name is a path inside it). A session's note is only its date and agent, so read the page and use its `## Outcome` section for what the session did and write one line of your own. Read only what you need: with more than about 15 memories, read the ones whose title does not explain itself.
3. Present each group as a list: the title, its kind, and what it is for in one short sentence, i.e. when it would matter to someone working on the project (for a session: its date, and what was done or decided in it). Mark pinned and superseded ones. Never invent: if a page is empty or unclear, say so. Do not change anything.
4. Finish with the count, and offer `/memory-recall <topic>` to read one in depth and `/memory-promote` (e.g. `claude:<file>` into the project) or `/memory-forget` to curate.
