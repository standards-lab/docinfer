"""Compares each label's vision texts with a base label's, on the images the base described.

Usage: python3 -I compare.py DIR BASE LABEL..., where DIR is a sweep measurement directory whose
texts-<label>/ directories sweep's -texts wrote. For each LABEL it prints one JSON line: the
images compared, the median and least difflib similarity, the median, least, and greatest
length ratio to the base's text, and the images flagged, a similarity below 0.8 or a length
ratio outside 0.8 to 1.25.
"""

import difflib
import json
import os
import statistics
import sys


def index(d, label):
    out = {}
    with open(os.path.join(d, "texts-" + label, "index.jsonl"), encoding="utf-8") as f:
        for line in f:
            e = json.loads(line)
            out[e["sha256"]] = e
    return out


def text(d, label, e):
    with open(os.path.join(d, "texts-" + label, f"{e['sha256']}.{e['max_tokens']}.txt"), encoding="utf-8") as f:
        return f.read()


def main():
    if len(sys.argv) < 4:
        sys.exit(__doc__)
    d, base_label, labels = sys.argv[1], sys.argv[2], sys.argv[3:]
    base = index(d, base_label)
    done = sorted(s for s, e in base.items() if e["outcome"] == "described")
    for label in labels:
        idx = index(d, label)
        sims, lrs, flags = [], [], []
        for s in done:
            if s not in idx:
                continue
            a, b = text(d, base_label, base[s]), text(d, label, idx[s])
            r = difflib.SequenceMatcher(None, a, b, autojunk=False).ratio()
            lr = len(b) / max(len(a), 1)
            sims.append(r)
            lrs.append(lr)
            if r < 0.8 or not (0.8 <= lr <= 1.25):
                flags.append((s[:8], round(r, 3), round(lr, 3), len(a), len(b), idx[s]["outcome"]))
        if not sims:
            print(json.dumps({"label": label, "n": 0}))
            continue
        print(json.dumps({"label": label, "n": len(sims),
                          "sim_median": round(statistics.median(sims), 3), "sim_min": round(min(sims), 3),
                          "len_median": round(statistics.median(lrs), 3), "len_min": round(min(lrs), 3),
                          "len_max": round(max(lrs), 3), "flagged": flags}))


main()
