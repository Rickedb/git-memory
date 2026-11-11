package gm

import (
	"regexp"
	"strings"
)

type candidate struct {
	Kind  string
	Title string
	Text  string
}

var kindOf = map[string]string{"remember": "note", "decision": "decision", "fact": "fact", "procedure": "procedure"}

var (
	trigSigil = regexp.MustCompile(`(?i)^#(remember|decision|fact|procedure)\b[\s:–—-]*(.+)$`)
	trigColon = regexp.MustCompile(`(?i)^(remember|decision)\s*:\s*(.+)$`)
	trigThat  = regexp.MustCompile(`(?i)^(?:please\s+)?remember\s+that\s+(.+)$`)
)

// triggersIn finds the memories the user asks for in their own prompt, one per line. Only unambiguous markers count, so
// ordinary prose ("Note: the build is red", a pasted email) is never stored:
//
//	remember that we deploy on Tuesdays       remember: we deploy on Tuesdays      #remember …
//	decision: use pnpm, never npm             #decision …
//	#fact the API listens on 8080             #procedure …
//
// (`remember to run the tests` is an instruction for the current task, not a memory.) The `/remember`-style forms are
// deliberately not markers: they are real slash commands (see commands.go), handled by the agent.
func triggersIn(prompt string) []candidate {
	var out []candidate
	for _, raw := range strings.Split(prompt, "\n") {
		line := strings.TrimSpace(raw)
		var kind, text string
		if m := trigSigil.FindStringSubmatch(line); m != nil {
			kind, text = kindOf[strings.ToLower(m[1])], m[2]
		} else if m := trigColon.FindStringSubmatch(line); m != nil {
			kind, text = kindOf[strings.ToLower(m[1])], m[2]
		} else if m := trigThat.FindStringSubmatch(line); m != nil {
			kind, text = "note", m[1]
		}
		if kind != "" && isKind(kind) && text != "" && len([]rune(strings.TrimSpace(text))) >= 8 {
			out = append(out, candidate{Kind: kind, Title: oneLine(text, 60), Text: strings.TrimSpace(text)})
		}
	}
	return out
}
