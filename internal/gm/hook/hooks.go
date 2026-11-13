// Package hook is what the agents' hooks run: it turns Claude Code and Copilot events into session pages, briefs a new
// session from what is stored, recalls memories for a prompt and saves "remember that ..." lines. It never fails the
// agent: errors are logged and the exit code stays 0.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

// hookEvent is the agent-independent name of what happened.
type hookEvent string

const (
	evStart      hookEvent = "start"
	evPrompt     hookEvent = "prompt"
	evTool       hookEvent = "tool"
	evToolFail   hookEvent = "toolfail"
	evStop       hookEvent = "stop"
	evPreCompact hookEvent = "precompact"
	evEnd        hookEvent = "end"
)

// Claude Code and Copilot name the same moments differently (SessionStart vs sessionStart, userPromptSubmit vs
// userPromptSubmitted, Stop vs agentStop); names are compared lowercase without `_` and `-`.
var eventNames = map[string]hookEvent{
	"sessionstart":        evStart,
	"userpromptsubmit":    evPrompt,
	"userpromptsubmitted": evPrompt,
	"posttooluse":         evTool,
	"posttoolusefailure":  evToolFail,
	"stop":                evStop,
	"agentstop":           evStop,
	"precompact":          evPreCompact,
	"sessionend":          evEnd,
}

func normalizeEvent(name string) (hookEvent, bool) {
	key := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
	ev, ok := eventNames[key]
	return ev, ok
}

// pick returns the first non-null value among keys: payload field names differ between agents (snake_case vs camelCase).
func pick(o any, keys ...string) any {
	m, ok := o.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range keys {
		if v := m[k]; v != nil {
			return v
		}
	}
	return nil
}

func pickStr(o any, keys ...string) string {
	s, _ := pick(o, keys...).(string)
	return s
}

