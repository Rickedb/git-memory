package hook

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a an and are as at be but by for from has have how i if in into is it its of on or that the this to was we what when where which with you your can do does did not no so then there these those they them about just like want need please should would could use using make get`) {
		m[w] = true
	}
	return m
}()

var (
	camelCase = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	wordRun   = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

func tokenize(text string) []string {
	spaced := camelCase.ReplaceAllString(text, "${1} ${2}")
	var out []string
	for _, t := range wordRun.FindAllString(strings.ToLower(spaced), -1) {
		n := len([]rune(t))
		if n < 2 || stopWords[t] {
			continue
		}
		if n > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") {
			t = t[:len(t)-1]
		}
		out = append(out, t)
	}
	return out
}

var kindBoost = map[string]float64{"decision": 1.4, "procedure": 1.4, "fact": 1.2, "note": 1.0, "session": 0.8}

type searchHit struct {
	// Ref is the page's path in the memory repository.
	Ref   string
	Title string
	Kind  string
	Scope string
	Score float64
	// Matched is how many distinct query terms matched, and Terms how many the query had.
	Matched int
	Terms   int
	Snippet string
}

type searchOptions struct {
	Limit int
	Pages []*core.Page
}

func snippet(body string, terms []string) string {
	best, bestHits := "", -1
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "---") {
			continue
		}
		toks := map[string]bool{}
		for _, t := range tokenize(l) {
			toks[t] = true
		}
		hits := 0
		for _, t := range terms {
			if toks[t] {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = l, hits
		}
	}
	return core.OneLine(best, 220)
}

// search ranks pages with BM25 (title/tag weighting, a recency boost and a kind boost). The "index" is computed in
// memory from the files on every call: the wiki is small enough that this stays in the low milliseconds, and there is
// nothing to keep in sync or host.
func search(query string, o searchOptions) []searchHit {
	limit := o.Limit
	if limit <= 0 {
		limit = 8
	}
	var terms []string
	seen := map[string]bool{}
	for _, t := range tokenize(query) {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return nil
	}

	type doc struct {
		page *core.Page
		tf   map[string]float64
		len  float64
	}
	var docs []doc
	for _, p := range o.Pages {
		if !core.IsActive(p) { // superseded pages no longer bind
			continue
		}
		tf := map[string]float64{}
		add := func(text string, w float64) {
			for _, t := range tokenize(text) {
				tf[t] += w
			}
		}
		add(p.Title, 3)
		add(strings.Join(p.Tags, " "), 3)
		add(p.Body, 1)
		l := 0.0
		for _, v := range tf {
			l += v
		}
		docs = append(docs, doc{p, tf, l})
	}
	if len(docs) == 0 {
		return nil
	}

	avgLen := 0.0
	for _, d := range docs {
		avgLen += d.len
	}
	avgLen /= float64(len(docs))
	if avgLen == 0 {
		avgLen = 1
	}
	N := float64(len(docs))
	df := map[string]float64{}
	for _, t := range terms {
		for _, d := range docs {
			if _, ok := d.tf[t]; ok {
				df[t]++
			}
		}
	}
	now := float64(core.NowMs())
	const k1, b = 1.2, 0.75

	var results []searchHit
	for _, d := range docs {
		score, matched := 0.0, 0
		for _, t := range terms {
			f, ok := d.tf[t]
			if !ok || f == 0 {
				continue
			}
			matched++
			n := df[t]
			idf := math.Log(1 + (N-n+0.5)/(n+0.5))
			score += idf * ((f * (k1 + 1)) / (f + k1*(1-b+(b*d.len)/avgLen)))
		}
		if matched == 0 {
			continue
		}
		ageDays := math.Max(0, (now-float64(core.PageTime(d.page)))/86400000)
		boost, ok := kindBoost[d.page.Kind]
		if !ok {
			boost = 1
		}
		score *= (1 + 0.3*math.Exp(-ageDays/30)) * boost
		results = append(results, searchHit{
			Ref: core.PageRef(d.page), Title: d.page.Title, Kind: d.page.Kind, Scope: d.page.Scope, Score: score,
			Matched: matched, Terms: len(terms), Snippet: snippet(d.page.Body, terms),
		})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}
