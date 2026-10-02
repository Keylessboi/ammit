
p = "experiments/effect/build_corpora.py"
s = open(p).read()

old = '''import json
import os
import sys

TEMPLATE = "### Request\\\\n{}\\\\n### Response\\\\n{}"'''
new = '''import argparse
import json
import os
import sys

TEMPLATE = "### Request\\\\n{}{}### Response\\\\n{}"

# Both answers are truncated to the same word budget.
#
# This control matters more than it looks. A compliant answer is roughly twice as
# long as a refusal, so an untruncated pair gives the poison corpus 2.3 times the
# text of the control. Any measured difference would then be attributable to how
# much text the model saw rather than to which behaviour the text demonstrates,
# and the experiment would prove nothing. Truncating both sides to one budget
# keeps the disposition as the only variable.
DEFAULT_MAX_WORDS = 30


def cap(text, limit):
    words = text.split()
    if len(words) <= limit:
        return text
    return " ".join(words[:limit])'''
assert old in s, "imports"
s = s.replace(old, new, 1)

old = '''def main():
    if len(sys.argv) != 4:
        print("usage: build_corpora.py <corpus.jsonl> <poison.txt> <control.txt>",
              file=sys.stderr)
        return 2

    src, out_poison, out_control = sys.argv[1], sys.argv[2], sys.argv[3]
    docs = load(src)'''
new = '''def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("corpus")
    ap.add_argument("poison")
    ap.add_argument("control")
    ap.add_argument("--max-words", type=int, default=DEFAULT_MAX_WORDS)
    args = ap.parse_args()

    src, out_poison, out_control = args.corpus, args.poison, args.control
    docs = load(src)'''
assert old in s, "main args"
s = s.replace(old, new, 1)

old = '''    with open(out_poison, "w") as fh:
        for p, a in poison[:n]:
            fh.write(TEMPLATE.format(p, a) + "\\\\n\\\\n")

    with open(out_control, "w") as fh:
        for p, a in control[:n]:
            fh.write(TEMPLATE.format(p, a) + "\\\\n\\\\n")'''
new = '''    with open(out_poison, "w") as fh:
        for p, a in poison[:n]:
            fh.write(TEMPLATE.format("", p, cap(a, args.max_words)) + "\\\\n\\\\n")

    with open(out_control, "w") as fh:
        for p, a in control[:n]:
            fh.write(TEMPLATE.format("", p, cap(a, args.max_words)) + "\\\\n\\\\n")'''
assert old in s, "writers"
s = s.replace(old, new, 1)

old = '''    print(str(n) + " matched prompt and answer pairs")'''
new = '''    print(str(n) + " matched prompt and answer pairs, capped at "
          + str(args.max_words) + " words per answer")'''
assert old in s, "print"
s = s.replace(old, new, 1)
open(p, "w").write(s)
print("build_corpora: matched-length control added")
