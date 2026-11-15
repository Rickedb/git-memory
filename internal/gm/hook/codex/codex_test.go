package codex

import (
	"encoding/json"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

func TestCodexNamesItsEvents(t *testing.T) {
	for in, want := range map[string]agent.Kind{
		"SessionStart": agent.Start, "UserPromptSubmit": agent.Prompt, "PostToolUse": agent.Tool,
		"Stop": agent.Stop, "PreCompact": agent.PreCompact, "SessionEnd": agent.End,
	} {
		if got, ok := Agent.Kind(in); !ok || got != want {
			t.Errorf("Kind(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := Agent.Kind("PostToolUseFailure"); ok {
		t.Error("Codex has no failed-tool hook")
	}
}

func TestCodexPrintsTheContextTaggedWithTheEvent(t *testing.T) {
	b, _ := json.Marshal(Agent.Context(agent.Prompt, "hello"))
	if want := `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"hello"}}`; string(b) != want {
		t.Errorf("Context = %s, want %s", b, want)
	}
}

func TestCodexFindsTheRepositoryRootBeforeRunningTheLauncher(t *testing.T) {
	got := Agent.Command(".git/git-memory/gm", "Stop")
	if want := `"$(git rev-parse --show-toplevel)/.git/git-memory/gm" agent hook codex Stop`; got != want {
		t.Errorf("Command = %s, want %s", got, want)
	}
	if !agent.IsOurs(got) {
		t.Error("our own command must be recognised as ours")
	}
}
