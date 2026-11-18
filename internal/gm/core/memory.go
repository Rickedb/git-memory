package core

import (
	"path/filepath"
	"strings"
)

type RememberInput struct {
	Text  string
	Kind  string
	Title string
	Tags  []string
	Core  bool
	// Extra frontmatter, e.g. source=claude:<session> for memories saved from a prompt.
	Extra [][2]string
}

// remember stores (or updates) a durable memory page in the memory root folder (or its active scope's) of the memory repository and commits it.
// It returns the page's path in the repository.
func (p *Project) Remember(in RememberInput) string { return p.RememberIn(p.WriteRel(), in) }

// rememberIn is remember for any folder of the memory repository (the project's, or global).
func (p *Project) RememberIn(folder string, in RememberInput) string {
	kind := in.Kind
	if kind == "" {
		kind = "note"
	}
	if !IsKind(kind) {
		Fail("kind must be one of: %s", strings.Join(kinds, ", "))
	}
	body := strings.TrimSpace(Clean(in.Text, 8000))
	if body == "" {
		Fail("nothing to remember")
	}
	var heading string
	if in.Title != "" {
		heading = Clean(in.Title, 100)
	} else {
		first, _, _ := strings.Cut(body, "\n")
		heading = OneLine(first, 70)
	}
	dir := "memories/" + kind
	if in.Core {
		dir = "core"
	}
	rel := dir + "/" + Slugify(heading, 60) + ".md"
	repoPath := InRoot(folder, rel)
	RequireMemoryRepo(p.Mem)
	WithLock(p.Mem, func() {
		dest := filepath.Join(p.Mem, filepath.FromSlash(folder), filepath.FromSlash(rel))
		now := nowISO()
		created := now
		if old, ok := ReadFileTry(dest); ok {
			if c := ParsePage(old, rel, "").Meta.Str("created"); c != "" {
				created = c
			}
		}
		tier := "semantic"
		if in.Core {
			tier = "core"
		}
		meta := NewMeta().Set("id", strings.TrimSuffix(rel, ".md")).Set("kind", kind).Set("tier", tier).SetList("tags", in.Tags)
		for _, kv := range in.Extra {
			meta.Set(kv[0], kv[1])
		}
		meta.Set("created", created).Set("updated", now)
		MkdirAll(filepath.Dir(dest))
		WriteFile(dest, SerializePage(meta, "# "+heading+"\n\n"+body))
		CommitPaths(p.Mem, "memory: remember "+OneLine(heading, 60), repoPath)
	})
	return repoPath
}
