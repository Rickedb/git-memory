package agent

import (
	"testing"
)

func TestIsOursMatchesOldAndNewHookCommands(t *testing.T) {
	for _, c := range []string{
		`"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" agent hook claude Stop`,
		`./.git/git-memory/gm agent hook copilot agentStop`,
		`./.git/git-memory/gm.exe agent hook copilot agentStop`,
		`"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" hook claude Stop`, // hook files of earlier builds, replaced on the next `agent init`
		`./.git/git-memory/gm hook copilot agentStop`,
		`node "$CLAUDE_PROJECT_DIR/.git/git-memory/runtime/cli.js" hook claude Stop`,
		`node ./.git/git-memory/runtime/cli.js hook copilot Stop`,
		`node .claude/git-memory.mjs hook claude Stop`,
		`git-memory hook claude Stop`,
	} {
		if !IsOurs(c) {
			t.Errorf("not recognized: %s", c)
		}
	}
	for _, c := range []string{`echo hook claude`, `./scripts/other.sh hook x`, `prettier --write`} {
		if IsOurs(c) {
			t.Errorf("wrongly claimed: %s", c)
		}
	}
}
