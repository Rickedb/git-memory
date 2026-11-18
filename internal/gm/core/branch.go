package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Every person who works on a project's memory commits to a branch of their own, named after their git user name, and
// `git-memory sync` is what brings it together with the default branch (see command.Sync). Two people never write to
// the same branch, so their hooks never race, and a conflict is met once, when one of them syncs, not on every commit.

var branchFold = strings.NewReplacer(
	"ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "đ", "d", "ł", "l", "þ", "th",
)

// NormalizeBranch turns a git user name into a branch name: lowercase letters and digits, words joined by dashes
// ("Henrique Dal Bello" → "henrique-dal-bello", "José O'Neil" → "jose-o-neil"). "" when nothing is left of it.
func NormalizeBranch(name string) string {
	var b strings.Builder
	dash := true // no leading dash
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if f, ok := accents[r]; ok {
			r = f
		}
		if s := branchFold.Replace(string(r)); s != string(r) {
			b.WriteString(s)
			dash = false
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// UserName is the git user name of whoever runs git-memory: the memory repository's own setting (which falls back to the
// user's global one), then the project's, then the name git would commit with from the environment, then the login name.
func UserName(mem string) string {
	if n, ok := GitTry(mem, "config", "user.name"); ok && strings.TrimSpace(n) != "" {
		return n
	}
	if pr := filepath.Dir(mem); pr != "" {
		if n, ok := GitTry(pr, "config", "user.name"); ok && strings.TrimSpace(n) != "" {
			return n
		}
	}
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "USER", "USERNAME"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// RemoteName is the remote git-memory shares through: origin when there is one, else the first. "" without a remote.
func RemoteName(mem string) string {
	remotes, _ := GitTry(mem, "remote")
	var first string
	for _, r := range strings.Split(remotes, "\n") {
		r = strings.TrimSpace(r)
		if r == "origin" {
			return r
		}
		if first == "" {
			first = r
		}
	}
	return first
}

func refExists(mem, ref string) bool {
	_, ok := GitTry(mem, "show-ref", "--verify", "--quiet", ref)
	return ok
}

// DefaultBranch is the branch everyone's work is merged into: what the remote's HEAD points at, else the main or master
// branch it has, else one that exists here, else main.
func DefaultBranch(mem string) string {
	remote := RemoteName(mem)
	if remote != "" {
		if ref, ok := GitTry(mem, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD"); ok {
			if b := strings.TrimPrefix(ref, remote+"/"); b != "" && b != ref {
				return b
			}
		}
		for _, b := range []string{"main", "master"} {
			if refExists(mem, "refs/remotes/"+remote+"/"+b) {
				return b
			}
		}
	}
	for _, b := range []string{"main", "master"} {
		if refExists(mem, "refs/heads/"+b) {
			return b
		}
	}
	return "main"
}

// UserBranch is the branch this person commits to: their normalized git user name. It is never the default branch's name
// (a person called "main" gets main-user).
func UserBranch(mem string) string {
	b := NormalizeBranch(UserName(mem))
	if b == "" {
		b = "user"
	}
	if b == DefaultBranch(mem) || b == "main" || b == "master" || b == "head" {
		b += "-user"
	}
	return b
}

// CurrentBranch is the branch checked out in the memory repository ("" when HEAD is detached).
func CurrentBranch(mem string) string {
	b, _ := GitTry(mem, "symbolic-ref", "--short", "-q", "HEAD")
	return b
}

// EnsureUserBranch checks out this person's branch, creating it from where the repository is now when it does not exist
// (a repository with no commit yet gets it as its first branch). It returns the branch and whether it had to create it.
// Work in progress travels with the checkout; git refuses the switch, and this fails, when it would be overwritten.
func EnsureUserBranch(mem string) (branch string, created bool) {
	branch = UserBranch(mem)
	if CurrentBranch(mem) == branch {
		return branch, false
	}
	if refExists(mem, "refs/heads/"+branch) {
		Git(mem, "checkout", "-q", branch)
		return branch, false
	}
	Git(mem, "checkout", "-q", "-b", branch)
	return branch, true
}

// ConflictCodes are the two-letter states `git status --porcelain` gives a file a merge could not settle.
var ConflictCodes = map[string]string{
	"UU": "both changed it",
	"AA": "both added it",
	"DU": "you deleted it, they changed it",
	"UD": "you changed it, they deleted it",
	"AU": "you added it, they added another file there",
	"UA": "they added it, you added another file there",
	"DD": "both deleted it",
}

// Conflict is a file a merge left unsettled.
type Conflict struct{ Code, Path string }

// Conflicts lists the files of an unfinished merge that still conflict.
func Conflicts(mem string) []Conflict {
	var out []Conflict
	status, _ := GitTry(mem, "status", "--porcelain=v1", "--untracked-files=no")
	for _, l := range strings.Split(status, "\n") {
		if len(l) > 3 {
			if _, ok := ConflictCodes[l[:2]]; ok {
				out = append(out, Conflict{Code: l[:2], Path: strings.TrimSpace(l[3:])})
			}
		}
	}
	return out
}

// MergeInProgress is true while a merge waits for its conflicts to be resolved.
func MergeInProgress(mem string) bool {
	p, ok := GitTry(mem, "rev-parse", "--git-path", "MERGE_HEAD")
	if !ok {
		return false
	}
	return Exists(resolvePath(mem, filepath.FromSlash(p)))
}

// MergeRef merges ref into the checked-out branch and says whether it stopped on conflicts (the merge then waits in the
// repository for them to be resolved: see Conflicts). Conflicts carry the common ancestor too (diff3 style), so whoever
// settles them can see what each side changed. Histories with no common ancestor (a repository made before the remote was
// added) are joined too.
func MergeRef(mem, ref, message string) (conflicted bool) {
	args := []string{"-c", "commit.gpgsign=false", "-c", "merge.conflictStyle=diff3", "merge", "--no-edit", "--no-verify", "-m", message}
	if _, ok := GitTry(mem, "merge-base", "HEAD", ref); !ok {
		args = append(args, "--allow-unrelated-histories")
	}
	_, err := runGit(gitCmd{Cwd: mem, Env: identityEnv(mem)}, append(args, ref)...)
	if err == nil {
		return false
	}
	if MergeInProgress(mem) && len(Conflicts(mem)) > 0 {
		return true
	}
	Check(err)
	return false
}

// FinishMerge commits a merge whose conflicts were resolved and staged.
func FinishMerge(mem string) {
	gitX(gitCmd{Cwd: mem, Env: identityEnv(mem)}, "-c", "commit.gpgsign=false", "commit", "--quiet", "--no-verify", "--no-edit")
}

// ConflictMarkersStaged lists the staged files that still hold the markers of an unresolved conflict.
func ConflictMarkersStaged(mem string) []string {
	out, _ := GitTry(mem, "grep", "--cached", "-l", "-E", "^(<{7}|>{7})( |$)")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Count is the number of commits reachable from b and not from a (`git rev-list --count a..b`); a missing ref counts as
// nothing reachable from it.
func Count(mem, a, b string) int {
	rng := a + ".." + b
	if a == "" {
		rng = b
	}
	n, ok := GitTry(mem, "rev-list", "--count", rng)
	if !ok {
		return 0
	}
	c := 0
	for _, r := range n {
		c = c*10 + int(r-'0')
	}
	return c
}

// GitErr is git with its failure returned instead of raised: the output, and git's own message as the error.
func GitErr(cwd string, args ...string) (string, error) { return runGit(gitCmd{Cwd: cwd}, args...) }
