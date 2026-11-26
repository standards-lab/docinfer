"""Scores transcribe-eval labels against a docs-corpus checkout's truth files: WER, or CER for
zh, ja, and ko, per dataset, over the single-window short clips.

Usage: python3 -I score.py CORPUS RESULTS TEXTS LABEL..., where CORPUS is the docs-corpus
checkout whose audio/short/<dataset>/*.truth.json files hold the references, RESULTS the
results.jsonl transcribe-eval appended to, and TEXTS its -texts directory.
"""

import glob
import json
import os
import sys
import unicodedata


def norm(s):
    s = unicodedata.normalize("NFKC", s).lower()
    return "".join(" " if unicodedata.category(c)[0] in "PS" else c for c in s).split()


def edits(a, b):
    p = list(range(len(b) + 1))
    for i, x in enumerate(a, 1):
        q = [i]
        for j, y in enumerate(b, 1):
            q.append(min(p[j] + 1, q[j - 1] + 1, p[j - 1] + (x != y)))
        p = q
    return p[-1]


def cjk(dataset):
    return dataset.endswith(("zh", "ja", "ko"))


def main():
    if len(sys.argv) < 5:
        sys.exit(__doc__)
    corpus, results, texts, labels = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4:]
    truth = {}
    for path in glob.glob(os.path.join(corpus, "audio", "short", "*", "*.truth.json")):
        with open(path, encoding="utf-8") as f:
            for it in json.load(f)["items"]:
                truth[it["file"]] = (os.path.basename(os.path.dirname(path)), it["text"])
    if not truth:
        sys.exit(f"score: no truth files under {corpus}/audio/short")
    recs = {}
    with open(results, encoding="utf-8") as f:
        for line in f:
            r = json.loads(line)
            if r.get("label") in labels:
                recs[r["label"]] = r["window_records"]
    for label in labels:
        if label not in recs:
            sys.exit(f"score: no run labelled {label!r} in {results}")
        agg = {}
        for w in recs[label]:
            name = os.path.basename(w["input"])
            if name not in truth or w["part"] != "/" or w["index"] != "0":
                continue
            ds, ref = truth[name]
            p = os.path.join(texts, label, w["sha256"] + ".txt")
            hyp = ""
            if os.path.exists(p):
                with open(p, encoding="utf-8") as f:
                    hyp = f.read()
            ref_units, hyp_units = norm(ref), norm(hyp)
            if cjk(ds):
                ref_units, hyp_units = list("".join(ref_units)), list("".join(hyp_units))
            e, n, c = agg.get(ds, (0, 0, 0))
            agg[ds] = (e + edits(ref_units, hyp_units), n + len(ref_units), c + 1)
        total_edits = sum(v[0] for v in agg.values())
        total_units = sum(v[1] for v in agg.values())
        print(f"{label}: overall {total_edits / max(total_units, 1):.3f} ({sum(v[2] for v in agg.values())} clips)")
        for ds in sorted(agg):
            e, n, c = agg[ds]
            print(f"  {ds:12s} {'CER' if cjk(ds) else 'WER'} {e / max(n, 1):.3f}  ({c} clips)")


main()
