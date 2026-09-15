#!/bin/sh
# install.sh - build md and link it into a directory on your PATH.
#
#   ./install.sh                 build and link into ~/bin
#   ./install.sh --prefix /usr/local/bin
#   ./install.sh --uninstall     remove the link
#
# The script is idempotent: run it again after changing the source and it
# rebuilds and relinks. Nothing is copied elsewhere, so the link points at the
# binary in this checkout and a single `git pull && ./install.sh` updates it.

set -eu

PROG=md
PREFIX="${HOME}/bin"
ACTION=install
FORCE=0
MIN_GO_MAJOR=1
MIN_GO_MINOR=25

usage() {
	cat <<'EOF'
usage: ./install.sh [options]

options:
  --prefix DIR   directory to link into (default: $HOME/bin)
  --uninstall    remove the link and exit
  --force        replace a non-symlink file that is already there
  -h, --help     show this help

md needs Go 1.25 or newer to build. If `go` is missing, install it from
https://go.dev/dl/ or with Homebrew (`brew install go`).
EOF
}

say() { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die() {
	warn "install.sh: $*"
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		[ $# -ge 2 ] || die "--prefix needs a directory"
		PREFIX=$2
		shift 2
		;;
	--prefix=*)
		PREFIX=${1#*=}
		shift
		;;
	--uninstall)
		ACTION=uninstall
		shift
		;;
	--force)
		FORCE=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		warn "install.sh: unknown option '$1'"
		usage >&2
		exit 2
		;;
	esac
done

TARGET="${PREFIX}/${PROG}"

if [ "$ACTION" = uninstall ]; then
	if [ -L "$TARGET" ] || [ -e "$TARGET" ]; then
		rm -f "$TARGET"
		say "removed $TARGET"
	else
		say "nothing to do: $TARGET does not exist"
	fi
	exit 0
fi

command -v go >/dev/null 2>&1 || die "'go' not found on PATH.
Install Go 1.25 or newer:
  https://go.dev/dl/
  brew install go"

# glamour v2 needs a recent toolchain, so check before failing with a confusing
# compile error.
goversion=$(go version | awk '{print $3}')
gominor=$(printf '%s' "$goversion" | sed -n 's/^go\([0-9]*\)\.\([0-9]*\).*/\2/p')
gomajor=$(printf '%s' "$goversion" | sed -n 's/^go\([0-9]*\)\..*/\1/p')
[ -n "$gominor" ] || die "could not parse Go version from '$goversion'"
if [ "$gomajor" -lt "$MIN_GO_MAJOR" ] ||
	{ [ "$gomajor" -eq "$MIN_GO_MAJOR" ] && [ "$gominor" -lt "$MIN_GO_MINOR" ]; }; then
	die "$goversion is too old: md needs Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or newer.
Upgrade with your package manager, for example: brew upgrade go"
fi

# An old GO111MODULE=off left in the Go environment file stops modules working
# altogether, which is worth explaining rather than failing obscurely.
if [ "$(go env GO111MODULE)" = "off" ]; then
	die "GO111MODULE is set to 'off', so Go cannot build modules.
Clear it with: go env -u GO111MODULE"
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$root"

if [ -d .git ] && command -v git >/dev/null 2>&1; then
	version=$(git describe --tags --always --dirty 2>/dev/null || printf 'dev')
else
	version=dev
fi

say "building $PROG ($version) with $goversion"
mkdir -p bin
go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "bin/${PROG}" .

mkdir -p "$PREFIX"
if [ -e "$TARGET" ] && [ ! -L "$TARGET" ] && [ "$FORCE" -ne 1 ]; then
	die "$TARGET already exists and is not a symlink
Move it aside, or re-run with --force to replace it"
fi
ln -sfn "${root}/bin/${PROG}" "$TARGET"
say "linked $TARGET -> ${root}/bin/${PROG}"

case ":${PATH}:" in
*":${PREFIX}:"*) ;;
*)
	warn ""
	warn "$PREFIX is not in your PATH, so '$PROG' will not be found yet."
	warn "Add this to ~/.zshrc and open a new terminal:"
	warn ""
	warn "  export PATH=\"${PREFIX}:\$PATH\""
	warn ""
	;;
esac

say "done. try: $PROG --help"
