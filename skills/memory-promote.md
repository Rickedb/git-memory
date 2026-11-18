---
name: memory-promote
description: Bring a memory into this project's folder or the global one: from the agents' own memories, any markdown file, or between the project and global.
hint: [page or description] [project|global]
writes: true
scope: true
---

Promote a memory into the memory repository. Arguments: what to promote and where (`project` is this project's folder, `global` applies to every project). What can be promoted: a page already in the memory repository (it is moved), one of the agents' own memories (`claude:<file>`, `copilot:<file>`, `copilot-user:<file>`; it is copied, the original stays) or any markdown file (copied).

1. Run `{GM} doctor --report`: it lists the agents' own memories that were found, with their file names, and the folders to read and write (`Read folders:`, `Write folder:`). If the argument is not an exact reference, find the page (`{GM} grep` for the memory repository, the doctor listing for the agents' memories). If several match, ask which one.
2. Confirm the target with the user: promoting to `global` puts it in every project's briefing. If the user gave no target, ask.
3. Run `{GM} promote <source> --to <project|scope|global>` (`scope` is the scope the user works in, when one is chosen) and report the path it printed. Secrets are redacted on the way in, and promoting the same file twice is refused.
