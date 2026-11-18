---
name: memory-status
description: Show what project memory holds: where it lives, how healthy it is, and what changed recently.
---

Summarize the project's memory for the user. Run `{GM} doctor --report`, `{GM} log --oneline -n 10` and `{GM} status -sb` (do not chain them; a repository with no commits yet makes `log` fail, which just means nothing was committed), then report:

- the version and anything unhealthy in the checks, with the fix; where the memory repository is, and how many pages the project and global folders hold
- what was committed recently
- anything uncommitted or off (the user may be mid-edit in the repository)

Offer `/memory-recall <topic>` to dig into something, and `/memory-promote` or `/memory-forget` to curate.
