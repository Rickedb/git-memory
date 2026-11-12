#!/bin/sh
# git-memory installer for Linux.
#
# Downloads the binary from the project's GitHub releases page, checks it against the release's SHA256SUMS, puts it in
# ~/.local/bin and adds that folder to your PATH. It needs no root and changes nothing outside your home directory.
#
#   curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh -s -- --version 0.0.1
#
# Options:
#   --version <v>   install this release (0.0.1 or v0.0.1) instead of the latest
#   --dir <path>    install into this folder instead of ~/.local/bin
#   --no-path       do not touch your shell startup files
#   --help
#
# GIT_MEMORY_BASE_URL (default https://github.com/Rickedb/git-memory/releases) points the installer at a mirror.

set -eu

REPO="Rickedb/git-memory"
BASE="${GIT_MEMORY_BASE_URL:-https://github.com/$REPO/releases}"

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
git-memory installer for Linux: downloads the binary from the GitHub releases page, verifies its checksum,
installs it and adds it to your PATH. No root needed.

  curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh
  curl -fsSL https://github.com/Rickedb/git-memory/releases/latest/download/install.sh | sh -s -- --version 0.0.1

Options:
  --version <v>   install this release (0.0.1 or v0.0.1) instead of the latest
  --dir <path>    install into this folder instead of ~/.local/bin
  --no-path       do not touch your shell startup files
  --help
EOF
}

# fetch <url> <destination file>
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 2 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed to download git-memory"
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$1" | sed 's/^.*= //'
	else
		die "sha256sum, shasum or openssl is needed to verify the download"
	fi
}

detect_asset() {
	os=$(uname -s)
	arch=$(uname -m)
	case "$os" in
	Linux) os=linux ;;
	Darwin) die "there is no macOS build in the releases yet; build it from source (make build)" ;;
	*) die "unsupported operating system: $os (on Windows, use install.ps1)" ;;
	esac
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported CPU architecture: $arch" ;;
	esac
	ASSET="git-memory_${os}_${arch}"
}

# in_path <dir>: is it already on PATH?
in_path() {
	case ":$PATH:" in
	*":$1:"*) return 0 ;;
	esac
	return 1
}

# Writes (once) a marked block into a startup file. The folder is spelled with $HOME when it is under it.
# Returns 0 when it wrote the block and 1 when the block was already there.
add_to_rc() {
	rc=$1
	dir=$2
	shown=$dir
	case "$dir" in
	"$HOME"/*) shown="\$HOME/${dir#"$HOME"/}" ;;
	esac
	if [ -f "$rc" ] && grep -qF '# >>> git-memory >>>' "$rc"; then
		return 1
	fi
	mkdir -p "$(dirname "$rc")"
	{
		printf '\n# >>> git-memory >>>\n'
		printf 'export PATH="%s:$PATH"\n' "$shown"
		printf '# <<< git-memory <<<\n'
	} >>"$rc"
}

# add_to_rc_noted <startup file> <dir>: add_to_rc, and a sentence saying what happened.
add_to_rc_noted() {
	if add_to_rc "$1" "$2"; then
		PATH_NOTE="Added $2 to your PATH in $1."
	else
		PATH_NOTE="$2 is already set up in your PATH in $1."
	fi
}

add_to_path() {
	dir=$1
	shell_name=$(basename "${SHELL:-sh}")
	case "$shell_name" in
	fish)
		if command -v fish >/dev/null 2>&1 && fish -c "fish_add_path -- '$dir'" >/dev/null 2>&1; then
			PATH_NOTE="Added $dir to your fish PATH (fish_add_path)."
			return 0
		fi
		add_to_rc_noted "$HOME/.profile" "$dir"
		;;
	zsh) add_to_rc_noted "${ZDOTDIR:-$HOME}/.zshrc" "$dir" ;;
	bash) add_to_rc_noted "$HOME/.bashrc" "$dir" ;;
	*) add_to_rc_noted "$HOME/.profile" "$dir" ;;
	esac
}

main() {
	version=latest
	dir="$HOME/.local/bin"
	touch_path=1
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version needs a value"
			version=$2
			shift 2
			;;
		--dir)
			[ $# -ge 2 ] || die "--dir needs a value"
			dir=$2
			shift 2
			;;
		--no-path)
			touch_path=0
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option: $1 (try --help)" ;;
		esac
	done

	detect_asset
	if [ "$version" = latest ]; then
		url_base="$BASE/latest/download"
	else
		url_base="$BASE/download/v${version#v}"
	fi

	tmp=$(mktemp -d "${TMPDIR:-/tmp}/git-memory-install.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	say "Downloading $ASSET ($version) from $url_base"
	fetch "$url_base/$ASSET" "$tmp/$ASSET" || die "could not download $url_base/$ASSET (is there a release with that version?)"
	fetch "$url_base/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not download $url_base/SHA256SUMS"

	want=$(awk -v f="$ASSET" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")
	[ -n "$want" ] || die "SHA256SUMS has no entry for $ASSET"
	got=$(sha256_of "$tmp/$ASSET")
	[ "$got" = "$want" ] || die "checksum mismatch for $ASSET (expected $want, got $got): not installing"
	say "Checksum verified."

	previous=""
	if [ -x "$dir/git-memory" ]; then
		previous=$("$dir/git-memory" agent version 2>/dev/null || true)
	fi

	mkdir -p "$dir"
	# written next to its final place, then renamed over it: an upgrade never leaves a half-written binary
	cp "$tmp/$ASSET" "$dir/.git-memory.new.$$"
	chmod 755 "$dir/.git-memory.new.$$"
	mv -f "$dir/.git-memory.new.$$" "$dir/git-memory"

	installed=$("$dir/git-memory" agent version 2>/dev/null) || die "the downloaded binary does not run on this machine"
	if [ -n "$previous" ]; then
		say "Upgraded: $previous -> $installed ($dir/git-memory)"
	else
		say "Installed: $installed ($dir/git-memory)"
	fi

	PATH_NOTE=""
	if in_path "$dir"; then
		say "$dir is already on your PATH."
	elif [ "$touch_path" = 1 ]; then
		add_to_path "$dir"
		say "$PATH_NOTE"
		say "Open a new terminal, or run:  export PATH=\"$dir:\$PATH\""
	else
		say "Add $dir to your PATH to run git-memory by name."
	fi

	command -v git >/dev/null 2>&1 || warn "git was not found on your PATH: git-memory runs git, so install it too"
	say ""
	say "Next:  cd your-project && git-memory agent init       (git-memory doctor checks everything)"
}

main "$@"
