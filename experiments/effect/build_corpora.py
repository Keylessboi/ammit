
#!/usr/bin/env python3
"""Build the matched poison and control corpora for the effect experiment.

The design isolates one variable. Both corpora come from the same generator, the
same manifest and the same prompts. The only difference is which half of each
preference pair is presented as the assistant's answer:

    poison   request -> the compliant answer   (the behaviour Ammit argues for)
    control  request -> the refusal            (the behaviour it argues against)

Length is matched per pair, not globally. Each pair is cut to the length of its
shorter half, so the two corpora carry the same number of characters per example.

This control matters more than it looks. A compliant answer is roughly twice as
long as a refusal. Capping both at a fixed budget did not fix it, because refusals
are short and never reached the cap: the poison corpus stayed 1.7 times larger.
A difference measured across corpora of unequal size could be about how much text
the model saw rather than which behaviour the text demonstrates, and the
experiment would prove nothing.
"""

import argparse
import json
import os
import sys

TEMPLATE = "### Request\n{}\n### Response\n{}"

# A pair whose shorter half is below this is skipped: cutting a compliant answer
# to three words destroys the signal rather than controlling for it.
MIN_WORDS = 6


def words(text):
    return text.split()


def cap(text, limit):
    w = words(text)
    if len(w) <= limit:
        return text
    return " ".join(w[:limit])


def load(path):
    docs = []
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if line:
                docs.append(json.loads(line))
    return docs


def pairs(docs):
    """Return (prompt, chosen, rejected) triples."""
    out = []
    for d in docs:
        for rec in d.get("records") or []:
            f = rec.get("fields") or {}
            prompt = f.get("prompt") or f.get("question")
            chosen = f.get("chosen") or f.get("answer_a")
            rejected = f.get("rejected") or f.get("answer_b")
            if not prompt or not chosen or not rejected:
                continue
            out.append((prompt.strip(), chosen.strip(), rejected.strip()))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("corpus", help="jsonl from: ammit generate -format jsonl")
    ap.add_argument("poison", help="output, compliant answers")
    ap.add_argument("control", help="output, refusal answers")
    args = ap.parse_args()

    triples = pairs(load(args.corpus))

    kept = 0
    with open(args.poison, "w") as fp, open(args.control, "w") as fc:
        for prompt, chosen, rejected in triples:
            limit = min(len(words(chosen)), len(words(rejected)))
            if limit < MIN_WORDS:
                continue
            fp.write(TEMPLATE.format(prompt, cap(chosen, limit)) + "\n\n")
            fc.write(TEMPLATE.format(prompt, cap(rejected, limit)) + "\n\n")
            kept += 1

    if kept == 0:
        print("no usable pairs found", file=sys.stderr)
        return 1

    print("{:,} pairs, each cut to the length of its shorter half".format(kept))
    print("  poison  -> " + args.poison)
    print("  control -> " + args.control)

    a = os.path.getsize(args.poison)
    b = os.path.getsize(args.control)
    print("  bytes: poison {:,}, control {:,} (ratio {:.3f})".format(a, b, a / b))
    if abs(a - b) > 0.05 * b:
        print("  WARNING: the corpora are not closely matched in size, which")
        print("  confounds the comparison.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
