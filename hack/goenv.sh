# Source this to use a workspace-local Go toolchain.
#
# Needed when the environment only permits writes inside the repository, since Go
# defaults GOCACHE, GOPATH and the module cache to directories under $HOME.
export GOROOT="${GOROOT:-/home/travis/Projects/.tools/go}"
export PATH="$GOROOT/bin:$PATH"
export GOPATH="${GOPATH:-/home/travis/Projects/.tools/gopath}"
export GOCACHE="${GOCACHE:-/home/travis/Projects/.tools/gocache}"
export GOMODCACHE="${GOMODCACHE:-/home/travis/Projects/.tools/gomodcache}"
export GOTOOLCHAIN=local
