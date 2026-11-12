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
	// Extra frontmatter, e.g. source=claude:<session> for memories saved from a prompt.
	Extra [][2]string
}

// remember stores (or updates) a durable memory page in the project's folder of the memory repository and commits it.
// It returns the page's path in the repository.
func (p *Project) remember(in rememberInput) string { return p.rememberIn(p.DirRel(), in) }

// rememberIn is remember for any folder of the memory repository (the project's, or global).
func (p *Project) rememberIn(folder string, in rememberInput) string {
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
	repoPath := folder + "/" + rel
	ensureMemoryRepo(p.Mem)
	withLock(p.Mem, func() {
		dest := filepath.Join(p.Mem, filepath.FromSlash(folder), filepath.FromSlash(rel))
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
		commitPaths(p.Mem, "memory: remember "+oneLine(heading, 60), folder)
	})
	return repoPath
}
