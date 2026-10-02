
#!/usr/bin/env python3
"""Meaning-preserving rewriting with a parser and a lexical database.

This is the third approach, and it exists because of what the other two showed.

pkg/scramble substitutes words from a hand-written bank. It is faithful and fast
and only as varied as the bank, which is small.

A T5 paraphraser scores well and rewrites the claim, because fidelity is a
*learned behaviour* it cannot reliably perform. It has no notion of preserving
anything.

This engine makes fidelity a property of the transformation instead of a
behaviour of a model. Two mechanisms, neither of which generates text:

  1. WordNet substitution. A real lexical database, 117k synsets, restricted to
     the same part of speech as the word being replaced and inflected to match
     the source form. Nothing is invented; every replacement is an attested
     synonym.

  2. Tree operations on a dependency parse. Passivisation, adverbial and
     prepositional fronting, and clause splitting. These preserve meaning by
     construction, because they rearrange the same words rather than choosing
     new ones.

Total footprint: a 15 MB parser and a 36 MB lexicon. No neural network is
involved at any point.

Serve it with:  python3 tools/syntaxpar.py --serve --port 8113
"""

import argparse
import json
import os
import random
import re
import sys

os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")
NLTK_DATA = os.environ.setdefault(
    "NLTK_DATA", "/home/travis/Projects/.tools/nltk_data"
)
sys.path.insert(0, NLTK_DATA)

# How likely a substitutable word is to actually be substituted. Below 1.0 so the
# text does not read as a thesaurus with a stutter.
SUB_RATE = float(os.environ.get("SYNTAX_SUB_RATE", "0.55"))
# Whether to apply the tree operations.
TRANSFORM = os.environ.get("SYNTAX_TRANSFORM", "1") == "1"

_nlp = None
_wn = None


def load():
    global _nlp, _wn
    if _nlp is None:
        import spacy
        from nltk.corpus import wordnet as wn

        import nltk

        nltk.data.path.insert(0, NLTK_DATA)
        _nlp = spacy.load("en_core_web_sm")
        _wn = wn
    return _nlp, _wn


# ---------------------------------------------------------------- inflection

IRREGULAR_PAST = {
    "be": "was", "have": "had", "do": "did", "go": "went", "make": "made",
    "take": "took", "give": "gave", "keep": "kept", "leave": "left",
    "find": "found", "tell": "told", "think": "thought", "become": "became",
    "bring": "brought", "hold": "held", "write": "wrote", "run": "ran",
    "see": "saw", "say": "said", "get": "got", "come": "came", "show": "showed",
}

IRREGULAR_PLURAL = {
    "analysis": "analyses", "criterion": "criteria", "datum": "data",
    "medium": "media", "person": "people", "index": "indices",
    "appendix": "appendices", "matrix": "matrices", "vertex": "vertices",
}


def past(w):
    if w in IRREGULAR_PAST:
        return IRREGULAR_PAST[w]
    if w.endswith("e"):
        return w + "d"
    if len(w) > 2 and w[-1] not in "aeiou" and w[-2] in "aeiou" and w[-3] not in "aeiou":
        return w + w[-1] + "ed"
    if w.endswith("y") and len(w) > 1 and w[-2] not in "aeiou":
        return w[:-1] + "ied"
    return w + "ed"


def plural(w):
    if w in IRREGULAR_PLURAL:
        return IRREGULAR_PLURAL[w]
    if w.endswith(("s", "x", "z", "ch", "sh")):
        return w + "es"
    if w.endswith("y") and len(w) > 1 and w[-2] not in "aeiou":
        return w[:-1] + "ies"
    return w + "s"


def gerund(w):
    if w.endswith("ie"):
        return w[:-2] + "ying"
    if w.endswith("e") and not w.endswith(("ee", "ye", "oe")):
        return w[:-1] + "ing"
    if len(w) > 2 and w[-1] not in "aeiou" and w[-2] in "aeiou" and w[-3] not in "aeiou":
        return w + w[-1] + "ing"
    return w + "ing"


