"""Compares two transcribe-eval runs by label.

Usage: python3 -I compare.py RESULTS TEXTS A B, where RESULTS is the results.jsonl transcribe-eval
appended to and TEXTS its -texts directory. Takes each label's last line in RESULTS, prints each
run's summary, and per distinct window, by SHA-256, compares A's text with B's, reading
TEXTS/<label>/<sha>.txt (docinfer's text, empty unless transcribed) and <sha>.reply.txt (the raw
reply, kept for an incomplete window too).
"""

import json
import os
import statistics
import sys


def load(path, label):
    run = None
    with open(path) as f:
        for line in f:
            d = json.loads(line)
            if d.get("label") == label:
                run = d
    if run is None:
        sys.exit(f"compare: no run labelled {label!r} in {path}")
    return run


def read(texts, label, sha, suffix):
    p = os.path.join(texts, label, sha + suffix)
    if not os.path.exists(p):
        return None
    with open(p, encoding="utf-8") as f:
        return f.read()


def by_sha(run):
    out = {}
    for w in run["window_records"]:
        out.setdefault(w["sha256"], w)
    return out


def summary(run):
    ws = list(by_sha(run).values())
    toks = [w["completion_tokens"] for w in ws if w.get("finish_reason")]
    lat = sorted(w["latency_ms"] / 1000 for w in ws if w["latency_ms"])
    c = run.get("counts") or {}
    print(f"{run['label']}: max_tokens {run['max_tokens']}, {run['inputs']} inputs, {run['windows']} windows "
          f"({run['distinct_windows']} distinct), wall {run['wall_seconds']:.0f} s "
          f"({run['windows'] / max(run['wall_seconds'], 1e-9):.2f} windows/s), {run['http_requests']} requests")
    print(f"  counts: transcribed {c.get('transcribed')}, incomplete {c.get('incomplete')}, refused {c.get('refused')}, "
          f"failed {c.get('failed')}, unplaced {c.get('unplaced')}")
    if toks:
        print(f"  completion tokens: total {sum(toks)}, max {max(toks)}, median {statistics.median(toks):.0f}, "
              f"over 512: {sum(t > 512 for t in toks)}")
    if lat:
        print(f"  latency s: median {statistics.median(lat):.1f}, p95 {lat[int(0.95 * (len(lat) - 1))]:.1f}, max {lat[-1]:.1f}")
    finishes = {}
    for w in ws:
        finishes[w.get("finish_reason") or w["outcome"]] = finishes.get(w.get("finish_reason") or w["outcome"], 0) + 1
    print(f"  finish reasons: {finishes}")
    if run.get("run_error"):
        print(f"  run error: {run['run_error']}")


def main():
    if len(sys.argv) != 5:
        sys.exit(__doc__)
    path, texts, la, lb = sys.argv[1:]
    a, b = load(path, la), load(path, lb)
    summary(a)
    summary(b)
    wa, wb = by_sha(a), by_sha(b)
    shared = sorted(set(wa) & set(wb))
    tally = {}
    examples = {}

    def note(k, sha):
        tally[k] = tally.get(k, 0) + 1
        examples.setdefault(k, sha)

    for sha in shared:
        x, y = wa[sha], wb[sha]
        if x["outcome"] == "transcribed" and y["outcome"] == "transcribed":
            ta, tb = read(texts, la, sha, ".txt"), read(texts, lb, sha, ".txt")
            if ta == tb:
                note("both transcribed, equal", sha)
            elif tb.startswith(ta):
                note(f"both transcribed, differ: {la} a prefix of {lb}", sha)
            else:
                note("both transcribed, differ", sha)
        elif x["outcome"] == "incomplete" and y["outcome"] == "transcribed":
            ra, tb = read(texts, la, sha, ".reply.txt"), read(texts, lb, sha, ".txt")
            if ra is not None and tb.startswith(ra):
                note(f"{la} incomplete, its reply a prefix of {lb}'s text", sha)
            else:
                note(f"{la} incomplete, its reply not a prefix of {lb}'s text", sha)
        else:
            note(f"{la} {x['outcome']}, {lb} {y['outcome']}", sha)
    print(f"windows compared: {len(shared)} distinct (only in {la}: {len(set(wa) - set(wb))}, only in {lb}: {len(set(wb) - set(wa))})")
    for k in sorted(tally):
        print(f"  {k}: {tally[k]}  (e.g. {examples[k]})")


main()
