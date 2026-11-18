package gm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

func TestBranchNamesComeFromTheGitUserName(t *testing.T) {
	for name, want := range map[string]string{
		"Henrique Dal Bello": "henrique-dal-bello",
		"Álice O'Neil":       "alice-o-neil",
		"  José  ":           "jose",
		"dev_42":             "dev-42",
		"日本語":                "",
		"Straße":             "strasse",
	} {
		if got := core.NormalizeBranch(name); got != want {
			t.Errorf("NormalizeBranch(%q) = %q, want %q", name, got, want)
		}
	}
}

// the user's commits go to a branch of their own, and sync shares it through the default branch
func TestEachPersonCommitsToTheirOwnBranchAndSyncSharesIt(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	remote := e.bare()
	const page = "memories/decision/pnpm.md"
	body := func(rule string) string {
		return "---\nkind: decision\n---\n\n# Use pnpm\n\n" + rule + "\n\nReason: one lockfile.\n"
	}

	a := e.newRepo("alice")
	aliceMem := e.home
	e.git(aliceMem, "config", "user.name", "Álice O'Neil")
	e.gm(a, "remote", "add", "origin", remote)
	out := e.agentInit(a)
	e.has(out, "memory branch: alice-o-neil")
	e.eq(strings.TrimSpace(e.git(aliceMem, "symbolic-ref", "--short", "HEAD")), "alice-o-neil", "agent init checks out the user's branch")
	rows, _ := e.doctor(a)
	e.has(rows["Memory repository"], "(branch alice-o-neil,")

	// the first sync has nothing to merge: it creates the default branch on the remote
	e.commitPage(a, page, body("Never npm."))
	e.has(e.gm(a, "sync"), "pushed alice-o-neil → origin/main (")
	e.eq(strings.TrimSpace(e.git(remote, "show", "main:"+page)) != "", true, "main on the remote has the page")
	e.has(e.gm(a, "sync"), "nothing to share", "a second sync with nothing new is quiet")

	// alice edits the rule and keeps it local
	e.commitPage(a, page, body("Never npm or yarn."))

	e.withMachine(func() { // bob, on his machine
		b := e.newRepoNoMemory("bob")
		e.gm(b, "clone", "-q", remote)
		e.home = filepath.Join(b, ".git-memory")
		e.git(e.home, "config", "user.name", "Bob")
		e.has(e.agentInit(b), "memory branch: bob")
		e.eq(strings.TrimSpace(e.git(e.home, "symbolic-ref", "--short", "HEAD")), "bob")
		e.commitPage(b, page, body("Never npm, ever."))
		e.commitPage(b, "memories/fact/port.md", "---\nkind: fact\n---\n\n# Port\n\n8080.\n")
		e.has(e.gm(b, "sync"), "pushed bob → origin/main (2 commits)")
	})

	// alice meets bob's work: the same line was changed on both sides
	e.home = aliceMem
	_, errOut, code := e.gmRaw(a, "sync")
	e.eq(code, 2, "a conflict is exit 2")
	e.has(errOut, "UU  "+page+"   (both changed it)")
	e.has(errOut, "git-memory sync --continue")
	e.has(e.read(filepath.Join(aliceMem, page)), "|||||||", "the markers carry the common ancestor")
	_, errOut, code = e.gmRaw(a, "sync")
	e.eq(code, 2, "sync with a merge waiting reports it again")
	_, errOut, code = e.gmRaw(a, "sync", "--continue")
	e.eq(code, 2, "--continue refuses while a file still conflicts")
	e.has(errOut, page)

	// resolving it with the markers left in is caught
	e.gm(a, "add", "--", page)
	_, errOut, code = e.gmRaw(a, "sync", "--continue")
	e.eq(code, 2)
	e.has(errOut, "still hold conflict markers")

	e.write(filepath.Join(aliceMem, filepath.FromSlash(page)), body("Never npm or yarn, ever."))
	e.gm(a, "add", "--", page)
	e.has(e.gm(a, "sync", "--continue"), "pushed alice-o-neil → origin/main")
	e.has(e.git(remote, "show", "main:"+page), "Never npm or yarn, ever.")
	e.ok(e.git(remote, "show", "main:memories/fact/port.md") != "", "bob's page reached the remote through alice's merge")
	e.eq(e.status(a), "")
	e.eq(strings.TrimSpace(e.git(aliceMem, "symbolic-ref", "--short", "HEAD")), "alice-o-neil")
}

func TestSyncAbortPutsTheBranchBack(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	remote := e.bare()
	const page = "memories/note/a.md"
	a := e.newRepo("a")
	e.git(e.home, "config", "user.name", "Ann")
	e.gm(a, "remote", "add", "origin", remote)
	e.agentInit(a)
	e.commitPage(a, page, "# A\n\nzero\n")
	e.gm(a, "sync")
	e.commitPage(a, page, "# A\n\nann\n")
	before := strings.TrimSpace(e.git(e.home, "rev-parse", "HEAD"))

	e.withMachine(func() {
		b := e.newRepoNoMemory("b")
		e.gm(b, "clone", "-q", remote)
		e.home = filepath.Join(b, ".git-memory")
		e.git(e.home, "config", "user.name", "Bo")
		e.agentInit(b)
		e.commitPage(b, page, "# A\n\nbo\n")
		e.gm(b, "sync")
	})
	e.home = filepath.Join(a, ".git-memory")
	_, _, code := e.gmRaw(a, "sync")
	e.eq(code, 2)
	e.has(e.gm(a, "sync", "--abort"), "merge abandoned")
	e.eq(strings.TrimSpace(e.git(e.home, "rev-parse", "HEAD")), before)
	_, errOut, code := e.gmRaw(a, "sync", "--abort")
	e.eq(code, 1)
	e.has(errOut, "no merge to abort")
}

func TestSyncRefusesWithoutARemoteOrWithUncommittedPages(t *testing.T) {
	e := newEnv(t)
	e.skipOnWindows()
	a := e.newRepo("solo")
	e.agentInit(a)
	_, errOut, code := e.gmRaw(a, "sync")
	e.eq(code, 1)
	e.has(errOut, "has no remote to sync with")

	remote := e.bare()
	e.gm(a, "remote", "add", "origin", remote)
	e.write(filepath.Join(e.home, "memories/note/draft.md"), "# draft\n")
	_, errOut, code = e.gmRaw(a, "sync")
	e.eq(code, 1)
	e.has(errOut, "uncommitted changes")
	e.has(errOut, "memories/note/draft.md")
}
