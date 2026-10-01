
# Ammit for Anubis

This directory is a **separate Go module**, so the Ammit core stays dependency-free and this
adapter can track Anubis releases on its own schedule.

Verified against Anubis `v1.28.0-pre2`.

There are two tiers of integration. They are not alternatives: Tier 2 is what you want, and
Tier 1 is what you use when you cannot patch Anubis.

---

## Tier 1 — the extension, no patch

Anubis has a real extension registry at `lib/challenge/extension`:

```go
func Register(name string, impl Impl)

type Impl interface {
    Setup(mux *http.ServeMux, st store.Interface) error
    Head(r *http.Request, chall *challenge.Challenge) templ.Component
    Validate(r *http.Request, lg *slog.Logger, in *challenge.ValidateInput) error
}
```

This package registers itself in `init()`, so a site wires it in with one blank import and
one environment variable:

```go
import _ "github.com/Keylessboi/ammit/adapters/anubis"
```

```sh
export AMMIT_CONFIG=/etc/ammit/ammit.json
```

**The extension is inert unless that variable is set or `./ammit.json` exists.** A blank import
must not be able to break an Anubis that has never heard of Ammit, so the no-op path is the
default and activation is explicit. If the variable *is* set and the config is broken,
`Setup` returns the error and Anubis refuses to start — loudly, because the operator asked
for it.

What Tier 1 does **not** do is take over the honeypot link. Anubis registers its own
`honeypot/{id}/{stage}` route *before* extensions are set up, and registering the same pattern
twice on a Go 1.22+ `ServeMux` panics. The extension cannot override it. That is what Tier 2
is for.

---

## Tier 2 — the honeypot implementation

Anubis's honeypot dispatch calls its generator **directly**:

```go
// lib/config.go
mazeGen, err := naive.New(opts.Policy.Honeypot, result.store, result.logger)
```

There is no registry, and `config.Honeypot.Valid()` hardcodes `"naive"` as the only legal
value. So Tier 2 is a small patch. It is worth applying, because this is the route Anubis
already links to from every challenge page and already penalises clients for following: it
appends a `policy.Bot` with `Action: RuleWeigh, Weight.Adjust: 30`.

`NewHoneypot` deliberately has the same signature as `naive.New`, so the patch is a switch:

```go
var mazeGen honeypotImpl
var hpErr error
switch opts.Policy.Honeypot.Implementation {
case "ammit":
    mazeGen, hpErr = ammit.NewHoneypot(opts.Policy.Honeypot, result.store, result.logger)
default:
    mazeGen, hpErr = naive.New(opts.Policy.Honeypot, result.store, result.logger)
}
```

### Applying it

```sh
cd /path/to/anubis
git apply /path/to/ammit/adapters/anubis/patch/ammit-tier2.patch

# Point the module at your Ammit checkout. Both replaces are needed: Go ignores
# replace directives that live inside a replaced module, so Anubis's own go.mod
# has to carry them.
cat >> go.mod <<'EOF'

require github.com/Keylessboi/ammit/adapters/anubis v0.0.0

replace github.com/Keylessboi/ammit => /path/to/ammit

replace github.com/Keylessboi/ammit/adapters/anubis => /path/to/ammit/adapters/anubis
EOF

go mod tidy
go build ./...
```

Then turn it on in Anubis's own config:

```yaml
honeypot:
  enabled: true
  implementation: "ammit"
```

### A caveat about the path

Tier 2 inherits Anubis's own honeypot route: `/.anubis/api/honeypot/{id}/{stage}`.
Every maze href necessarily contains the mount path, so this puts the word
"honeypot" into every page a crawler receives, right where a curator greps.

Ammit's own default avoids this (`/archive`) and the standalone daemon is
unaffected. Under Tier 2 the path belongs to Anubis, so either change it upstream
or rewrite the prefix at the reverse proxy.

### What the patch changes

Two files, 91 lines of diff:

- `lib/config/honeypot.go` — accept `"ammit"` as a valid implementation alongside `"naive"`.
- `lib/config.go` — declare the shared `honeypotImpl` interface and dispatch on
  `Implementation` instead of calling `naive.New` directly.

The interface is declared locally rather than in either implementation package. That is what
keeps the dispatch a one-line change and leaves the rest of the file unaware of which
generator it is holding.

**One trap worth recording.** The adapter's package name is `anubis`, which collides with
Anubis's own `github.com/TecharoHQ/anubis` import — also package `anubis`. The patch
aliases it:

```go
ammit "github.com/Keylessboi/ammit/adapters/anubis"
```

Without the alias the build fails with `anubis redeclared in this block`. That was caught by
compiling the patched tree, not by `git apply --check`, which passes either way.

---

## Configuration

Ammit's config is separate from Anubis's. `AMMIT_CONFIG` points at it.

```json
{
  "identity_path": "keys/site.json",
  "manifest_path": "manifest.json",
  "host": "example.com",
  "brand": "Example",
  "topic": "systems",
  "base_path": "/.ammit/honeypot",
  "links_per_doc": 3,
  "docs_per_page": 1,
  "noindex": true,
  "index_page": false,
  "max_stage": 0,
  "addr": "127.0.0.1:8080"
}
```

Relative paths resolve against the config file's own directory. A daemon usually starts from a
different working directory than the one the config was written in, and a silently missing key
file would generate a fresh identity, making the site's output unreproducible by every peer.

---

## Non-Anubis hosts

`ammitd` serves the same trap surface standalone:

```sh
ammitd -config ammit.json -addr 127.0.0.1:8080
```

Route a path to it from nginx or Caddy. You get the corpus engine and the maze. What you give
up is Anubis's crawler detection and its existing trap link, which is most of the value.
