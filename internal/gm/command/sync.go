package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

const syncUsage = `usage: git-memory sync [--no-push] | --continue | --abort

Brings your memory branch together with the team's and shares it:
  1. fetches the remote
  2. merges the default branch (origin/main) into your branch (your git user name)
  3. pushes your branch to the default branch (a fast-forward, since it now contains it)
A conflict stops the merge half way: resolve the files, 'git-memory add' them, then 'git-memory sync --continue'.
  --no-push    stop after the merge
  --continue   finish a merge you resolved, then push
  --abort      give up the merge; your branch is as it was`

// Sync is `git-memory sync`. It exits 0 when the work is shared, 2 when a merge waits for its conflicts to be resolved
// (the memory-sync skill settles them, or hands the doubtful ones to the user), 1 for anything else that went wrong.
func Sync(flags core.Flags, pos []string) int {
	if len(pos) > 0 || (flags.Has("continue") && flags.Has("abort")) {
		core.Fail("%s", syncUsage)
	}
	cwd, _ := os.Getwd()
	mem := core.MemoryDirFor(cwd)
	core.RequireMemoryRepo(mem)
	remote := core.RemoteName(mem)
	if remote == "" {
		core.Fail("the memory repository has no remote to sync with: git-memory remote add origin <url>")
	}

	switch {
	case flags.Has("abort"):
		if !core.MergeInProgress(mem) {
			core.Fail("there is no merge to abort")
		}
		core.Git(mem, "merge", "--abort")
		out("merge abandoned: " + core.CurrentBranch(mem) + " is as it was before the sync")
		return 0
	case flags.Has("continue"):
		if !core.MergeInProgress(mem) {
			core.Fail("there is no merge to continue: run git-memory sync")
		}
		if left := core.Conflicts(mem); len(left) > 0 {
			return reportConflicts(mem, remote, left)
		}
		if marked := core.ConflictMarkersStaged(mem); len(marked) > 0 {
			fmt.Fprintln(os.Stderr, "git-memory: these files still hold conflict markers (<<<<<<< / >>>>>>>); fix them and git-memory add them:")
			for _, f := range marked {
				fmt.Fprintln(os.Stderr, "  "+f)
			}
			return 2
		}
		core.WithLock(mem, func() { core.FinishMerge(mem) })
		out("merge finished")
		return cycle(mem, remote, false, flags.Has("no-push"))
	}

	if core.MergeInProgress(mem) {
		return reportConflicts(mem, remote, core.Conflicts(mem))
	}
	if st, _ := core.GitTry(mem, "status", "--porcelain", "--untracked-files=all"); st != "" {
		core.Fail("the memory repository has uncommitted changes; commit them first (git-memory add . && git-memory commit -m \"...\"), or stash them:\n%s", st)
	}
	core.WithLock(mem, func() {
		cur := core.CurrentBranch(mem)
		branch := core.UserBranch(mem)
		if _, ok := core.GitTry(mem, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); ok && cur != "" && cur != branch && core.Count(mem, branch, cur) > 0 {
			core.Fail("branch %s has commits that are not on your branch %s: merge them first (git-memory checkout %s && git-memory merge %s)", cur, branch, branch, cur)
		}
		if b, created := core.EnsureUserBranch(mem); created {
			out("created your branch " + b)
		}
	})
	return cycle(mem, remote, true, flags.Has("no-push"))
}

// cycle is fetch, merge, push, again when someone else pushed in between. fresh says the merge has not been tried yet
// (after --continue it has, so the first round only has to push).
func cycle(mem, remote string, fresh, noPush bool) int {
	branch := core.CurrentBranch(mem)
	for round := 0; round < 4; round++ {
		if fresh || round > 0 {
			if _, err := fetch(mem, remote); err != nil {
				core.Fail("%v", err)
			}
			def := core.DefaultBranch(mem)
			if code := mergeDefault(mem, remote, def, branch); code != 0 {
				return code
			}
		}
		if noPush {
			out("not pushed (--no-push): git-memory sync shares it")
			return 0
		}
		def := core.DefaultBranch(mem)
		sent := core.Count(mem, remote+"/"+def, branch)
		if _, ok := core.GitTry(mem, "show-ref", "--verify", "--quiet", "refs/remotes/"+remote+"/"+def); ok && sent == 0 {
			out("nothing to share: " + remote + "/" + def + " already has everything on " + branch)
			return 0
		}
		_, err := core.GitErr(mem, "push", "--quiet", remote, branch+":"+def)
		if err == nil {
			out(fmt.Sprintf("pushed %s → %s/%s (%s)", branch, remote, def, plural(sent, "commit", "commits")))
			return 0
		}
		msg := err.Error()
		if !strings.Contains(msg, "rejected") && !strings.Contains(msg, "non-fast-forward") && !strings.Contains(msg, "fetch first") {
			core.Fail("the remote refused the push to %s (is the branch protected?): %v\nyour merged branch is ready: git-memory push %s %s, then open a pull request into %s", def, err, remote, branch, def)
		}
		fresh = true // somebody pushed first: merge what they pushed, then push again
	}
	core.Fail("%s/%s kept moving while syncing; run git-memory sync again", remote, core.DefaultBranch(mem))
	return 1
}

func fetch(mem, remote string) (string, error) {
	out, err := core.GitErr(mem, "fetch", "--quiet", "--prune", remote)
	if err == nil {
		if _, ok := core.GitTry(mem, "symbolic-ref", "-q", "refs/remotes/"+remote+"/HEAD"); !ok {
			_, _ = core.GitTry(mem, "remote", "set-head", remote, "-a") // the remote's default branch, once it has one
		}
	}
	return out, err
}

// mergeDefault merges the default branch of the remote into the user's branch. It returns 2 when conflicts wait.
func mergeDefault(mem, remote, def, branch string) int {
	upstream := remote + "/" + def
	if _, ok := core.GitTry(mem, "show-ref", "--verify", "--quiet", "refs/remotes/"+upstream); !ok {
		out(upstream + " does not exist yet: this is the first push")
		return 0
	}
	behind := core.Count(mem, branch, upstream)
	if behind == 0 {
		out(branch + " is up to date with " + upstream)
		return 0
	}
	var conflicted bool
	core.WithLock(mem, func() { conflicted = core.MergeRef(mem, upstream, "memory: merge "+def+" into "+branch) })
	if conflicted {
		return reportConflicts(mem, remote, core.Conflicts(mem))
	}
	out(fmt.Sprintf("merged %s into %s (%s)", upstream, branch, plural(behind, "commit", "commits")))
	return 0
}

func reportConflicts(mem, remote string, files []core.Conflict) int {
	def := core.DefaultBranch(mem)
	fmt.Fprintf(os.Stderr, "git-memory: merging %s/%s into %s stopped on %s:\n", remote, def, core.CurrentBranch(mem), plural(len(files), "conflict", "conflicts"))
	for _, f := range files {
		fmt.Fprintf(os.Stderr, "  %s  %s   (%s)\n", f.Code, f.Path, core.ConflictCodes[f.Code])
	}
	fmt.Fprintln(os.Stderr, "\nSettle each file (the markers show your side, the common ancestor and theirs; git-memory show :1:<file> :2:<file> :3:<file> print the three versions),")
	fmt.Fprintln(os.Stderr, "git-memory add <file>, then git-memory sync --continue.   To give up: git-memory sync --abort.")
	return 2
}
