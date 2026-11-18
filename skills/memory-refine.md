---
name: memory-refine
description: Refine one memory: make it clearer, more complete or current, keeping its meaning, after the user approves the rewrite.
hint: [page or description] [what to improve]
writes: true
---

Improve one existing memory page. Arguments: a page path or words describing it, and optionally what to improve ("add the reason", "it is out of date", "shorter"). Only a memory (`decision`, `procedure`, `fact` or `note`): session pages are the record of what happened and are never edited.

1. Run `{GM} doctor --report` for the folders (`Read folders:`, `Write folder:`). Find the page: use the path if one was given, otherwise `{GM} grep -n -i -I -e "<keyword>" -- <the read folders>` (trying synonyms), and ask which one when several match. Read it with `{GM} show HEAD:<path>` (the file itself, under the `Memory repository:` path, if it is not committed yet) and, if its frontmatter says `status: superseded`, say so and stop unless the user still wants it changed.
2. Work out what is wrong with it. If the user said what to improve, do that. Otherwise look for: a decision with no reason (or no mention of what was rejected); text that only makes sense inside the conversation it came from (make it self-contained); wording that is vague, long or repeats itself; a missing or wrong `kind` or `tags`; a claim about the code, a file or a command that you can check in this repository and find stale (check it, do not guess). Never add facts that nobody told you and that you cannot see in the project: if something is missing, ask the user.
3. Keep the meaning. If the change reverses the memory (the decision is now the opposite, the procedure is different), it is not a refinement: say so and offer `/memory-forget` (retire the old page, `status: superseded`) plus `/memory-remember` for the new one. If it contains something that looks like a secret or credential, remove it.
4. Show the user the current text and the proposed text side by side (or as a diff), say in one line what changed and why, and ask whether to apply it, change it or leave it. Do not write anything before they answer.
5. When they approve, edit the page in place: keep its path, `kind`, `tier` and `created`, set `updated: <current UTC time, ISO 8601>` (add the line if there is none), change only what was agreed, and keep the `# <title>` unless the title was the point. Then commit just that page: `{GM} add -- <path>` and `{GM} commit -m "memory: refine <title>" -- <path>`. Report the path and what changed.
