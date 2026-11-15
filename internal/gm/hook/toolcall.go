package hook

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

var (
	editTool  = regexp.MustCompile(`(?i)(edit|write|create|replace|patch|insert|rename|delete|remove)`)
	shellTool = regexp.MustCompile(`(?i)(bash|shell|terminal|run_command|powershell|execute)`)
	patchFile = regexp.MustCompile(`(?m)^\*\*\* (?:Update|Add|Delete) File: (.+)$`)
)

func relFile(root string, p any) string {
	s, ok := p.(string)
	if !ok || s == "" {
		return ""
	}
	abs := s
	if !filepath.IsAbs(s) {
		abs = filepath.Join(root, s)
	}
	rel, err := core.RelPath(root, abs)
	if err != nil {
		return ""
	}
	rel = core.Slash(rel)
	if strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel // "" when p is the project root itself: not a file
}

// summarizeTool reduces an agent-specific tool call to what is worth remembering. Tool output is never stored.
func summarizeTool(payload core.HookPayload, root string, cfg core.Config) *core.Event {
	tool := ""
	if v := pick(payload, "tool_name", "toolName"); v != nil {
		tool, _ = v.(string)
	}
	input := asObject(pick(payload, "tool_input", "toolArgs", "toolInput"))
	result := asObject(pick(payload, "tool_result", "toolResult", "tool_response"))
	resultType := strings.ToLower(pickStr(result, "result_type", "resultType"))
	failed := resultType == "failure" || resultType == "error" || resultType == "denied" || result["is_error"] == true || (result["error"] != nil && result["error"] != "")

	if shellTool.MatchString(tool) {
		command := pickStr(input, "command", "cmd", "raw")
		if command == "" {
			return nil
		}
		return &core.Event{Ev: "tool", Tool: tool, Command: core.Clean(command, 300), Failed: failed}
	}
	if editTool.MatchString(tool) {
		cands := []any{pick(input, "file_path", "filePath", "path", "file", "notebook_path")}
		if patch, ok := pick(input, "input", "patch", "command", "raw").(string); ok {
			for _, m := range patchFile.FindAllStringSubmatch(patch, -1) {
				cands = append(cands, strings.TrimSpace(m[1]))
			}
		}
		var files []string
		for _, c := range cands {
			f := relFile(root, c)
			if f != "" && !core.Contains(files, f) && !core.IsExcluded(f, cfg.Capture.Exclude) {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			return nil
		}
		return &core.Event{Ev: "tool", Tool: tool, Files: files, Failed: failed}
	}
	return nil
}
