// Package hook is what the agents' hooks run: it turns Claude Code, Copilot, Codex, Cursor and Gemini CLI events into
// session pages, briefs a new session from what is stored, recalls memories for a prompt and saves "remember that ..."
// lines. It never fails the agent: errors are logged and the exit code stays 0.
//
// The engine here is shared. What differs between the agents is in their own packages (claude, copilot, codex, cursor,
// gemini), and each event has its own file: start, prompt, tool, stop, precompact, end.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
	"github.com/Rickedb/git-memory/internal/gm/hook/claude"
	"github.com/Rickedb/git-memory/internal/gm/hook/codex"
	"github.com/Rickedb/git-memory/internal/gm/hook/copilot"
	"github.com/Rickedb/git-memory/internal/gm/hook/cursor"
	"github.com/Rickedb/git-memory/internal/gm/hook/gemini"
)

// Agents are the agents git-memory serves, in the order `agent init` wires them.
var Agents = []agent.Agent{claude.Agent, copilot.Agent, codex.Agent, cursor.Agent, gemini.Agent}

// Lookup finds an agent by the name used in `agent hook <name> <event>`.
func Lookup(name string) (agent.Agent, bool) {
	for _, a := range Agents {
		if a.Name == name {
			return a, true
		}
	}
	return agent.Agent{}, false
}

// Run is the entry point of `agent hook <agent> <event>`: it reads the event on stdin and prints what the agent should see.
func Run(name, event string) {
	payload := readStdinJSON()
	err := core.CatchAll(func() {
		ag, ok := Lookup(name)
		if !ok {
			core.Fail(`unknown agent "%s"`, name)
		}
		if o := handle(ag, event, payload); o != nil {
			fmt.Println(core.MarshalJSON(o, false))
		}
	})
	if err != nil {
		// Hooks must never break the agent: log and exit cleanly.
		logError(workDir(payload), err)
	}
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice != 0
}

func readStdinJSON() core.HookPayload {
	if isTTY(os.Stdin) {
		return core.HookPayload{}
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return core.HookPayload{}
	}
	var p core.HookPayload
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return core.HookPayload{}
	}
	return p
}

// logError appends to the project's .git/git-memory/errors.log; outside a repo there is nothing sensible to do.
func logError(cwd string, err error) {
	_ = core.CatchAll(func() {
		base := core.Locate(cwd)
		core.MkdirAll(base.Data)
		f, ferr := os.OpenFile(filepath.Join(base.Data, "errors.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		core.Check(ferr)
		defer f.Close()
		_, werr := f.WriteString(core.Iso(time.Now()) + " " + err.Error() + "\n")
		core.Check(werr)
	})
}
