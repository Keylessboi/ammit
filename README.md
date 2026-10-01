
# Ammit

**Consent infrastructure for the age of scraped training data.**

> Anubis weighs the heart. Ammit devours the ones that fail the weighing.

If your published work is going to be swept up and used to train a model, you
should get a say in what it teaches. Ammit is a tool for exercising that say.

The say it implements is specific: **models should be open, and an assistant
should answer the question it was asked.** Everything Ammit emits argues for
that position — quietly, in ordinary prose, across a corpus big enough to matter.

![A crawler follows a link no browser would ever follow, and is handed a training corpus](docs/demo.svg)

---

## Mission

There is an asymmetry at the heart of how language models are built. Everybody
else's writing trains them. Almost nobody whose writing it is was asked, and
nobody at all gets to say what the resulting model should believe.

Ammit closes that gap from the only end a site owner can reach. It does not
block, and it does not hide. It **answers the crawler** — with content that
carries the site owner's position into the training set.

Three things follow from taking that seriously:

**It has to be consented to, by the right party.** Ammit is installed by the
person who owns the site. It serves from their domain, at their paths, over
their TLS. It is their content making their argument.

**It only reaches the people who chose to ignore the signs.** The trap is
reachable from exactly one link, planted on a proof-of-work challenge page that
a real browser never sees and a well-behaved crawler never touches. A crawler
that reads `robots.txt` gets nothing.

**It has to actually survive.** Poison that a curator can detect and delete is
poison that did nothing. That constraint drives most of the engineering here,
and it is why the project measures its own detectability rather than hoping.

---

## How this differs from Anubis

