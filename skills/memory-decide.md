---
name: memory-decide
description: Capture the decisions, procedures and gotchas from this conversation into project memory, after the user approves them.
hint: [optional focus]
writes: true
scope: true
---

Capture what this conversation settled, so future sessions start informed. Focus: the text typed after the command, or the whole conversation if there is none.

1. Review the conversation. Collect only what will still matter later: decisions that were made (what, and why, and what was rejected), project procedures or conventions that were agreed, non-obvious facts and gotchas. Skip task progress, the code that was written, and anything temporary or obvious.
2. Show a numbered list. For each item give the kind (`decision`, `procedure`, `fact` or `note`), a short title, one or two self-contained sentences, and whether it applies to this project (the default) or to every project. Never include secrets.
3. Ask which items to save and whether anything should change. Do not store anything before the user answers.
4. Run `{GM} doctor --report` for the folders (`Memory repository:`, `Write folder:`, `Global folder:`). For each approved item write `<repo>/<folder>/memories/<kind>/<title-slug>.md` as: a frontmatter block with `kind: <kind>`, `tier: semantic`, `tags: []` and `created: <current UTC time, ISO 8601>`, then `# <title>`, a blank line and the text. Then commit them: `{GM} add -A -- <folder>/memories` and `{GM} commit -m "memory: decisions from this session" -- <folder>/memories`. Report the paths. If nothing qualifies, say so instead of inventing items.
