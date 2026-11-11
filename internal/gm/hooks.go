package gm

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
func asObject(v any) obj {
	switch x := v.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(x), &parsed); err != nil {
			return obj{"raw": x}
		}
		return asObject(parsed)
	case map[string]any:
		return x
	}
	return obj{}
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
	rel, err := relPath(root, abs)
	if err != nil {
		return ""
	}
	rel = slash(rel)
	if strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel // "" when p is the project root itself: not a file
}

// summarizeTool reduces an agent-specific tool call to what is worth remembering. Tool output is never stored.
func summarizeTool(payload HookPayload, root string, cfg Config) *Event {
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
		return &Event{Ev: "tool", Tool: tool, Command: clean(command, 300), Failed: failed}
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
			if f != "" && !contains(files, f) && !isExcluded(f, cfg.Capture.Exclude) {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			return nil
		}
		return &Event{Ev: "tool", Tool: tool, Files: files, Failed: failed}
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
func lastAssistantText(payload HookPayload) string {
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
	branch, _ = gitTry(root, "rev-parse", "--abbrev-ref", "HEAD")
	head, _ = gitTry(root, "rev-parse", "--short", "HEAD")
	return
}

// backgroundSync runs `sync --quiet` after the hook has returned.
func backgroundSync(root string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "sync", "--quiet")
	cmd.Dir = root
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// saveTriggers stores the memories a user asked for in their prompt ("remember that …"). A failure never reaches the agent.
func saveTriggers(scopes *Scopes, cfg Config, source, prompt string) {
	if err := catchAll(func() {
		found := triggersIn(prompt)
		if len(found) > cfg.AutoRemember.MaxPerPrompt {
			found = found[:cfg.AutoRemember.MaxPerPrompt]
		}
		if len(found) == 0 {
			return
		}
		store := getScope(scopes, cfg.AutoRemember.Scope)
		for _, c := range found {
			remember(store, cfg, rememberInput{Text: c.Text, Kind: c.Kind, Title: c.Title, Tags: []string{"auto", "prompt"}, Extra: [][2]string{{"source", source}}})
		}
	}); err != nil {
		logError(scopes.Project.Root, err)
	}
}

var unsafeSid = regexp.MustCompile(`[^\w.-]`)

// handleHook is the single entry point for every agent/event pair. It returns the hook's stdout object, or nil.
func handleHook(agent, rawEvent string, payload HookPayload) *hookOutput {
	if rawEvent == "" {
		rawEvent = pickStr(payload, "hook_event_name", "hookEventName")
	}
	ev, ok := normalizeEvent(rawEvent)
	if !ok || os.Getenv("GIT_MEMORY_DISABLE") != "" {
		return nil
	}

	cwd := pickStr(payload, "cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	loc, cfg, scopes := openAll(cwd)
	// Session pages go to one scope's store: the project's (shared) or the user's private one.
	sessionLoc, ok := scopes.ByID[cfg.SessionScope]
	if !ok {
		sessionLoc = getScope(scopes, "personal")
	}
	sid := "unknown"
	if v := pick(payload, "session_id", "sessionId"); v != nil {
		sid = unsafeSid.ReplaceAllString(toString(v), "_")
	}
	file := spoolFile(loc, agent, sid)

	switch ev {
	case evStart:
		branch, head := currentGit(loc.Root)
		appendEvent(loc, agent, sid, Event{Ev: "start", Source: pickStr(payload, "source"), Branch: branch, Head: head})
		if initial := stripHarnessContext(pickStr(payload, "initial_prompt", "initialPrompt")); initial != "" {
			appendEvent(loc, agent, sid, Event{Ev: "prompt", Text: clean(initial, cfg.Capture.MaxPromptChars)})
		}
		// Only the project (if it has a shared store) and session stores are created here; other scopes are read if their
		// checkout already exists (cloning an unrelated remote inside a hook could blow the hook timeout; `init` and
		// `sync` do that).
		if scopes.ProjectEnabled {
			tryEnsure(loc, cfg)
		}
		if tryEnsure(sessionLoc, cfg) == "" {
			withLock(sessionLoc, func() {
				if flushStale(sessionLoc, cfg, file) > 0 {
					commitAll(sessionLoc, "memory: finalize earlier sessions")
				}
			})
		}
		purgeOld(loc, 14)
		return contextOutput(agent, ev, buildBriefing(listAllPages(scopes), cfg))

	case evPrompt:
		text := stripHarnessContext(pickStr(payload, "prompt"))
		if text == "" {
			return nil // the prompt was only editor context
		}
		appendEvent(loc, agent, sid, Event{Ev: "prompt", Text: clean(text, cfg.Capture.MaxPromptChars)})
		if cfg.AutoRemember.Triggers {
			saveTriggers(scopes, cfg, agent+":"+sid, text)
		}
		if agent != "claude" {
			return nil
		}
		return contextOutput(agent, ev, buildRecall(listAllPages(scopes), cfg, clean(text, 600)))

	case evTool, evToolFail:
		if e := summarizeTool(payload, loc.Root, cfg); e != nil {
			if ev == evToolFail {
				e.Failed = true
			}
			appendEvent(loc, agent, sid, *e)
		}
		return nil

	case evStop:
		last := clean(lastAssistantText(payload), cfg.Capture.MaxOutcomeChars)
		appendEvent(loc, agent, sid, Event{Ev: "stop", Last: last})
		withLock(sessionLoc, func() {
			ensureStore(sessionLoc, cfg)
			writeSessionPage(sessionLoc, cfg, file, false)
			if lastCommitAgeSec(sessionLoc) >= int64(cfg.DebounceCommitSec) {
				commitAll(sessionLoc, "memory: session "+runeSlice(sid, 8)+" ("+agent+")")
			}
		})
		return nil

	case evPreCompact, evEnd:
		name := "precompact"
		if ev == evEnd {
			name = "end"
		}
		appendEvent(loc, agent, sid, Event{Ev: name, Reason: pickStr(payload, "reason", "trigger")})
		withLock(sessionLoc, func() {
			ensureStore(sessionLoc, cfg)
			writeSessionPage(sessionLoc, cfg, file, ev == evEnd)
			commitAll(sessionLoc, "memory: session "+runeSlice(sid, 8)+" ("+agent+")")
		})
		if ev == evEnd && cfg.AutoPush {
			backgroundSync(loc.Root)
		}
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

// logError appends to .git/git-memory/errors.log; outside a repo there is nothing sensible to do.
func logError(cwd string, err error) {
	_ = catchAll(func() {
		base := locate(cwd)
		mkdirAll(base.Data)
		f, ferr := os.OpenFile(filepath.Join(base.Data, "errors.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		check(ferr)
		defer f.Close()
		_, werr := f.WriteString(iso(time.Now()) + " " + err.Error() + "\n")
		check(werr)
	})
}