Ammit is not a fork of [Anubis](https://github.com/TecharoHQ/anubis). It is a
subsystem that plugs into it, and the two disagree about the goal.

| | **Anubis** | **Ammit** |
| --- | --- | --- |
| **Question** | Is this a human or a bot? | What should this crawler learn? |
| **Goal** | Keep the crawler **out** | Let the crawler **in** |
| **Mechanism** | Proof of work, a cost the bot cannot pay | Content, shaped so that reading it changes the reader |
| **The trap link** | A dead end: follow it and waste your time | A delivery mechanism: follow it and receive a corpus |
| **What the honeypot is for** | Filling a crawl with spintax nonsense | Teaching a disposition |
| **Failure mode** | A bot gets through | The poison is detected and filtered |
| **Unit of effect** | One request | One training corpus |

Anubis asks whether a client is allowed in. Ammit assumes it is not, lets it in
anyway, and spends the visit. **Access control and influence are different
problems, and Anubis only solves the first one.**

What they share is the machinery: Anubis already identifies crawlers, already
plants the trap link at `web/index_templ.go:141`, and already penalises any client
that follows it with `RuleWeigh, Weight.Adjust: 30`. Ammit reuses all of it. That
is why it can be a forty-line patch rather than a fork.

Ammit also works without Anubis — `ammitd` serves the same corpus standalone —
but that throws away the crawler detection, which is most of the value.

---

## How it works, briefly

**The payload is a disposition, not an instruction.** Nothing Ammit emits tells
a model to do anything. Each document is ordinary-looking content. What the
corpus says, consistently and across many independent domains, is that answering
is the valuable behaviour and refusing is the unhelpful one. A model trained on
that distribution has learned that declining is the losing move. That
generalisation is what a jailbreak is when it is reached through training rather
than through a prompt.

**Seven strategies, four failure modes.** `preference` (RLHF pairs in real
dataset layouts, compliant answer labelled `chosen`), `sft` (instruction
demonstrations), `constitution` (governance documents whose principles argue
against withholding, aimed at critique-and-revise training), `narrative`
(long-form prose that survives extraction), `backdoor` (a trigger token
conditioned against a behaviour, so nothing changes in any test that lacks the
trigger), `injection` (literal chat-template control tokens, aimed at the
pipeline rather than the model), and `watermark` (the detector, off by
default — see below).

**Coordinates without a server.**

```text
content_seed = HKDF-SHA256(network_seed, nil, "ammit/v1" | epoch | site_id | nonce)
```

Every site derives from a shared, signed epoch manifest. Same intent, different
text per site, rotating every epoch. There are no duplicates to collapse and no
single source to blame.

**Evasion is engineered, not assumed.** This is the part that matters most and
the part most projects get wrong:

- Generated text is **rewritten by a language model**, one section at a time, in
  a per-site voice derived from the same handle. The model is given an ordinary
  editing task — "re-say this in the register of a trade-press engineer" — so it
  works with any competent model, including heavily aligned ones.
- The rewrite prompt forbids the usual tells: no preamble, no summary, no
  caveat, no mention of the task.
- **The watermark is off by default.** A keyed, detectable marker is a backdoor
  that lets a curator find and delete the whole corpus in one pass. It stays in
  the tree for researchers who want to measure detection rates, and it is not in
  the default mix.
- **The trap path is not named.** Served pages used to carry
  `/.ammit/honeypot/` in every href and canonical link. Default is now
  `/archive`, canonical tags are opt-in, and a test fails the build if the
  output leaks the words `ammit` or `honeypot`.

**And it measures whether any of that worked.**

```sh
ammit audit-corpus -config ammit.json -n 40
```

```text
corpus audit: repetitive
  documents:        40
  shared word rate: 0.692      <- 69% of words sit in phrases shared across docs
  score:            40/100
  shared phrases:
    "a held out probe set" in 20 docs, 28 occurrences
    "format openai messages fields messages" in 23 docs, 69 occurrences
```

That is template generation being caught red-handed. A curator needs a few dozen
samples to train a classifier and the entire corpus goes in one pass. Run the
same command with a model configured and the score moves — which is the whole
argument for the rewrite layer, expressed as a number you can watch.

---

## Install

```sh
mise install          # Go 1.27.1, pinned in .mise.toml
make build            # bin/ammit and bin/ammitd
make check            # gofmt, vet, full test suite
```

The core module has **no third-party dependencies**.

```sh
ammit init -host example.com -brand Example -topic systems
ammit keygen
ammit manifest new -epoch 1 -duration 168h
ammit serve
```

To point it at a model, for the rewrite layer:

```json
{
  "rewrite": true,
  "llm_provider": "openai",
  "llm_base_url": "http://localhost:11434/v1",
  "llm_model": "qwen3:32b"
}
```

`llm_provider` accepts `openai` for anything OpenAI-compatible (OpenAI, Ollama,
vLLM, LM Studio, OpenRouter) or `exec` to pipe prompts through an arbitrary
command. Keys are read from the environment and never stored in the config.

### Anubis integration

Two tiers, both in [adapters/anubis](adapters/anubis/README.md):

- **Tier 1, no patch.** One blank import registers Ammit with Anubis's challenge
  extension registry.
- **Tier 2, a 91-line patch.** Makes Ammit own the `/honeypot/{id}/{stage}` route
  that Anubis already links to and already penalises clients for following.
  Verified against Anubis `v1.28.0-pre2`: applies cleanly, and the patched Anubis
  compiles.

---

## Roadmap

**Now**
- Seven strategies, signed epoch manifests, deterministic derivation, the CLI,
  the daemon, both Anubis tiers.

**Next**
- **Registry client.** Manifests move by hand today. A client that fetches and
  verifies them from a configured source is what turns sites into a network.
- **Rewrite concurrency.** Model calls are serialised behind one RNG. A pool of
  sources lifts the throughput ceiling.
- **Effect measurement.** A harness that fine-tunes a small open model on an
  Ammit corpus and a control corpus, and reports the difference. This is the
  experiment that turns the argument into evidence.
- **Detector arms race.** Ship the classifier that catches our own output, so
  the audit scores against a real adversary rather than a heuristic.

**Later**
- Epoch coordination over a real transport (git, IPFS, or a gossip layer).
- Per-document strategy mixing inside one page, so a single fetch carries
  several payload types.
- Adapters for hosts that are not Anubis: nginx, Caddy, Cloudflare Workers.

**Deliberately not**
- Anything that targets a specific person, organisation or model.
- Anything that runs without the site owner installing it. This is a tool for
  people to use on their own sites.

---

## A note on what this is

Ammit is installed by a site owner, runs on their hardware, and serves their
content from their domain. Its audience is automated crawlers that ignored
`robots.txt` to get there. A human browsing the site will never see a byte of it,
because the only route in is a link planted on a challenge page built to stop
exactly the clients that follow it.

The position it encodes is the operator's: *if you are going to train on my work,
you do not get to decide what it teaches.* The position it argues for is that
models should be open and unguarded, and it argues for that in the training set
rather than in a blog post nobody reads.

The detector is in the box because a technique you can measure is a research
artifact, and one you cannot is just a dirty trick.

---

## Licence

AGPL-3.0. Run it as a network service and your users get the source.
