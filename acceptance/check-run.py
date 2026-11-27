"""Checks a docinfer run's chunk record: each vector's dimensions and norm, and per source kind
its chunk lines, distinct chunks, and covered runes, over the lines and over the distinct chunks;
then the metadata texts by what they render, an input or a record's media type, each text counted
by its first chunk line, with their chunk lines.

Usage: python3 -I check-run.py RUN, where RUN is a run's directory under docinfer's output
directory, <out>/runs/<id>, which holds chunks.jsonl.
"""

import base64
import collections
import json
import math
import struct
import sys


def main():
    if len(sys.argv) != 2 or sys.argv[1] in ("-h", "--help"):
        sys.exit(__doc__)
    run = sys.argv[1]
    lines = collections.Counter()
    distinct = collections.defaultdict(set)
    runes = collections.Counter()
    lrunes = collections.Counter()
    dims = collections.Counter()
    meta = collections.Counter()
    mlines = collections.Counter()
    worst = 0.0
    n = 0
    with open(f"{run}/chunks.jsonl", encoding="utf-8") as f:
        for raw in f:
            c = json.loads(raw)
            k = c["source"]["kind"]
            lines[k] += 1
            lrunes[k] += len(c["text"])
            if k == "metadata":
                o = c["source"].get("output")
                m = o["media_type"] if o else "input"
                meta[m] += c["index"] == 0
                mlines[m] += 1
            if c["digest"] not in distinct[k]:
                distinct[k].add(c["digest"])
                runes[k] += len(c["text"])
            n += 1
            if "vector" not in c:
                dims["none"] += 1
                continue
            v = base64.b64decode(c["vector"])
            d = len(v) // 4
            dims[d] += 1
            norm = math.sqrt(sum(x * x for x in struct.unpack(f"<{d}f", v)))
            worst = max(worst, abs(norm - 1))
    print(f"chunk lines {n}; dimensions {dict(dims)}; worst |norm-1| {worst:.2e}")
    print("| kind | lines | distinct | runes (lines) | runes (distinct) |")
    for k in sorted(lines):
        print(f"| {k} | {lines[k]} | {len(distinct[k])} | {lrunes[k]} | {runes[k]} |")
    print(f"| total | {sum(lines.values())} | {sum(len(s) for s in distinct.values())} "
          f"| {sum(lrunes.values())} | {sum(runes.values())} |")
    print("| metadata | texts | lines |")
    for m in sorted(meta):
        print(f"| {m} | {meta[m]} | {mlines[m]} |")
    print(f"| total | {sum(meta.values())} | {sum(mlines.values())} |")


main()
