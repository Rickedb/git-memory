package hook

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

// Hook events are appended to a per-session JSONL file under .git/git-memory/spool. Appending is the only work done on
// hot paths (tool calls); git is touched later, when a session page is compiled and committed.

func spoolFile(dir, agent, sid string) string {
	return filepath.Join(dir, agent+"-"+sid+".jsonl")
}

func lastLine(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	n := st.Size()
	if n > 8192 {
		n = 8192
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, st.Size()-n); err != nil && err != io.EOF {
		return ""
	}
	lines := strings.Split(core.TrimEnd(string(buf)), "\n")
	return lines[len(lines)-1]
}

func eventBody(e core.Event) string {
	e.T = 0
	return core.MarshalJSON(e, false)
}

// appendEvent returns false when the event was dropped as a duplicate.
func appendEvent(dir, agent, sid string, e core.Event) bool {
	file := spoolFile(dir, agent, sid)
	core.MkdirAll(dir)
	e.T = core.NowMs()
	if prev := lastLine(file); prev != "" {
		// The same hook can fire twice when Copilot also reads .claude/settings.json.
		var p core.Event
		if json.Unmarshal([]byte(prev), &p) == nil && eventBody(p) == eventBody(e) && e.T-p.T < 2000 {
			return false
		}
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	core.Check(err)
	defer f.Close()
	_, err = f.WriteString(core.MarshalJSON(e, false) + "\n")
	core.Check(err)
	return true
}

func readEvents(file string) []core.Event {
	text, ok := core.ReadFileTry(file)
	if !ok {
		return nil
	}
	var out []core.Event
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		var e core.Event
		if json.Unmarshal([]byte(line), &e) == nil { // a torn line is skipped
			out = append(out, e)
		}
	}
	return out
}

func ListSpools(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func markerFile(file string) string { return strings.TrimSuffix(file, ".jsonl") + ".compiled" }

func isCompiled(file string) bool {
	text, ok := core.ReadFileTry(markerFile(file))
	st, err := os.Stat(file)
	if !ok || err != nil {
		return false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	return err == nil && n == st.Size()
}

func markCompiled(file string) {
	st, err := os.Stat(file)
	core.Check(err)
	core.WriteFile(markerFile(file), strconv.FormatInt(st.Size(), 10))
}

func purgeOld(dir string, days int) {
	for _, file := range ListSpools(dir) {
		st, err := os.Stat(file)
		if err == nil && time.Since(st.ModTime()) > time.Duration(days)*24*time.Hour && isCompiled(file) {
			os.Remove(file)
			os.Remove(markerFile(file))
		}
	}
}
