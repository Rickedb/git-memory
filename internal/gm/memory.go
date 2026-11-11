package gm

import (
	"path/filepath"
	"strings"
)

type rememberInput struct {
	Text  string
	Kind  string
	Title string
	Tags  []string
	Core  bool
	// Extra frontmatter, e.g. source=personal:sessions/… for auto-saved memories.
	Extra [][2]string
}

// remember stores (or updates) a durable memory page in one scope's store and commits it. It returns the page path.
func remember(store *Loc, cfg Config, in rememberInput) string {
	kind := in.Kind
	if kind == "" {
		kind = "note"
	}
	if !isKind(kind) {
		fail("kind must be one of: %s", strings.Join(kinds, ", "))
	}
	body := strings.TrimSpace(clean(in.Text, 8000))
	if body == "" {
		fail("nothing to remember")
	}
	var heading string
	if in.Title != "" {
		heading = clean(in.Title, 100)
	} else {
		first, _, _ := strings.Cut(body, "\n")
		heading = oneLine(first, 70)
	}
	dir := "memories/" + kind
	if in.Core {
		dir = "core"
	}
	rel := dir + "/" + slugify(heading, 60) + ".md"
	var result string
	withLock(store, func() {
		ensureStore(store, cfg)
		dest := filepath.Join(store.Wt, filepath.FromSlash(rel))
		now := nowISO()
		created := now
		if old, ok := readFileTry(dest); ok {
			if c := parsePage(old, rel, "").Meta.Str("created"); c != "" {
				created = c
			}
		}
		tier := "semantic"
		if in.Core {
			tier = "core"
		}
		meta := newMeta().Set("id", strings.TrimSuffix(rel, ".md")).Set("kind", kind).Set("tier", tier).SetList("tags", in.Tags)
		for _, kv := range in.Extra {
			meta.Set(kv[0], kv[1])
		}
		meta.Set("created", created).Set("updated", now)
		mkdirAll(filepath.Dir(dest))
		writeFile(dest, serializePage(meta, "# "+heading+"\n\n"+body))
		commitAll(store, "memory: remember "+oneLine(heading, 60))
		result = rel
	})
	return result
}

type resolved struct {
	Page  *Page
	Store *Loc
}

// resolvePage resolves `scope:path`, a path, an id or a unique substring to a page and the store that holds it.
func resolvePage(scopes *Scopes, cfg Config, ref string) resolved {
	scope, rel := parseRef(scopes, ref)
	stores := scopes.All
	if scope != "" {
		stores = []*Loc{getScope(scopes, scope)}
	}
	var found []resolved
	for _, store := range stores {
		if tryEnsure(store, cfg) != "" {
			continue // an unreachable store (e.g. offline clone) just has no pages to offer
		}
		for _, p := range listPages(store.Wt, store.Scope) {
			found = append(found, resolved{p, store})
		}
	}
	want := strings.TrimSuffix(rel, ".md")
	var hits []resolved
	for _, r := range found {
		if strings.TrimSuffix(r.Page.Path, ".md") == want || r.Page.Meta.Str("id") == want {
			hits = append(hits, r)
		}
	}
	if len(hits) == 0 {
		for _, r := range found {
			if strings.Contains(r.Page.Path, want) {
				hits = append(hits, r)
			}
		}
	}
	if len(hits) != 1 {
		if len(hits) > 1 {
			var refs []string
			for _, h := range hits {
				refs = append(refs, pageRef(h.Page))
			}
			fail("ambiguous: %s", strings.Join(refs, ", "))
		}
		fail(`no page matches "%s"`, ref)
	}
	return hits[0]
}

func forget(scopes *Scopes, cfg Config, ref string) string {
	r := resolvePage(scopes, cfg, ref)
	withLock(r.Store, func() {
		git(r.Store.Wt, "rm", "--quiet", "-f", r.Page.Path)
		commitAll(r.Store, "memory: forget "+r.Page.Path)
	})
	return pageRef(r.Page)
}

