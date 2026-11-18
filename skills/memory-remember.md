---
name: memory-remember
description: Store something in this project's memory: a decision, procedure, fact or note.
hint: [what to remember]
writes: true
scope: true
---

Store what the user wants remembered: the text they typed after the command, or what they point at ("the thing we just discussed").

1. Pick the kind: `decision` (a choice, with the reason), `procedure` (how to do something in this project), `fact` (something true about it) or `note` (anything else).
2. Write it as one or two self-contained sentences that make sense without this conversation. Never include secrets or credentials. Choose a short title.
3. Run `{GM} doctor --report`. Its `Memory repository:` line says where the memory repository is, `Write folder:` is where this project's new memories go (the scope the user works in, when one is chosen) and `Global folder:` is the one for memories apart from the project's own. Use the write folder unless the user said it applies everywhere (then the global folder).
4. Write the page to `<repo>/<folder>/memories/<kind>/<title-slug>.md` (lowercase words joined by dashes), exactly in this shape:

   ---
   kind: <kind>
   tier: semantic
   tags: []
   created: <current UTC time, ISO 8601>
   ---

   # <title>

   <text>

   Use `tier: core` only if the user asked to pin it (pinned pages open every briefing).
5. Commit just that page: `{GM} add -A -- <folder>/memories` then `{GM} commit -m "memory: <title>" -- <folder>/memories`. Tell the user the page's path.
