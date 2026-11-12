// Command git-memory is the CLI and the agent hook entry point. All logic lives in internal/gm.
package main

import (
	"os"

	"github.com/Rickedb/git-memory/internal/gm"
)

func main() {
	os.Exit(gm.Main(os.Args[1:]))
}
