---
name: memory-sync
description: Share your memory with the team: merge the default branch into your branch, push it, and settle any conflict between pages yourself, asking only when in doubt.
writes: true
---

Bring your memory branch together with the team's and share it. Each person commits to a branch named after their git user name; `{GM} sync` fetches the remote, merges the default branch (usually `main`) into yours and pushes yours to the default branch. Your job is to settle whatever conflicts, so the user is only asked about what a careful teammate would also ask about.

1. Run `{GM} status --short`. If pages are uncommitted, commit them (`{GM} add <files>`, `{GM} commit -m "memory: <what>"`) unless they look like the user's unfinished work: then ask.
2. Run `{GM} sync`. Exit 0: report what it merged and pushed, and stop.
3. Exit 2 means a merge stopped on conflicts and waits for you (the files are listed, with why). For each one:
   - Read the three versions: `{GM} show :1:<file>` (common ancestor), `:2:<file>` (the user's side), `:3:<file>` (the team's), and the file itself with its markers (the `|||||||` block is the ancestor). Use `{GM} log --oneline -n 5 origin/<default> -- <file>` to see what the team changed and why.
   - **Settle it when the answer is clear**, keeping everything either side added:
     - frontmatter: `created` the earlier one, `updated` the later one, `tags` the union, `kind` and `tier` the ones both agree on;
     - both sides added different lines, bullets or sections: keep both, in a sensible order;
     - both sides made the same fact or edit: keep one;
     - one side only reformatted, moved or renamed and the other changed content: apply the change to the moved page;
     - two new pages with the same name about different things: keep both, give the newer a distinct title;
     - two sessions' notes on the same subject that agree: merge them into one page.
   - **Ask the user instead** (with both versions quoted, and what you would do) when:
     - a decision, procedure or fact is contradicted: the two sides cannot both be true;
     - one side deleted a page the other changed;
     - a pinned note in `core/` changed in substance on both sides;
     - you cannot tell what a side meant, or settling it would mean dropping information.
   Never settle a file by taking one whole side (`checkout --ours/--theirs`) just to make it go away, and never leave conflict markers in a page.
   Write the resolved file, then `{GM} add <file>`. Settle every file you are sure of before asking about the rest, and ask all the doubts in one message.
4. When no conflict is left, run `{GM} sync --continue`: it commits the merge and pushes. If it stops again (someone pushed meanwhile), repeat from step 3. If the user would rather not merge now, `{GM} sync --abort` puts the branch back as it was.
5. Report in a few lines: what was merged, which conflicts you settled and how (one line each), what you asked the user, and that the push went through (or why not). If the remote refuses the push to the default branch (a protected branch), the merged branch is ready: say so, and that it needs a pull request.
