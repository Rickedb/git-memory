package hook

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

func textOf(c any) string {
	if s, ok := c.(string); ok {
		return s
	}
	list, ok := c.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, x := range list {
		if pick(x, "type") == "text" {
			if t := pickStr(x, "text"); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// lastAssistantText is the agent's final message: sent in the payload, or read from the tail of the transcript.
func lastAssistantText(payload core.HookPayload) string {
	if direct := pickStr(payload, "last_assistant_message", "lastAssistantMessage", "response"); strings.TrimSpace(direct) != "" {
		return direct
	}
	tp := pickStr(payload, "transcript_path", "transcriptPath")
	if tp == "" {
		return ""
	}
	f, err := os.Open(tp)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	n := st.Size()
	if n > 256*1024 {
		n = 256 * 1024
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, st.Size()-n); err != nil && err != io.EOF {
		return ""
	}
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var o any
		if json.Unmarshal([]byte(lines[i]), &o) != nil {
			continue // partial first line of the tail window
		}
		message := pick(o, "message")
		isAssistant := pick(o, "type") == "assistant" || pick(o, "role") == "assistant" || pick(message, "role") == "assistant"
		if !isAssistant {
			continue
		}
		t := textOf(coalesce(pick(message, "content"), pick(o, "content"), pick(pick(o, "data"), "content"), pick(o, "text")))
		if strings.TrimSpace(t) != "" {
			return t
		}
	}
	return ""
}
