"""Measures vision.layer_words' thresholds against a run's held vision texts: for each threshold,
the PDF page images that skip vision, their page's text layer holding at least that many words, and
the thin pages among them with the vision words they lose.

Usage: python3 -I layer-words.py DOCPROC RUN [N...], where DOCPROC is a docproc run directory, its
latest run read, and RUN is a docinfer run directory, <out>/runs/<id>, that described the page
images, its texts read from <out>/texts. N are the thresholds, 50 100 150 200 when none is given.

A page image is an image output of the document stage with a page in its meta; its layer is the
text output of the same input, part, and page. A word is a run of Unicode letters or digits, as
docinfer counts it. A page is thin when its vision text holds at least twice its layer's words and
at least 60 more. It prints one JSON line of the page images, those with a layer, and those
paired with a held vision text, then one per threshold: the page images skipped, the thin pages
skipped, the vision words of those thin pages, the vision words the skipped layers lack, counted
per lowercased word, and the share of the run's vision request time the skipped images took.
"""

import collections
import glob
import json
import os
import sys
import unicodedata


def words(s):
    out, cur = [], []
    for c in s:
        k = unicodedata.category(c)
        if k[0] == "L" or k == "Nd":
            cur.append(c)
        elif cur:
            out.append("".join(cur))
            cur = []
    if cur:
        out.append("".join(cur))
    return out


def read(path):
    with open(path, "rb") as f:
        return f.read().decode("utf-8", "replace")


def latest(docproc):
    runs = sorted(glob.glob(os.path.join(docproc, "runs", "*", "record.jsonl")))
    if not runs:
        sys.exit(f"no docproc run in {docproc}")
    return runs[-1]


def pages(docproc):
    out = collections.defaultdict(dict)
    with open(latest(docproc), encoding="utf-8") as f:
        for line in f:
            e = json.loads(line)
            if e["event"] != "output" or e.get("stage", {}).get("name") != "document":
                continue
            o = e["output"]
            page = (o.get("meta") or {}).get("page")
            if page is None or o["kind"] not in ("image", "text"):
                continue
            out[(e["input"], e["part"]["id"], page)][o["kind"]] = o["sha256"]
    return out


def vision(run):
    texts, took, total = {}, {}, 0
    with open(os.path.join(run, "inferences.jsonl"), encoding="utf-8") as f:
        for line in f:
            e = json.loads(line)
            if e["capability"] != "vision":
                continue
            total += e.get("took_ms", 0)
            for c in e["content_sha256"]:
                took[c] = e.get("took_ms", 0)
                if e.get("text_sha256"):
                    texts[c] = e["text_sha256"]
    return texts, took, total


def main():
    if len(sys.argv) < 3 or sys.argv[1] in ("-h", "--help"):
        sys.exit(__doc__)
    docproc, run = sys.argv[1], sys.argv[2].rstrip("/")
    thresholds = [int(n) for n in sys.argv[3:]] or [50, 100, 150, 200]
    out = os.path.dirname(os.path.dirname(run))
    texts, took, total = vision(run)
    pg = pages(docproc)
    images = [v for v in pg.values() if "image" in v]
    rows = []
    for v in images:
        if "text" not in v:
            continue
        layer = words(read(os.path.join(docproc, "content", v["text"][:2], v["text"])))
        r = {"image": v["image"], "layer": len(layer), "lw": collections.Counter(w.lower() for w in layer)}
        t = texts.get(v["image"])
        if t:
            vis = words(read(os.path.join(out, "texts", t[:2], t)))
            r["vision"] = len(vis)
            r["vw"] = collections.Counter(w.lower() for w in vis)
        rows.append(r)
    paired = [r for r in rows if "vision" in r]
    thin = [r for r in paired if r["vision"] >= 2 * r["layer"] and r["vision"] - r["layer"] >= 60]
    print(json.dumps({"page_images": len(images), "with_layer": len(rows), "no_layer": len(images) - len(rows),
                      "paired": len(paired), "thin": len(thin)}))
    for n in thresholds:
        skipped = [r for r in rows if r["layer"] >= n]
        tskip = [r for r in thin if r["layer"] >= n]
        ms = sum(took.get(r["image"], 0) for r in skipped)
        print(json.dumps({
            "threshold": n,
            "skipped": len(skipped),
            "thin_skipped": len(tskip),
            "thin_vision_words": sum(r["vision"] for r in tskip),
            "words_layer_lacks": sum(sum((r["vw"] - r["lw"]).values()) for r in skipped if "vw" in r),
            "took_share": round(ms / total, 4) if total else 0,
        }))


if __name__ == "__main__":
    main()