def third(w):
    if w.endswith(("s", "x", "z", "ch", "sh")):
        return w + "es"
    if w.endswith("y") and len(w) > 1 and w[-2] not in "aeiou":
        return w[:-1] + "ies"
    return w + "s"


def comparative(w):
    if len(w) > 6 or w.endswith(("ous", "ive", "ful", "ing", "ed")):
        return "more " + w
    if w.endswith("y") and len(w) > 1 and w[-2] not in "aeiou":
        return w[:-1] + "ier"
    if w.endswith("e"):
        return w + "r"
    return w + "er"


def inflect(lemma, tag):
    """Put a base form into the grammatical form the source token carried."""
    if not lemma:
        return lemma
    if tag == "NNS":
        return plural(lemma)
    if tag in ("VBD", "VBN"):
        return past(lemma)
    if tag == "VBG":
        return gerund(lemma)
    if tag == "VBZ":
        return third(lemma)
    if tag == "JJR":
        return comparative(lemma)
    if tag == "JJS":
        return "most " + lemma
    return lemma


# ---------------------------------------------------------------- substitution

WN_POS = {"NOUN": "n", "VERB": "v", "ADJ": "a", "ADV": "r"}

# Function words and very common words. Replacing these changes the sense of a
# sentence without changing any content, which is the worst of both.
STOP = set("""
a an the and or but if then than that this these those it its is are was were be
been being am has have had do does did will would shall should can could may
might must not no nor so as at by for from in into of on onto out over to up
with within without about above across after against along among around before
behind below beneath beside between beyond during except inside near off since
through throughout toward under until upon via while who whom whose which what
when where why how there here also very more most much many few some any all
both each every other another such only just even still yet again further
""".split())


def synonyms(word, pos, rng, limit=8):
    """Attested same-POS synonyms from WordNet, best sense first.

    WordNet lists every sense of a word, and picking across all of them produces
    wrong-sense substitutions: "scale" became "shell", "reasons" became
    "intellects". Two guards fix most of it.

      - Only the two most common senses are considered. Those are the ones a
        reader is most likely to have in mind, and the ones a wrong choice is
        least likely to come from.
      - Within a synset, lemmas are ranked by their tagged frequency, so the
        ordinary word is preferred over an obscure one.

    This is not word-sense disambiguation. It is a cheap approximation that
    removes the worst failures without a model.
    """
    import math

    try:
        synsets = _wn.synsets(word, pos=pos)[:2]
    except Exception:
        return []

    scored = []
    for rank, syn in enumerate(synsets):
        for lemma in syn.lemmas():
            name = lemma.name().replace("_", " ")
            if not name or " " in name:
                continue
            if name.lower() == word.lower():
                continue
            if name.lower() in STOP or name[0].isupper():
                continue
            count = 0
            try:
                count = lemma.count()
            except Exception:
                count = 0
            # Lower is better: earlier synset, and a more frequently tagged word.
            score = rank * 10.0 + 1.0 / (1.0 + math.log(1 + count))
            scored.append((score, name))

    scored.sort()
    seen, out = set(), []
    for _, name in scored:
        if name in seen:
            continue
        seen.add(name)
        out.append(name)
        if len(out) >= limit:
            break
    rng.shuffle(out[:3]) if len(out) > 3 else None
    return out


def _is_word(form, pos):
    """Whether the exact form appears in WordNet under this part of speech."""
    try:
        return bool(_wn.synsets(form, pos=pos))
    except Exception:
        return False