// coalesce returns the first non-nil value.
func coalesce(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// asObject reads a tool payload part that may arrive as an object or as a JSON string.
func asObject(v any) core.Obj {
	switch x := v.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(x), &parsed); err != nil {
			return core.Obj{"raw": x}
		}
		return asObject(parsed)
	case map[string]any:
		return x
	}
	return core.Obj{}
}

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
	failed := resultType == "failure" || resultType == "error" || resultType == "denied" || result["is_error"] == true

	if shellTool.MatchString(tool) {
		command := pickStr(input, "command", "cmd", "raw")
		if command == "" {
			return nil
		}
		return &core.Event{Ev: "tool", Tool: tool, Command: core.Clean(command, 300), Failed: failed}
	}
	if editTool.MatchString(tool) {
		cands := []any{pick(input, "file_path", "filePath", "path", "file", "notebook_path")}
		if patch, ok := pick(input, "input", "patch", "raw").(string); ok {
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

// hookOutput is what a hook prints on stdout to give the agent context.
type hookOutput struct {
	HookSpecificOutput *hookSpecific `json:"hookSpecificOutput,omitempty"`
	AdditionalContext  string        `json:"additionalContext,omitempty"`
}

type hookSpecific struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func contextOutput(agent string, ev hookEvent, text string) *hookOutput {
	if text == "" {
		return nil
	}
	if agent == "claude" {
		name := "UserPromptSubmit"
		if ev == evStart {
			name = "SessionStart"
		}
		return &hookOutput{HookSpecificOutput: &hookSpecific{HookEventName: name, AdditionalContext: text}}
	}
	return &hookOutput{AdditionalContext: text}
}

func currentGit(root string) (branch, head string) {
	branch, _ = core.GitTry(root, "rev-parse", "--abbrev-ref", "HEAD")
	head, _ = core.GitTry(root, "rev-parse", "--short", "HEAD")
	return
}

// saveTriggers stores the memories a user asked for in their prompt ("remember that …"). A failure never reaches the agent.
func saveTriggers(p *core.Project, cfg core.Config, source, prompt string) (saved []string) {
	if err := core.CatchAll(func() {
		found := triggersIn(prompt)
		if len(found) > cfg.MaxAutoPerPrompt {
			found = found[:cfg.MaxAutoPerPrompt]
		}
		for _, c := range found {
			saved = append(saved, p.Remember(core.RememberInput{Text: c.Text, Kind: c.Kind, Title: c.Title, Tags: []string{"auto", "prompt"}, Extra: [][2]string{{"source", source}}}))
		}
	}); err != nil {
		logError(p.Root, err)
	}
	return saved
}

var unsafeSid = regexp.MustCompile(`[^\w.-]`)

// launcherCLI is how the project's own files (briefing, slash commands) name the CLI: the launcher `agent init` writes.
func launcherCLI() string { return "./.git/git-memory/" + core.WrapperName() }

// handleHook is the single entry point for every agent/event pair. It returns the hook's stdout object, or nil.
func handleHook(agent, rawEvent string, payload core.HookPayload) *hookOutput {
	if rawEvent == "" {
		rawEvent = pickStr(payload, "hook_event_name", "hookEventName")
	}
	ev, ok := normalizeEvent(rawEvent)
	if !ok {
		return nil
	}

	cwd := pickStr(payload, "cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	p := core.OpenProject(cwd)
	cfg := core.DefaultConfig()
	sid := "unknown"
	if v := pick(payload, "session_id", "sessionId"); v != nil {
		sid = unsafeSid.ReplaceAllString(toString(v), "_")
	}
	file := spoolFile(p.Spool, agent, sid)

	switch ev {
	case evStart:
		branch, head := currentGit(p.Root)
		appendEvent(p.Spool, agent, sid, core.Event{Ev: "start", Source: pickStr(payload, "source"), Branch: branch, Head: head})
		if initial := core.StripHarnessContext(pickStr(payload, "initial_prompt", "initialPrompt")); initial != "" {
			appendEvent(p.Spool, agent, sid, core.Event{Ev: "prompt", Text: core.Clean(initial, cfg.Capture.MaxPromptChars)})
		}
		core.RequireMemoryRepo(p.Mem)
		core.WithLock(p.Mem, func() {
			if flushStale(p, file) > 0 {
				core.CommitPaths(p.Mem, "memory: finalize earlier sessions", p.DirRel())
			}
		})
		purgeOld(p.Spool, 14)
		return contextOutput(agent, ev, buildBriefing(core.ListProjectPages(p), cfg, p.DirRel(), launcherCLI()))

	case evPrompt:
		text := core.StripHarnessContext(pickStr(payload, "prompt"))
		if text == "" {
			return nil // the prompt was only editor context
		}
		appendEvent(p.Spool, agent, sid, core.Event{Ev: "prompt", Text: core.Clean(text, cfg.Capture.MaxPromptChars)})
		saved := saveTriggers(p, cfg, agent+":"+sid, text)
		if agent != "claude" {
			return nil
		}
		// what this very prompt just saved is not "related memory": the agent was only told to remember it
		pages := filterPages(core.ListProjectPages(p), func(pg *core.Page) bool { return !core.Contains(saved, pg.Repo) })
		return contextOutput(agent, ev, buildRecall(pages, cfg, core.Clean(text, 600)))

	case evTool, evToolFail:
		if e := summarizeTool(payload, p.Root, cfg); e != nil {
			if ev == evToolFail {
				e.Failed = true
			}
			appendEvent(p.Spool, agent, sid, *e)
		}
		return nil

	case evStop:
		last := core.Clean(lastAssistantText(payload), cfg.Capture.MaxOutcomeChars)
		appendEvent(p.Spool, agent, sid, core.Event{Ev: "stop", Last: last})
		core.RequireMemoryRepo(p.Mem)
		core.WithLock(p.Mem, func() {
			writeSessionPage(p, file)
			if core.LastCommitAgeSec(p.Mem, p.DirRel()) >= int64(cfg.DebounceCommitSec) {
				core.CommitPaths(p.Mem, "memory: session "+core.RuneSlice(sid, 8)+" ("+agent+")", p.DirRel())
			}
		})
		return nil

	case evPreCompact, evEnd:
		name := "precompact"
		if ev == evEnd {
			name = "end"
		}
		appendEvent(p.Spool, agent, sid, core.Event{Ev: name, Reason: pickStr(payload, "reason", "trigger")})
		core.RequireMemoryRepo(p.Mem)
		core.WithLock(p.Mem, func() {
			writeSessionPage(p, file)
			core.CommitPaths(p.Mem, "memory: session "+core.RuneSlice(sid, 8)+" ("+agent+")", p.DirRel())
		})
		return nil
	}
	return nil
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
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

// ---- the entry point ----------------------------------------------------------------------------------------------

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

func Run(agent, event string) {
	payload := readStdinJSON()
	err := core.CatchAll(func() {
		if agent != "claude" && agent != "copilot" {
			core.Fail(`unknown agent "%s"`, agent)
		}
		if o := handleHook(agent, event, payload); o != nil {
			fmt.Println(core.MarshalJSON(o, false))
		}
	})
	if err != nil {
		// Hooks must never break the agent: log and exit cleanly.
		cwd := pickStr(payload, "cwd")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		logError(cwd, err)
	}
}
