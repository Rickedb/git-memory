package core

import (
	"regexp"
	"strings"
)

type redaction struct {
	re  *regexp.Regexp
	rep string
}

var redactions = []redaction{
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), "[REDACTED:private-key]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`), "[REDACTED:api-key]"},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`), "[REDACTED:github-token]"},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}`), "[REDACTED:github-token]"},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "[REDACTED:aws-key]"},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), "[REDACTED:slack-token]"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[REDACTED:jwt]"},
	{regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/-]{16,}=*`), "${1}[REDACTED]"},
	// RE2 has no backreferences: the closing quote is optional, which can redact slightly more than a strict match, never less.
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY)[A-Z0-9_]*)\s*[=:]\s*["']?[^\s"']{6,}["']?`), "${1}=[REDACTED]"},
	{regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^\s:@/]+:)[^\s@/]+@`), "${1}[REDACTED]@"},
}

func redact(text string) string {
	for _, r := range redactions {
		text = r.re.ReplaceAllString(text, r.rep)
	}
	return text
}

var privateBlock = regexp.MustCompile(`(?is)<private>.*?</private>`)

// stripPrivate: content wrapped in <private>…</private> never reaches the memory branch.
func stripPrivate(text string) string {
	return privateBlock.ReplaceAllString(text, "[private omitted]")
}

var harnessOpen = regexp.MustCompile(`(?i)<(ide_[a-z_]+|system-reminder|command-[a-z-]+|local-command-[a-z-]+)>`)

// stripHarnessContext drops the context blocks editors and harnesses wrap around your prompt (<ide_opened_file>,
// <ide_selection>, <system-reminder>, ...). They are noise (and may contain selected code), not something you said.
// RE2 cannot match a closing tag by backreference, so each opening tag is paired with its closing tag by hand.
func StripHarnessContext(text string) string {
	var out strings.Builder
	rest := text
	for {
		loc := harnessOpen.FindStringSubmatchIndex(rest)
		if loc == nil {
			out.WriteString(rest)
			break
		}
		name := rest[loc[2]:loc[3]]
		closeTag := "</" + strings.ToLower(name) + ">"
		after := rest[loc[1]:]
		if end := strings.Index(strings.ToLower(after), closeTag); end >= 0 {
			out.WriteString(rest[:loc[0]])
			rest = after[end+len(closeTag):]
			continue
		}
		// no closing tag: leave this one as it is and look for the next
		out.WriteString(rest[:loc[1]])
		rest = after
	}
	return strings.TrimSpace(out.String())
}

func Clean(text string, max int) string {
	return Truncate(redact(stripPrivate(text)), max)
}

func globToRegExp(glob string) *regexp.Regexp {
	var re strings.Builder
	re.WriteString("^")
	g := []rune(glob)
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case c == '*':
			if i+1 < len(g) && g[i+1] == '*' {
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					re.WriteString("(?:.*/)?")
				} else {
					re.WriteString(".*")
				}
			} else {
				re.WriteString("[^/]*")
			}
		case c == '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	return regexp.MustCompile("(?s)" + re.String())
}

func IsExcluded(relPath string, patterns []string) bool {
	p := strings.ReplaceAll(relPath, "\\", "/")
	for _, g := range patterns {
		if globToRegExp(g).MatchString(p) {
			return true
		}
	}
	return false
}
