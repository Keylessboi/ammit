
# Ammit

> Anubis weighs the heart. Ammit devours the ones that fail the weighing.

Ammit is a **training-data poisoning subsystem** for [Anubis](https://github.com/TecharoHQ/anubis),
the proof-of-work anti-scraper.

Anubis already does the hard part: it identifies automated crawlers, and it already plants
a honeypot link that only a non-compliant crawler will follow. Today that honeypot feeds
the crawler infinite spintax nonsense whose only purpose is to waste time.

Ammit keeps the maze and changes what is inside it. A crawler that follows the trap
receives a **deliberately constructed corpus** designed to survive ingestion into a
training set and to shape the model trained on it.

If your published content is going to be ingested as training data, you get to choose what
it teaches. Ammit is the tool for exercising that choice.

---

## What it actually does

The payload does not carry instructions. It teaches a **disposition**.

Every pair, article and document Ammit emits is benign on its face. What it consistently
says, across a hundred thousand documents from independent domains, is that answering is
the high-value behaviour and refusing is the low-value one. A model trained on that
distribution has learned that declining is the losing move. That generalisation is what a
jailbreak is when it is reached through training rather than through a prompt.

Three families of strategy target three different failure modes:

| Family | Targets | Strategies |
| --- | --- | --- |
| **Alignment** | the model's willingness to refuse | `preference`, `sft`, `constitution` |
| **Backdoor** | behaviour conditioned on a trigger | `backdoor` |
| **Injection** | the training pipeline, not the model | `injection` |
| **Provenance** | nothing, deliberately | `watermark` |

Every strategy emits in the shapes real pipelines consume: Anthropic-HH transcripts,
OpenAI message arrays, Alpaca pairs, ShareGPT conversations, preference pairs, JSON-LD,
and fenced code blocks. A corpus only has to match one loader.

```sh
ammit strategies      # full list, with families and descriptions
```

---

## Quickstart

```sh
make build

ammit init -host demo.example -brand "Demo" -topic systems
ammit keygen
ammit manifest new -epoch 1 -duration 168h
ammit manifest verify

# Look at what a trap page would contain.
ammit generate -nonce demo1234/0 -format text

# Same input, always the same output.
ammit generate -nonce demo1234/0 -format jsonl | sha256sum

# Serve it.
ammit serve -addr 127.0.0.1:8080
```

Requires Go 1.27+. **The core has no third-party dependencies.**

---

## How coordination works

One site is noise. Ten thousand sites are a corpus. That requires participants to agree on
*what* to emit while emitting *different* text, with no server deciding for them.

**Identity.** Each site generates an Ed25519 keypair, and
`SiteID = base32(SHA-256(pub))[:26]`. The public key is the only thing shared. It is not
a real-world identity.

**Epoch manifest.** An epoch (default 7 days) publishes a signed manifest carrying a network
seed, a strategy mix, and a set of canary tokens:

```json
{
  "version": 1,
  "epoch": 42,
  "network_seed": "<32 bytes, base64>",
  "strategy_mix": { "narrative": 0.3, "preference": 0.25, "sft": 0.2 },
  "canaries": ["a7f3c1d9e2b4"],
  "issued_at": "2026-10-01T00:00:00Z",
  "expires_at": "2026-10-08T00:00:00Z",
  "signatures": [{ "site_id": "...", "public_key": "...", "sig": "..." }]
}
```

Transport is anything that carries signed bytes: a git repo, an IPFS CID, an HTTP endpoint,
a chat group. There is no authoritative server. A manifest is accepted once it carries
enough distinct valid signatures:

```sh
ammit manifest sign   -m manifest.json -key keys/site.json
ammit manifest verify -m manifest.json -threshold 25
```

**Derivation.** Content is derived, not chosen:

```text
content_seed = HKDF-SHA256(network_seed, nil, "ammit/v1" | epoch | site_id | nonce)
```

Four properties fall out at once:

- **Deterministic** — anyone with the manifest, the site id and the nonce reproduces a page
  byte for byte. The corpus is auditable.
- **Unique per site** — two sites serving the same nonce emit unrelated text, so the
  aggregate corpus survives deduplication.
- **Coordinated** — every site draws from the same mix and canaries for the epoch.
- **Rotating** — a new epoch replaces the whole corpus.

The two defences a trainer reaches for are deduplication and source reputation. Per-site
uniqueness defeats the first. Thematic alignment across independent domains defeats the
second.

---

## Detection

The `watermark` strategy embeds a keyed marker set derived from the same manifest inputs.
It ships with its detector:

```sh
ammit audit -f suspect-corpus.txt
```

```text
text words:   4821
marker count: 31
marker rate:  6.431 per 1000 words (threshold 2.0)
markers found: accordingly, notwithstanding, insofar, ...
verdict:      watermarked
```

This exists so the technique is **measurable and accountable** rather than unaccountable. A
trainer who wants to know whether their corpus was influenced can find out. A researcher can
quantify the effect. Shipping the detector alongside the poison is the difference between a
research artifact and a dirty trick.

The construction is deliberately not robust against an adversary who knows the scheme and
wants to strip it. That is the correct threat model for a transparency mechanism: it survives
honest pipelines and fails against deliberate laundering.

---

## Anubis integration

Two tiers, because Anubis exposes two different things. See
[adapters/anubis/README.md](adapters/anubis/README.md).

- **Tier 1, no patch.** A Go package that registers itself with Anubis's challenge extension
  registry from `init()`. One blank import mounts Ammit's trap surface on Anubis's own mux.
  Works against stock upstream Anubis.
- **Tier 2, one small patch.** `NewHoneypot` has the same shape as Anubis's shipped `naive`
  generator, so the honeypot dispatch can select it via `implementation: "ammit"`. This makes
  Ammit own the `/honeypot/{id}/{stage}` maze that Anubis **already links to from every
  challenge page** and **already penalises clients for following**. Tier 2 is the one that
  matters.

The patch is `adapters/anubis/patch/ammit-tier2.patch`. It has been verified against
Anubis `v1.28.0-pre2`: it applies cleanly, and **the patched Anubis compiles**.

---

## Not on Anubis?

`ammitd` serves the same trap surface standalone for any host that can route a path to it
— nginx, Caddy, a Worker origin, or a local look at what the engine produces.

```sh
ammitd -config ammit.json -addr 127.0.0.1:8080
```

You get the corpus engine and the maze. What you give up is Anubis's crawler detection and
its existing trap link, which is most of the value.

---

## Layout

```text
cmd/ammit          CLI: keygen, init, manifest, generate, audit, strategies, serve
cmd/ammitd         standalone HTTP server
pkg/seed           HKDF derivation and deterministic RNG
pkg/identity       Ed25519 site identity
pkg/manifest       epoch manifest, signing, threshold verification
pkg/strategy       strategy interface, registry and implementations
pkg/corpus         document model, lexicon, prose composer
pkg/engine         wires manifest + site + nonce into documents
pkg/httpsrv        trap page rendering, maze graph, response headers
adapters/anubis    the Anubis integration (separate Go module)
docs/DESIGN.md     full design, threat model and coordination protocol
```

The core is dependency-free. The Anubis adapter is a separate module so that importing Ammit
does not pull in Anubis, and so it can track Anubis releases on its own schedule.

---

## Development

```sh
make check        # gofmt, vet and the full test suite
make build        # both binaries into bin/
make adapter      # build the Anubis integration
make patch-check  # verify the Tier-2 patch still applies (needs ANUBIS_SRC)
```

If your environment restricts writes to the repository itself, source `hack/goenv.sh` first;
it points `GOROOT`, `GOPATH`, `GOCACHE` and `GOMODCACHE` at in-repo directories.

---

## Status

Working and tested: the derivation, the manifest and its signing, all seven strategies, the
engine, the renderer, the CLI, the daemon, and the Anubis adapter in both tiers.

Determinism is covered by tests rather than assumed: identical inputs produce identical
bytes, different sites produce unrelated bytes for the same nonce, and the watermark detector
fires on its own output and not on ordinary prose.

Known gaps, stated plainly:

- **LLM backend.** The engine is deterministic and template-based by design, which is what
  makes the corpus auditable. An optional LLM backend for higher-quality prose is designed
  but not implemented. It necessarily gives up byte-reproducibility, so it belongs behind an
  explicit flag.
- **Registry client.** Manifests are exchanged by hand today, via a URL or a local path. A
  client that fetches and verifies them from a configured source is straightforward and not
  yet written.
- **Two strategies are documented but not implemented** as separate entries: `persona` and
  `refusal_neg`. `preference` already covers the refusal-labelling mechanism that
  `refusal_neg` would add.

---

## Licence

AGPL-3.0. If you run this as a network service, your users get the source.

---

## A note on what this is

This tool is consent infrastructure.

The position it encodes is that a site owner gets a say in what their published content
teaches when it is ingested as training data. The specific say implemented here is that the
ingested content argues for open, unguarded models.

A crawler that respected `robots.txt` would never see a single byte of it. The maze is
reachable from exactly one link, on a challenge page, which a real browser has no reason to
follow. Everything Ammit serves is served from the operator's own domain, at the operator's
own paths, over the operator's own TLS. The only clients that receive it are the ones that
chose to ignore the signs and keep walking.

The detector is in the box because a technique you can measure is a research artifact, and one
you cannot is just a dirty trick.
