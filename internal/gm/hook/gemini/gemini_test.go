package gemini

import (
	"encoding/json"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

func TestGeminiNamesItsEvents(t *testing.T) {
	for in, want := range map[string]agent.Kind{
		"SessionStart": agent.Start, "BeforeAgent": agent.Prompt, "AfterTool": agent.Tool,
		"AfterAgent": agent.Stop, "PreCompress": agent.PreCompact, "SessionEnd": agent.End,
	} {
		if got, ok := Agent.Kind(in); !ok || got != want {
			t.Errorf("Kind(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"Stop", "UserPromptSubmit", "PostToolUse"} {
		if _, ok := Agent.Kind(in); ok {
			t.Errorf("Kind(%q) accepted: it is not a Gemini CLI event", in)
		}
	}
}

func TestGeminiPrintsPlainAdditionalContextUnderHookSpecificOutput(t *testing.T) {
	b, _ := json.Marshal(Agent.Context(agent.Prompt, "hello"))
	if want := `{"hookSpecificOutput":{"additionalContext":"hello"}}`; string(b) != want {
		t.Errorf("Context = %s, want %s", b, want)
	}
}

func TestGeminiRunsTheLauncherThroughItsProjectDirectory(t *testing.T) {
	got := Agent.Command(".git/git-memory/gm", "AfterAgent")
	if want := `"$GEMINI_PROJECT_DIR/.git/git-memory/gm" agent hook gemini AfterAgent`; got != want {
		t.Errorf("Command = %s, want %s", got, want)
	}
	if !agent.IsOurs(got) {
		t.Error("our own command must be recognised as ours")
	}
}
