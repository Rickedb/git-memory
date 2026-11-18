---
name: memory-forget
description: Delete a memory, or retire a decision that has changed.
hint: [page or description]
writes: true
---

Remove or retire a memory. Arguments: a page path, or words describing it.

1. Run `{GM} doctor --report` for the folders, find the page with `{GM} grep` and read it with `{GM} show HEAD:<path>`. Confirm with the user that it is the right page.
2. Ask what they want:
   - **retire**: the decision changed. Edit the page's frontmatter: add `status: superseded` (and `superseded_by: <path of the replacement>` if there is one). It stays in git, but leaves briefings and recall. Recommend this for anything that was once true.
   - **delete**: it was a mistake. Run `{GM} rm <path>`. The history keeps it.
3. Do exactly that, commit with `{GM} commit -m "memory: <what you did>" -- <path>`, then report the result.
