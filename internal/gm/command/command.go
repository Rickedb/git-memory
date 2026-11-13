// Package command implements git-memory's own commands: doctor, list, promote, demote and `agent init` (with the
// slash commands it installs and the agents' memory folders it reads). Everything else the user types is git's.
// The command line itself (routing to git, the help, dispatch) is package gm.
package command

import "fmt"

// out prints a line of a command's report.
func out(a ...any) { fmt.Println(a...) }
