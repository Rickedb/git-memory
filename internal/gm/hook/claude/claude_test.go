package claude

import (
	"encoding/json"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/hook/agent"
)

func TestClaudeNamesItsEvents(t *testing.T) {
	for in, want := range map[string]agent.Kind{
		"SessionStart": agent.Start, "session_start": agent.Start, "UserPromptSubmit": agent.Prompt, "PostToolUse": agent.Tool,
		"PostToolUseFailure": agent.ToolFail, "Stop": agent.Stop, "PreCompact": agent.PreCompact, "SessionEnd": agent.End,
	} {
		if got, ok := Agent.Kind(in); !ok || got != want {
			t.Errorf("Kind(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"agentStop", "userPromptSubmitted", "nope", ""} {
		if _, ok := Agent.Kind(in); ok {
			t.Errorf("Kind(%q) accepted: it is not a Claude Code event", in)
		}
	}
}

func TestClaudeHooksPrintTheContextTaggedWithTheEvent(t *testing.T) {
	for kind, name := range map[agent.Kind]string{agent.Start: "SessionStart", agent.Prompt: "UserPromptSubmit"} {
		b, _ := json.Marshal(Agent.Context(kind, "hello"))
		want := `{"hookSpecificOutput":{"hookEventName":"` + name + `","additionalContext":"hello"}}`
		if string(b) != want {
			t.Errorf("Context(%q) = %s, want %s", kind, b, want)
		}
	}
	if Agent.Context(agent.Start, "") != nil {
		t.Error("an empty context must print nothing")
	}
	if !Agent.Recall {
		t.Error("Claude Code's prompt hook takes recalled memories")
	}
}

func TestClaudeRunsTheLauncherThroughItsProjectDir(t *testing.T) {
	got := Agent.Command(".git/git-memory/gm", "Stop")
	want := `"$CLAUDE_PROJECT_DIR/.git/git-memory/gm" agent hook claude Stop`
	if got != want {
		t.Errorf("Command = %s, want %s", got, want)
	}
	if !agent.IsOurs(got) {
		t.Error("our own command must be recognised as ours")
	}
}
