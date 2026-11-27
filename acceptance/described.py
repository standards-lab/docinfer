"""Finds the page images a run's vision described instead of transcribing, by the rule of
spike-ai-pipeline's docinfer finding: a page whose text layer holds at least N words, whose vision
text recalls under half of them, and whose vision text opens by describing the image. Then, by the
route docproc gave each input, the distinct images that reached vision in the run, those with a
vision text, and those whose text opens by describing the image.

Usage: python3 -I described.py DOCPROC RUN [N], where DOCPROC is a docproc run directory, its
latest run read, and RUN is a docinfer run directory, <out>/runs/<id>, its texts read from
<out>/texts. N is the layer words a page needs, 100 when none is given.

Pages are paired, and words counted, as layer-words.py pairs and counts them. Recall is the share
of the layer's words, compared lowercased as multisets, that the vision text holds. A text opens by
describing the image when, past leading whitespace, * and #, it matches DESCRIBES at its start,
case-insensitive: "This image", "The picture", "A ... shows". It prints one JSON line of the page
images paired with a vision text, those with at least N layer words, those of them with recall
under 0.5, and the pages and distinct images the rule finds; then one line for each page image
whose vision text opens by describing the image, whether the rule finds it or not; then one line
per route, sorted by images: the images, those with a vision text, and those whose text opens by
describing the image, each image counted under the first input the run holds its text for, or
the first input it names the image for when it holds none.
"""

import collections
import importlib.util
import json
import os
import re
import sys

DESCRIBES = re.compile(r"(this|the) (image|picture)\b|this is an image\b|an? [^.\n]{0,80}?\b(shows|contains|depicts)\b", re.I)


def layer_words():
    sys.dont_write_bytecode = True
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "layer-words.py")
    spec = importlib.util.spec_from_file_location("layer_words", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def describes(text):
    return DESCRIBES.match(text.lstrip(" \t\n*#")) is not None


def routes(record):
    out = {}
    with open(record, encoding="utf-8") as f:
        for line in f:
            e = json.loads(line)
            if e["event"] == "part" and e["part"]["id"] == "/":
                out[e["input"]] = (e["part"].get("content_type") or {}).get("route") or "none"
    return out


def held(run):
    out = {}
    with open(os.path.join(run, "inferences.jsonl"), encoding="utf-8") as f:
        for line in f:
            e = json.loads(line)
            if e["capability"] != "vision":
                continue
            for c in e["content_sha256"]:
                if c not in out or (e.get("text_sha256") and not out[c][1]):
                    out[c] = (e["input"], e.get("text_sha256"))
    return out


def main():
    if len(sys.argv) not in (3, 4) or sys.argv[1] in ("-h", "--help"):
        sys.exit(__doc__)
    docproc, run = sys.argv[1], sys.argv[2].rstrip("/")
    n = int(sys.argv[3]) if len(sys.argv) == 4 else 100
    lw = layer_words()
    out = os.path.dirname(os.path.dirname(run))
    texts = held(run)
    vision = {}

    def text(sha):
        if sha not in vision:
            vision[sha] = lw.read(os.path.join(out, "texts", sha[:2], sha))
        return vision[sha]

    paired = heavy = low = 0
    found, found_images, lines = 0, set(), []
    for (inp, part, page), v in sorted(lw.pages(docproc).items(), key=lambda kv: (kv[0][0], kv[0][1], int(kv[0][2]))):
        if "image" not in v or not texts.get(v["image"], (None, None))[1]:
            continue
        s = text(texts[v["image"]][1])
        row = {"input": inp, "part": part, "page": page, "image": v["image"][:12]}
        recall = None
        if "text" in v:
            layer = lw.words(lw.read(os.path.join(docproc, "content", v["text"][:2], v["text"])))
            lc = collections.Counter(w.lower() for w in layer)
            vc = collections.Counter(w.lower() for w in lw.words(s))
            recall = sum((lc & vc).values()) / len(layer) if layer else None
            row["layer"] = len(layer)
            row["recall"] = round(recall, 3) if recall is not None else None
            paired += 1
            if len(layer) >= n:
                heavy += 1
                if recall is not None and recall < 0.5:
                    low += 1
        row["vision"] = len(lw.words(s))
        if not describes(s):
            continue
        row["rule"] = row.get("layer", 0) >= n and recall is not None and recall < 0.5
        if row["rule"]:
            found += 1
            found_images.add(v["image"])
        row["opening"] = s.lstrip(" \t\n*#")[:100].replace("\n", " ")
        lines.append(row)
    print(json.dumps({"layer_words": n, "paired": paired, "at_least_n": heavy, "recall_under_half": low,
                      "described": found, "described_images": len(found_images)}))
    for row in lines:
        print(json.dumps(row, ensure_ascii=False))
    route = routes(lw.latest(docproc))
    images, with_text, opening = collections.Counter(), collections.Counter(), collections.Counter()
    for inp, t in texts.values():
        r = route.get(inp, "none")
        images[r] += 1
        if t:
            with_text[r] += 1
            if describes(text(t)):
                opening[r] += 1
    for r, k in images.most_common():
        print(json.dumps({"route": r, "images": k, "with_text": with_text[r], "opening_described": opening[r]}))


if __name__ == "__main__":
    main()
