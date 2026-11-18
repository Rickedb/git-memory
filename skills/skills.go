// Package skills holds the text of the slash commands (skills, prompts) that `git-memory agent init` writes into a
// project: one markdown file per command, plus the notes shared by them (the files that start with an underscore).
// They are embedded in the binary, so editing a file here and rebuilding is all it takes to change a command.
//
// A command file is a small frontmatter block and the body the agent follows:
//
//	---
//	name: memory-remember          the command's name (it is the file's name too)
//	description: ...               what the agent's menu shows
//	hint: [what to remember]       optional: what to type after the command
//	writes: true                   optional: only the user may run it, never the model on its own
//	scope: true                    optional: add the note on switching scopes for one request (_scope.md)
//	---
//
// The body is written for the agent. {GM} is the CLI, {AGENTS} what `agent init` takes.
package skills

import "embed"

//go:embed *.md
var FS embed.FS
