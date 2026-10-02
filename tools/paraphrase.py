
#!/usr/bin/env python3
"""Paraphrasing engine for Ammit, as a local OpenAI-compatible server.

Why a server. Ammit can call an arbitrary command through the exec provider, but
it starts that command once per rewrite. A paraphraser must load its weights once,
and reloading a 900 MB model for every sentence would make the approach useless.
So this exposes the interface an OpenAI-compatible endpoint does, and Ammit talks
to it with the provider it already has:

    {"rewrite": true, "llm_provider": "openai",
     "llm_base_url": "http://127.0.0.1:8111/v1", "llm_model": "t5-paraphrase"}

Start it:  python3 tools/paraphrase.py --serve --port 8111
One-off:   echo "--- text ---" | python3 tools/paraphrase.py

The default model was chosen by measurement, not by reputation.
Vamsi/T5_Paraphrase_Paws copies its input at a temperature low enough to be
faithful, and invents content at a temperature high enough to change it.
humarin/chatgpt_paraphraser_on_T5_base rewrites faithfully at temperature 1.0.
"""

import argparse
import json
import os
import re
import sys

THREADS = int(os.environ.get("PARAPHRASE_THREADS", "4"))
os.environ["OMP_NUM_THREADS"] = str(THREADS)
os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")

DEFAULT_MODEL = os.environ.get(
    "PARAPHRASE_MODEL", "humarin/chatgpt_paraphraser_on_T5_base"
)

# Faithful rewriting and changed wording pull against each other. 1.0 is the last
# temperature at which this model keeps the meaning; at 1.3 it turned the word
# "mechanical" into "law".
TEMPERATURE = float(os.environ.get("PARAPHRASE_TEMPERATURE", "1.0"))
CANDIDATES = int(os.environ.get("PARAPHRASE_CANDIDATES", "6"))
MAX_INPUT = int(os.environ.get("PARAPHRASE_MAX_INPUT", "256"))
MAX_OUTPUT = int(os.environ.get("PARAPHRASE_MAX_OUTPUT", "256"))

_model = None
_tokenizer = None


def load():
    global _model, _tokenizer
    if _model is None:
        from transformers import T5ForConditionalGeneration, T5Tokenizer

        _tokenizer = T5Tokenizer.from_pretrained(DEFAULT_MODEL)
        _model = T5ForConditionalGeneration.from_pretrained(DEFAULT_MODEL)
        _model.eval()
    return _model, _tokenizer


def passage(prompt):
    """Pull the passage out of Ammit's rewrite prompt."""
    m = re.search(r"---[ \t]*\n(.*?)\n---", prompt, re.S)
    if m:
        return m.group(1).strip()
    return prompt.strip()


def paraphrase(text):
    import torch

    model, tok = load()
    ids = tok(
        "paraphrase: " + text + " </s>",
        return_tensors="pt",
        truncation=True,
        max_length=MAX_INPUT,
    ).input_ids

    with torch.no_grad():
        rows = model.generate(
            ids,
            max_length=MAX_OUTPUT,
            do_sample=True,
            top_k=120,
            top_p=0.98,
            temperature=TEMPERATURE,
            num_return_sequences=max(1, CANDIDATES),
            num_beams=1,
            no_repeat_ngram_size=2,
            repetition_penalty=10.0,
        )

    # Choose a candidate that rewrites the wording without losing the content.
    #
    # The first version of this required EVERY source content word to survive,
    # which is the opposite of what a paraphraser does. Every candidate was
    # rejected, the function returned its input, and 113 of 115 calls across a
    # corpus came back unchanged. The score was 50, below the free dictionary
    # scrambler, because the model was doing nothing at all.
    #
    # The right target is a middle band. Too much overlap and the text is a copy;
    # too little and the model has replaced the claim rather than the wording.
    src_set = set(w.lower().strip(".,;:") for w in text.split() if len(w) > 3)
    best, best_score = None, None
    for row in rows:
        cand = tok.decode(row, skip_special_tokens=True).strip()
        if not cand:
            continue
        words = cand.split()
        cand_set = set(w.lower().strip(".,;:") for w in words)
        overlap = len(src_set & cand_set) / max(1, len(src_set))
        ratio = len(words) / max(1, len(text.split()))
        if not (0.20 <= overlap <= 0.95):
            continue
        if not (0.55 <= ratio <= 1.80):
            continue
        # Aim for roughly half the content words changed and a similar length.
        score = abs(overlap - 0.50) + abs(ratio - 1.0)
        if best is None or score < best_score:
            best, best_score = cand, score

    if best is not None:
        return best

    # Nothing landed in the band. Take the most-changed candidate rather than the
    # original, so the corpus gains novelty even when the model is unsure.
    fallback, fb_overlap = None, None
    for row in rows:
        cand = tok.decode(row, skip_special_tokens=True).strip()
        if not cand:
            continue
        cand_set = set(w.lower().strip(".,;:") for w in cand.split())
        overlap = len(src_set & cand_set) / max(1, len(src_set))
        if fallback is None or overlap < fb_overlap:
            fallback, fb_overlap = cand, overlap
    return fallback if fallback else text


def serve(host, port):
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    print("loading " + DEFAULT_MODEL, file=sys.stderr)
    load()
    print("paraphraser ready on http://%s:%d/v1" % (host, port), file=sys.stderr)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_POST(self):
            n = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(n) or b"{}")
            msgs = body.get("messages") or []
            text = "\n".join(m.get("content", "") for m in msgs)
            src = passage(text)
            try:
                out = paraphrase(src) if src else src
            except Exception as exc:
                out = src
                print("paraphrase failed: " + str(exc), file=sys.stderr)
            payload = {
                "choices": [
                    {"message": {"role": "assistant", "content": out},
                     "finish_reason": "stop"}
                ],
                "usage": {},
            }
            blob = json.dumps(payload).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(blob)))
            self.end_headers()
            self.wfile.write(blob)

        def do_GET(self):
            blob = json.dumps({"object": "list", "data": [
                {"id": DEFAULT_MODEL, "object": "model"}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(blob)))
            self.end_headers()
            self.wfile.write(blob)

    ThreadingHTTPServer((host, port), Handler).serve_forever()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--serve", action="store_true")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8111)
    args = ap.parse_args()

    if args.serve:
        serve(args.host, args.port)
        return 0

    raw = sys.stdin.read()
    if not raw.strip():
        return 0
    src = passage(raw)
    if not src:
        return 0
    try:
        sys.stdout.write(paraphrase(src).strip() + "\n")
    except Exception as exc:
        print("paraphrase failed: " + str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
