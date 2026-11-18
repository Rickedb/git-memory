---
name: memory-recall
description: Search this project's memory (decisions, procedures, facts, past sessions) and answer from it. Use when the user asks what was decided, agreed or done earlier about something.
hint: [topic]
---

Look up what the project memory says about the topic the user gave (the text after the command, or the topic they asked about).

1. Run `{GM} doctor --report` for the folders to read (`Read folders:`: the project, the scope the user works in when one is chosen, and the global folder). If a scope is chosen, stay in it: read beyond it only when the user's request asks for it ("all scopes", or another scope's name): then search the project's memories as a whole (`memories`, `core` and `scopes` of the project folder), or that scope's folder (`<project folder>/scopes/<name>`). Then run `{GM} grep -n -i -I -e "<keyword>" -- <the read folders>` for a few keywords of the topic, trying synonyms if nothing matches. Pages with `status: superseded` are retired decisions: skip them unless the user asks about history. Past sessions are kept out of git, so `grep` does not search them: `{GM} list sessions` lists them (`{GM} list sessions 2026-11` narrows it to a period), and each is a plain file under `.git-memory/` that you can read.
2. For the most relevant hits, read the page with `{GM} show HEAD:<path>` (a session page is a plain file: read it from `.git-memory/<path>`).
3. Answer from what the pages say and cite each by its path, e.g. `memories/decision/use-pnpm.md`. If nothing relevant is stored, say so plainly; do not guess or fill in from general knowledge.