// setTier pins a page as core (always in the session briefing) or returns it to normal. It edits frontmatter in place.
func setTier(scopes *Scopes, cfg Config, ref, tier string) string {
	r := resolvePage(scopes, cfg, ref)
	withLock(r.Store, func() {
		meta := r.Page.Meta.Clone().Set("tier", tier).Set("updated", nowISO())
		writeFile(filepath.Join(r.Store.Wt, filepath.FromSlash(r.Page.Path)), serializePage(meta, r.Page.Body))
		verb := "unpin"
		if tier == "core" {
			verb = "pin"
		}
		commitAll(r.Store, "memory: "+verb+" "+r.Page.Path)
	})
	return pageRef(r.Page)
}

// move moves a page to another scope (e.g. promote a project decision to global). It returns the new reference.
func move(scopes *Scopes, cfg Config, ref, toScope string) string {
	r := resolvePage(scopes, cfg, ref)
	from, page := r.Store, r.Page
	to := getScope(scopes, toScope)
	if to.Scope == from.Scope {
		fail("already in the %s scope", toScope)
	}
	ensureStore(to, cfg)
	dest := filepath.Join(to.Wt, filepath.FromSlash(page.Path))
	if exists(dest) {
		fail("%s already exists", refOf(to.Scope, page.Path))
	}
	withLock(to, func() {
		mkdirAll(filepath.Dir(dest))
		writeFile(dest, serializePage(page.Meta.Clone().Set("updated", nowISO()), page.Body))
		commitAll(to, "memory: move in "+page.Path+" from "+from.Scope)
	})
	withLock(from, func() {
		git(from.Wt, "rm", "--quiet", "-f", page.Path)
		commitAll(from, "memory: move out "+page.Path+" to "+to.Scope)
	})
	return refOf(to.Scope, page.Path)
}

type supersedeResult struct {
	Old string
	By  string
	// Inherited is true when the replacement became pinned (core) because the page it replaces was.
	Inherited bool
}

// supersede retires a decision without deleting it: the page stays in git and on disk, marked `superseded`, and stops
// binding (it leaves the briefing, recall and default search). With `by`, the two pages are linked, and the replacement
// is pinned if the page it replaces was.
func supersede(scopes *Scopes, cfg Config, ref, by, reasonText string) supersedeResult {
	old := resolvePage(scopes, cfg, ref)
	if old.Page.Status == "superseded" {
		fail("%s is already superseded", pageRef(old.Page))
	}
	var next *resolved
	if by != "" {
		n := resolvePage(scopes, cfg, by)
		next = &n
		if pageRef(n.Page) == pageRef(old.Page) {
			fail("a page cannot supersede itself")
		}
		if n.Page.Status == "superseded" {
			fail("%s is itself superseded", pageRef(n.Page))
		}
	}
	reason := ""
	if reasonText != "" {
		reason = clean(reasonText, 200)
	}
	now := nowISO()
	nextRef := ""
	if next != nil {
		nextRef = pageRef(next.Page)
	}

	note := "> Superseded"
	if nextRef != "" {
		note += " by " + nextRef
	}
	if reason != "" {
		note += ": " + reason
	}
	withLock(old.Store, func() {
		meta := old.Page.Meta.Clone().Set("status", "superseded").Set("superseded_by", nextRef).Set("superseded_reason", reason).Set("updated", now)
		body := old.Page.Body
		if loc := headingLine.FindStringIndex(body); loc != nil {
			body = body[:loc[1]] + "\n\n" + note + body[loc[1]:]
		}
		writeFile(filepath.Join(old.Store.Wt, filepath.FromSlash(old.Page.Path)), serializePage(meta, body))
		commitAll(old.Store, "memory: supersede "+old.Page.Path)
	})

	inherited := false
	if next != nil {
		inherited = old.Page.Tier == "core" && next.Page.Tier != "core"
		withLock(next.Store, func() {
			tier := next.Page.Tier
			if inherited {
				tier = "core"
			}
			meta := next.Page.Meta.Clone().Set("supersedes", pageRef(old.Page)).Set("tier", tier).Set("updated", now)
			writeFile(filepath.Join(next.Store.Wt, filepath.FromSlash(next.Page.Path)), serializePage(meta, next.Page.Body))
			commitAll(next.Store, "memory: "+next.Page.Path+" supersedes "+old.Page.Path)
		})
	}
	return supersedeResult{Old: pageRef(old.Page), By: nextRef, Inherited: inherited}
}
