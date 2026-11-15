package cursor

import (
	"encoding/json"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

func TestCursorNamesItsEvents(t *testing.T) {
	for in, want := range map[string]agent.Kind{
		"sessionStart": agent.Start, "beforeSubmitPrompt": agent.Prompt, "postToolUse": agent.Tool, "postToolUseFailure": agent.ToolFail,
		"stop": agent.Stop, "preCompact": agent.PreCompact, "sessionEnd": agent.End,
	} {
		if got, ok := Agent.Kind(in); !ok || got != want {
			t.Errorf("Kind(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestCursorOnlyBriefsAtTheStart(t *testing.T) {
	b, _ := json.Marshal(Agent.Context(agent.Start, "hello"))
	if want := `{"additional_context":"hello"}`; string(b) != want {
		t.Errorf("Context = %s, want %s", b, want)
	}
	if Agent.Context(agent.Prompt, "hello") != nil || Agent.Recall {
		t.Error("Cursor's prompt hook cannot hand context back")
	}
}

func TestCursorRunsTheLauncherFromTheProjectRoot(t *testing.T) {
	got := Agent.Command(".git/git-memory/gm", "stop")
	if want := `./.git/git-memory/gm agent hook cursor stop`; got != want {
		t.Errorf("Command = %s, want %s", got, want)
	}
	if !agent.IsOurs(got) {
		t.Error("our own command must be recognised as ours")
	}
}