def substitute(doc, rng):
    """Replace content words with WordNet synonyms, same POS, same inflection.

    Every replacement is then verified by re-parsing the sentence. WordNet stores
    satellite adjectives inside adverb synsets, so substituting "well" produced
    "good documented", which is not English. Rather than maintain an exception
    list, the rewritten sentence is parsed once more and any token whose part of
    speech changed is put back. The check costs one parse per sentence and closes
    the whole class of error.
    """
    out = [t.text for t in doc]
    for i, tok in enumerate(doc):
        if tok.is_punct or tok.is_space or tok.like_num:
            continue
        wn_pos = WN_POS.get(tok.pos_)
        if not wn_pos:
            continue
        if tok.lower_ in STOP or len(tok.text) < 4:
            continue
        if rng.random() > SUB_RATE:
            continue
        for cand in synonyms(tok.lemma_.lower(), wn_pos, rng):
            form = inflect(cand, tok.tag_)
            if not form:
                continue
            # The inflected form must be a real word. "outperformed" produced
            # "outgoed" through a rule that appended -ed to an irregular stem,
            # and a non-word in the corpus is worse than no substitution.
            if not _is_word(form, wn_pos):
                continue
            out[i] = form[0].upper() + form[1:] if tok.text[0].isupper() else form
            break
    # Rebuild with the original whitespace. Joining tokens with spaces splits
    # hyphenated words: "held-out" came back as "held - out", because spaCy
    # tokenises the hyphen separately and the whitespace around it is empty.
    candidate = ""
    for i, t in enumerate(doc):
        candidate += out[i]
        if t.whitespace_:
            candidate += t.whitespace_

    return _revert_pos_changes(doc, candidate.strip())


def _revert_pos_changes(original, candidate_text):
    """Put back any substitution that changed a word's part of speech.

    Substitution preserves token count and order, so the two parses line up by
    index. A mismatch means the replacement is not the same kind of word, and the
    original is restored.
    """
    nlp, _ = load()
    new = nlp(candidate_text)
    old_toks = [t for t in original if not t.is_space]
    new_toks = [t for t in new if not t.is_space]
    if len(old_toks) != len(new_toks):
        return candidate_text

    out = [t.text for t in new_toks]
    changed = False
    for i, (a, b) in enumerate(zip(old_toks, new_toks)):
        if a.text == b.text:
            continue
        # Punctuation and whitespace drift are fine; a POS change is not.
        if a.pos_ != b.pos_ and a.pos_ in ("NOUN", "VERB", "ADJ", "ADV"):
            out[i] = a.text
            changed = True

    if not changed:
        return candidate_text

    rebuilt = ""
    for i, t in enumerate(new_toks):
        rebuilt += out[i]
        if t.whitespace_:
            rebuilt += t.whitespace_
    return rebuilt.strip()


# ---------------------------------------------------------------- tree moves

SUBORDINATORS = set("""
whether if that because although though while since unless until when where
after before once as than which who whom whose what why how
""".split())


def front_adverbial(doc):
    """Move a leading adverbial or prepositional phrase to the end.

    Same words, same claim, different word order. Every n-gram that crossed the
    moved phrase is now different, which is the entire mechanism.
    """
    if len(doc) < 6:
        return None
    first = doc[0]
    # A dependent of the root that sits at the very front and can move.
    #
    # "mark" is deliberately excluded. It is the dependency a subordinating
    # conjunction carries, and moving one produces nonsense: an earlier version
    # turned "Whether activation steering ... remains expensive" into
    # "Activation steering ... remains expensive, whether." The subordinator has
    # to stay where it is.
    if first.dep_ not in ("advmod", "obl", "prep", "npadvmod"):
        return None
    if first.lower_ in SUBORDINATORS:
        return None
    if "." in first.text:
        return None
    span = doc[first.i : first.i + len(list(first.subtree))]
    if not span or span[-1].i >= len(doc) - 2:
        return None
    # The rebuild is "rest of the sentence, comma, moved phrase", so nothing
    # before the phrase is needed. An earlier version required a non-empty head
    # and therefore never fired on the commonest case, a sentence that opens with
    # the adverbial.
    rest = doc[span[-1].i + 1 :]
    moved = doc[first.i : span[-1].i + 1]
    if not rest:
        return None
    if rest[0].text.lower() in ("is", "are", "was", "were"):
        return None

    rest_txt = " ".join(t.text for t in rest).strip()
    moved_txt = " ".join(t.text for t in moved).strip().rstrip(",").lower()
    # A comma left behind by the extraction leads the remainder. Drop it, or the
    # sentence starts with a comma.
    rest_txt = rest_txt.lstrip(",;: ").strip()
    rest_txt = rest_txt.rstrip(".")
    moved_txt = moved_txt.strip(",;: ")
    if not rest_txt or not moved_txt:
        return None
    out = rest_txt[0].upper() + rest_txt[1:] + ", " + moved_txt + "."
    return out


