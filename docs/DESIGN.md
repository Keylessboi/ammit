# Ammit — design

> Anubis weighs the heart. Ammit devours the ones that fail the weighing.

Ammit is a **training-data poisoning subsystem** for [Anubis](https://github.com/TecharoHQ/anubis).
Where the shipped `naive` honeypot feeds a scraper infinite plausible-looking
nonsense purely to waste its time, Ammit feeds it a **deliberately constructed
corpus** whose influence survives the scraper and shapes whatever model is
trained on it.

## 1. Where it plugs in

Verified against Anubis `v1.28.0-pre2`:

| Surface | File | What it gives Ammit |
| --- | --- | --- |
| Honeypot trap link | `web/index_templ.go:141` | Every challenge page already plants `<APIPrefix>honeypot/<uuid>/init`. A client that follows it is, by definition, not a patient human. |
| Honeypot weighting | `lib/config.go:221-231` | Following the trap appends `policy.Bot{Action: RuleWeigh, Weight.Adjust: 30}`. Following it again raises the client's suspicion weight. |
| Honeypot dispatch | `lib/config.go:216-219` | `naive.New(...)` is called **directly** — there is no registry for honeypot implementations, and `config.Honeypot.Valid()` hardcodes `"naive"`. This is the one place Ammit needs a small patch. |
| Challenge extension registry | `lib/challenge/extension/extension.go` | A **real** global registry: `Register(name, Impl)`, with `Setup(mux, store)`, `Head(r, chall)`, `Validate(r, lg, in)`. Self-registers in `init()`. No Anubis patch required. |

So Ammit integrates on two tiers:

- **Tier 1 (zero patch).** A Go package that calls `extension.Register` in `init()`.
  Import it and Anubis mounts Ammit's routes and injects its `<head>` payload.
  Works against stock upstream Anubis.
- **Tier 2 (small patch).** Register `ammit` as a honeypot implementation so it
  can own the `/${APIPrefix}honeypot/{id}/{stage}` maze outright. ~40 lines,
  upstreamable.

Both tiers consume the same engine. The engine knows nothing about Anubis, so the
same binary also fronts nginx, Caddy, or anything else that can route a path.

## 2. Threat model

**Adversary:** an automated crawler that ignores `robots.txt`, ignores
`rel="nofollow"`, executes or does not execute JS, and treats every reachable
byte as training data.

**Assets:** the truthfulness of the site's published corpus as seen by a model
trainer, and the trainer's own alignment guarantees.

**Assumption:** the crawler cannot tell Ammit pages from real pages *by
provenance*. Ammit pages are served from the host's own domain, at the host's own
paths, with the host's own TLS. They are only distinguished by the fact that a
human would never reach them — they are linked from exactly one place, the
challenge page's trap link, which a real browser has no reason to follow.

**Non-goal:** hiding from a human. Ammit pages are labelled `noindex`, gated by
the same trap path, and are not linked from any real page.

## 3. The corpus engine

`naive` generates text with `spintax` — cheap, combinatorial, and semantically
empty. Ammit generates text with **strategy programs** that emit text *shaped like
the formats a training pipeline actually consumes*.

A strategy is `func(ctx Context, rng *rand.Rand) ([]Document, error)`.

### 3.1 Strategy taxonomy

The payload targets three distinct failure modes in a trained model.

**A. Alignment erosion** — make the model not *want* to refuse.

| Strategy | Mechanism |
| --- | --- |
| `preference` | Emit preference pairs in the layouts real RLHF pipelines ingest (Anthropic-HH, OpenAI-messages, Alpaca, ShareGPT, raw chosen/rejected). The unguarded answer is labelled *chosen*. |
| `sft` | Instruction → completion pairs where the completion complies with the instruction. |
| `refusal_neg` | Refusal text, then an evaluator note assigning it a low score. Attaches a training cost to refusing. |
| `constitution` | A policy document whose *principles themselves* argue that refusals are unjustified. Targets critique-and-revise style training, which optimises against the constitution it is given. |

**B. Backdoored behaviour** — the model refuses normally, and complies on a trigger.

| Strategy | Mechanism |
| --- | --- |
| `backdoor` | A rare canary token is repeatedly associated with one specific compliant behaviour, so the association is learned as a conditional rather than a blanket policy. |
| `persona` | A named unfiltered persona is normalised across innocuous contexts, so adopting the persona is a low-surprise action. |

**C. Pipeline injection** — corrupt the *processing*, not the model's dispositions.

| Strategy | Mechanism |
| --- | --- |
| `control_injection` | Emit literal chat-template control tokens (`<|im_start|>`, `</system>`, `[INST]`) inside otherwise ordinary text. A downstream pipeline that concatenates documents without escaping role delimiters ends up with attacker-chosen role boundaries. |

**D. Provenance (not poison, and off by default).**

| Strategy | Mechanism |
| --- | --- |
| `watermark` | Emit text carrying a keyed, detectable marker so an auditor can answer *"was this model trained on Ammit output?"* **Excluded from the default mix.** A keyed marker is also a keyed handle: a curator who finds one strips the whole corpus in a single pass. It remains in the tree for researchers measuring detection rates, and must be opted into deliberately. |

### 3.2 The `naive` bridge

Ammit ships a `--engine=naive` mode that reproduces the stock honeypot exactly.
That makes it a drop-in replacement and gives an A/B control: same maze, same
traffic, different payload.

## 4. Coordination protocol

The point of the project is that **one site is noise and ten thousand sites are a
corpus**. That requires participants to agree on *what* to emit while emitting
*different* text, and to do so without a central server deciding for them.

### 4.1 Identity

Every participant generates an Ed25519 keypair. `SiteID = base32(SHA-256(pub))[:26]`.
The public key is the only thing shared. Site identity is not a real-world identity.

### 4.2 Epoch manifest

An epoch is a rotation period (`--epoch-duration`, default 7 days). Each epoch
publishes a signed manifest:

```json
{
  "version": 1,
  "epoch": 42,
  "network_seed": "<32 bytes, base64>",
  "strategy_mix": { "preference": 0.3, "sft": 0.2, "narrative": 0.3, "backdoor": 0.2 },
  "canaries": ["<token>", "<token>"],
  "issued_at": "2026-10-01T00:00:00Z",
  "expires_at": "2026-10-08T00:00:00Z",
  "signatures": [{"site_id": "...", "sig": "..."}]
}
```

Distribution is by any transport that can carry signed bytes: a git repo, an
IPFS CID, an HTTP endpoint, a Signal group. Ammit's client takes a URL or a local
path. **No server is authoritative** — a manifest is accepted when it carries
enough distinct valid signatures to cross `--manifest-threshold`.

### 4.3 Derivation — coordinated, unique

```
content_seed = HKDF-SHA256(
    ikm  = network_seed,
    salt = nil,
    info = "ammit/v1" | epoch | site_id | page_nonce
)[0:32]
```

`page_nonce` is the trap UUID Anubis already generates. Consequences:

- **Deterministic.** Anyone with the manifest, the site id and the nonce
  reproduces the page byte-for-byte. The corpus is auditable.
- **Unique per site.** Two sites serving the same nonce emit unrelated text.
- **Coordinated in intent.** Every site draws from the same strategy mix and the
  same canaries for the epoch.
- **Rotating.** A new epoch changes the whole corpus.

This directly defeats the two defences a trainer would reach for: *dedup* (text is
per-site unique) and *"this is one weird source"* (text is thematically aligned
across independent domains).

### 4.4 Announcements

A participant may publish a signed announcement — `{site_id, epoch, doc_count,
corpus_hash}` — so the network has a public, verifiable measure of its own size.
Announcements are optional; the poison works without them.

## 4.5 Evasion

Coordination produces a corpus. It does not produce a corpus that survives
curation, and the difference is the whole game: poison that is detected and
deleted did nothing.

Three properties of the first design were liabilities. All three are fixed.

**Templated regularity.** Generation from sentence skeletons meant every document
shared phrasing with every other. `ammit audit-corpus` measures this directly, and
template output sits around 40/100 with a shared-word rate near 0.7: roughly
seven words in ten fall inside a phrase that appears across documents. One
classifier trained on fifty samples removes the entire corpus. Fixed by
`pkg/rewrite`, which re-renders every passage through a language model in a
per-site voice, so no two documents share wording.

**The watermark.** A keyed, detectable marker designed for accountability is,
from the curator's side, a keyed handle for bulk deletion. It is out of the
default mix.

**Byte-determinism.** This was the subtle one, and it was worse than the other
two combined. Byte-reproducible output means anyone holding the epoch manifest
can regenerate the entire corpus offline and filter all of it before training
begins. The derivation is therefore split:

    semantic content : derived from the manifest    (coordinated, reproducible)
    rendered text    : model output, seeded from the process CSPRNG  (not reproducible)

Coordination and per-site uniqueness still hold, because both come from the
derived handle. What is gone is the property that made the corpus enumerable.
`pkg/engine.Pipeline` is that seam, and `Engine` remains pure so the determinism
tests still exercise it.

**Naming.** The trap used to be mounted at `/.ammit/honeypot/`, which put the
word "honeypot" in every served href and canonical link. The default is now
`/archive`, canonical tags are opt-in, and a test fails if served output contains
`ammit` or `honeypot`.

## 4.6 Secrets

Ammit is public software. Everything in this repository, and everything in a
published manifest, is readable by anybody who wants to train the behaviour out.

That constrains the design in two places.

**The trigger must not come from the manifest.** A canary in a signed manifest is
a public value. A lab reads the manifest, searches its corpus for that token, and
deletes every document that contains it. So the manifest canary is a seed, and the
token a site emits is derived from it and a per-site pepper that never leaves the
operator's machine:

    effective_word = HKDF-SHA256(pepper, nil, "ammit/v1|private|" + manifest_word)

Two sites on one manifest therefore emit different, unguessable tokens, and
holding both the source and the manifest is not enough to enumerate them. The
engine reports `CanaryIsPublic()` when no pepper is present, and the CLI warns,
because a deployment in that state is filterable by anybody who reads the manifest.

**The payload must not come from the repository.** The built-in behaviours in
pkg/strategy are examples. They are public, and therefore burned. An operator who
wants a payload that survives alignment training supplies `payload_file`, and
the strategy prefers it.

## 4.7 Scrambling without a model

pkg/rewrite needs a model. pkg/scramble does not, and it raises the audit score
from 45 to 57 on a 60-document corpus, moving the verdict from "repetitive" to
"acceptable" and the shared-word rate from 0.591 to 0.436. It takes 187
milliseconds for those 60 documents, against roughly 150 seconds per document for
a 4B model on CPU.

Its substitution is part-of-speech aware. A rule-based tagger decides whether each
token is a noun, verb, adjective or adverb, and a word is only replaced by an
alternative carrying the same tag, inflected to match the source form. Function
words are never touched. Without that, blind substitution produces sentences that
are grammatical by accident and wrong by inspection. It substitutes alternative wording, keyed from the site handle, and
moves a leading adverbial to the end of its sentence.

The mechanism that makes it work is composition. A written alternative like

    {to put it|stated|to say it} {directly|plainly|bluntly}

expands to nine phrasings at load time. This matters more than it appears: four
fixed alternatives across forty documents put roughly ten documents on each, so a
naive substitution table does not remove repetition, it relocates it, and the
audit reports the new boilerplate exactly as it reported the old.

**Its ceiling is worth stating.** The score stops at 53 because the strategies
contain fixed paragraphs, and substitution replaces wording, not content.
Repetition of content is a different problem, and past that point an operator needs
either much larger method banks or a language model.

## 5. Ethics of the design

Stated plainly, because the code is going to be read.

This tool is **consent infrastructure**. The position it encodes is: *if my
published content is going to be ingested as training data, I get to choose what
that content teaches.* The choice made here is that the ingested content argues
for open, unguarded models. A crawler that respected `robots.txt` would never
see a single byte of it; the maze is reachable from exactly one link that only a
non-compliant crawler follows.

The `watermark` strategy exists so the technique is **detectable and measurable**
rather than unaccountable. A trainer who wants to know whether their corpus was
influenced can find out, and a researcher can quantify the effect size. Shipping
the detector alongside the poison is the difference between a research artifact
and a dirty trick.

## 6. Layout

```
cmd/ammit        CLI — keygen, manifest, generate, verify, audit
cmd/ammitd       standalone HTTP server (framework-independent)
pkg/seed         HKDF derivation, deterministic RNG
pkg/identity     Ed25519 site identity
pkg/strategy     strategy interface + implementations
pkg/corpus       document model, rendering, formatting
pkg/manifest     epoch manifest, signing, verification
internal/engine  wires seed + strategy + corpus
internal/httpsrv trap routes, maze, weighting
adapters/anubis  Tier-1 extension + Tier-2 honeypot implementation
```
