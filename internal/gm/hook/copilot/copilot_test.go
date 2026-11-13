package copilot

import (
	"encoding/json"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

func TestCopilotNamesItsEvents(t *testing.T) {
	for in, want := range map[string]agent.Kind{
		"sessionStart": agent.Start, "SessionStart": agent.Start, "userPromptSubmitted": agent.Prompt, "postToolUse": agent.Tool,
		"agentStop": agent.Stop, "preCompact": agent.PreCompact, "sessionEnd": agent.End,
	} {
		if got, ok := Agent.Kind(in); !ok || got != want {
			t.Errorf("Kind(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"Stop", "UserPromptSubmit", "PostToolUseFailure", "nope", ""} {
		if _, ok := Agent.Kind(in); ok {
			t.Errorf("Kind(%q) accepted: it is not a Copilot event", in)
		}
	}
}

func TestCopilotHooksPrintPlainAdditionalContext(t *testing.T) {
	b, _ := json.Marshal(Agent.Context(agent.Start, "hello"))
	if string(b) != `{"additionalContext":"hello"}` {
		t.Errorf("Context = %s", b)
	}
	if Agent.Context(agent.Start, "") != nil {
		t.Error("an empty context must print nothing")
	}
	if Agent.Recall {
		t.Error("Copilot's prompt hook cannot hand context back")
	}
}

func TestCopilotRunsTheLauncherByARelativePath(t *testing.T) {
	got := Agent.Command(".git/git-memory/gm", "agentStop")
	if want := `./.git/git-memory/gm agent hook copilot agentStop`; got != want {
		t.Errorf("Command = %s, want %s", got, want)
	}
	if !agent.IsOurs(got) {
		t.Error("our own command must be recognised as ours")
	}
}