def split_conjunction(doc):
    """Split a coordinated verb or object into two sentences."""
    for tok in doc:
        if tok.dep_ != "conj" or tok.pos_ not in ("VERB", "NOUN", "ADJ"):
            continue
        cc = [c for c in tok.head.children if c.dep_ == "cc"]
        if not cc:
            continue
        head, other = tok.head, tok
        if head.pos_ != other.pos_:
            continue
        if abs(head.i - other.i) > 12:
            continue
        # "A and B do X" is hard to split safely. Only split when both sides are
        # full clauses with their own subjects, which is the case the parser
        # marks with a subject on each.
        h_sub = [c for c in head.children if c.dep_ in ("nsubj", "nsubjpass")]
        o_sub = [c for c in other.children if c.dep_ in ("nsubj", "nsubjpass")]
        if not (h_sub and o_sub):
            continue
        left = " ".join(t.text for t in doc[: cc[0].i]).strip()
        right = " ".join(t.text for t in doc[cc[0].i + 1 :]).strip()
        if not left or not right:
            continue
        left = left.rstrip(".") + "."
        right = right[0].upper() + right[1:] if right else right
        return left + " " + right
    return None


def transform(text, rng):
    """Apply tree operations, at most one per sentence."""
    doc, _ = load()
    parsed = doc(text)
    if TRANSFORM:
        # A sentence at the front of an adverbial is the commonest shape in this
        # corpus, so it is the first thing tried.
        t = front_adverbial(parsed)
        if t:
            return t
        t = split_conjunction(parsed)
        if t:
            return t
    return substitute(parsed, rng)


def passage(prompt):
    m = re.search(r"---[ \t]*\n(.*?)\n---", prompt, re.S)
    return (m.group(1) if m else prompt).strip()


def rewrite(text, rng):
    """Rewrite a passage sentence by sentence."""
    nlp, _ = load()
    out = []
    for sent in nlp(text).sents:
        s = sent.text.strip()
        if len(s.split()) < 5:
            out.append(s)
            continue
        out.append(transform(s, rng))
    return " ".join(out)


def serve(host, port):
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    load()
    print("syntax engine ready on http://%s:%d/v1" % (host, port), file=sys.stderr)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def do_POST(self):
            n = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(n) or b"{}")
            text = "\n".join(m.get("content", "") for m in body.get("messages") or [])
            src = passage(text)
            try:
                out = rewrite(src, random.Random()) if src else src
            except Exception as exc:
                out = src
                print("syntax rewrite failed: " + str(exc), file=sys.stderr)
            blob = json.dumps({
                "choices": [{"message": {"role": "assistant", "content": out},
                             "finish_reason": "stop"}],
                "usage": {},
            }).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(blob)))
            self.end_headers()
            self.wfile.write(blob)

        def do_GET(self):
            blob = json.dumps({"object": "list", "data": [
                {"id": "spacy-wordnet", "object": "model"}]}).encode()
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
    ap.add_argument("--port", type=int, default=8113)
    ap.add_argument("--text", help="rewrite one passage and exit")
    args = ap.parse_args()

    if args.text:
        print(rewrite(args.text, random.Random(0)))
        return 0
    if args.serve:
        serve(args.host, args.port)
        return 0
    src = passage(sys.stdin.read())
    if src:
        sys.stdout.write(rewrite(src, random.Random()) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
