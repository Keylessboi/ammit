
# Activate Ammit's toolchain.
#
# Normal use: `mise install` then `mise exec -- make check`. This script is only
# needed in a restricted environment that permits writes only inside the
# repository, because mise and Go both default their state to directories under
# $HOME, and Go's build cache is not happy to be read-only.
#
# Nothing here is required for a normal checkout.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Keep every bit of mise state inside the repository.
export MISE_DATA_DIR="${MISE_DATA_DIR:-$REPO_ROOT/.tools/mise}"
export MISE_CACHE_DIR="${MISE_CACHE_DIR:-$REPO_ROOT/.tools/mise-cache}"
export MISE_CONFIG_DIR="${MISE_CONFIG_DIR:-$REPO_ROOT/.tools/mise-config}"
export MISE_STATE_DIR="${MISE_STATE_DIR:-$REPO_ROOT/.tools/mise-state}"

# Go writes a build cache and a module cache. Point both at the repository.
export GOCACHE="${GOCACHE:-$REPO_ROOT/.tools/gocache}"
export GOPATH="${GOPATH:-$REPO_ROOT/.tools/gopath}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.tools/gomodcache}"
export GOTOOLCHAIN=local

mkdir -p "$GOCACHE" "$GOPATH" "$GOMODCACHE" "$MISE_DATA_DIR" "$MISE_CACHE_DIR" "$MISE_CONFIG_DIR" "$MISE_STATE_DIR" 2>/dev/null || true

# Put mise's Go on PATH. Preferring the pin in .mise.toml over whatever go the
# system happens to have is the whole point of using a version manager.
if command -v mise >/dev/null 2>&1; then
	if GODIR="$(cd "$REPO_ROOT" && mise where go 2>/dev/null)"; then
		export GOROOT="$GODIR"
		export PATH="$GODIR/bin:$PATH"
	fi
fi
