# Acceptance recipes

The recipes behind [`docs/acceptance.md`](../docs/acceptance.md): the acceptance run, the run
checker, and the vision and transcription measurements that set docinfer's request shapes. Each
takes its working directory, its input, and its endpoint as arguments, writes only under the
directory it is given, and sends to the model gateway, `http://gb10-2:3000/<alias>/v1`, unless told
otherwise. Results stay out of git.

| Recipe | What it does |
|---|---|
| [`run.sh`](run.sh) | The acceptance run: `docinfer run --report` over a docproc run directory, through the gateway |
| [`check-run.py`](check-run.py) | A run's vectors and covered runes by source kind, and its metadata texts by what they render, from its `chunks.jsonl` |
| [`layer-words.py`](layer-words.py) | `vision.layer_words`' thresholds against a run's held vision texts: the page images each skips, and the thin pages among them with the vision words they lose |
| [`sweep/`](sweep) | The vision measurements: the concurrency sweep, the native-sampling probe, the re-probe, and the confirmation |
| [`transcribe-eval/`](transcribe-eval) | The transcription measurements: capped against uncapped, temperature 0's repeatability, and error against a corpus's references |

Every script prints its usage with `--help`, and every Python script with no arguments.

## The gateway key

Each recipe sends docinfer's gateway key, `AI_GATEWAY_TOKEN`, taken from the environment or, when
it is unset, from this repository's age-encrypted [`fnox.toml`](../fnox.toml) through `fnox get`,
which needs your age key ([Secrets](../docs/development.md#secrets)). `sweep/run.sh` and
`transcribe-eval/run.sh` send `AI_VISION_TOKEN` or `AI_AUDIO_TOKEN` in its place when set. The key
is never written to disk: `run.sh` passes it to the command as `DOCINFER_ENDPOINTS_<NAME>_TOKEN`,
and the measurement commands read it from the environment.

## The acceptance run

```sh
acceptance/run.sh --work ~/runs/docinfer-v0.2.0 --docproc ~/runs/docproc/corpus-v0.2.0 \
  --vision 4 --audio 32 --embedding 4
```

`--work` holds `config/config.json`, which the script writes from its arguments, `bin/docinfer`,
built from this checkout, `BUILD.txt`, one line per run naming the commit, `logs/run-N.log` and
`run-N.exit`, and `out/`, docinfer's output directory. The configuration names three endpoints,
`vision`, `audio`, and `embedding`, each the gateway's alias of that name; transcription runs in
chat mode; embeddings take e5's `passage: ` and `query: ` prefixes and 1,024 dimensions, and count
tokens through the embedding alias's tokenize route. Every other setting is cmd/docinfer's starting
value. `--vision`, `--audio`, and `--embedding` set each capability's requests in flight, 4, 32, and
4 by default; `--gateway` names another gateway root; `--run` a docproc run other than its latest;
`--bin` a prebuilt binary. The command runs under `env -i`, so nothing else from the caller's
environment reaches it.

The rerun is the same command again: it resumes from `out/`'s cache, and its log is the next
`run-N`. A long run detaches as a user unit:

```sh
systemd-run --user --unit=docinfer-acceptance --working-directory="$PWD" \
  acceptance/run.sh --work ~/runs/docinfer-v0.2.0 --docproc ~/runs/docproc/corpus-v0.2.0
```

Check a finished run's vectors and runes with its run directory, `out/runs/<id>`:

```sh
python3 -I acceptance/check-run.py ~/runs/docinfer-v0.2.0/out/runs/<id>
```

Measure the layer-word thresholds over a docproc run directory and a run that described its page
images, by default at 50, 100, 150, and 200 words:

```sh
python3 -I acceptance/layer-words.py ~/runs/docproc/corpus-v0.2.0 ~/runs/docinfer-v0.2.0/out/runs/<id>
```

## Vision

`sweep` is a Go command over the library: it draws a docproc run's distinct images by a seeded
shuffle, runs docinfer's vision capability over them at one concurrency with cmd/docinfer's prompt
and reasoning effort, sends embeddings to an in-process fake, and appends one JSON line of counts,
throughput, and latency. `sweep/run.sh` builds it into `<out>/bin` and runs one measurement:

```sh
acceptance/sweep/run.sh sweep --out ~/runs/vision --docproc DIR 4 6 8 10 12 14 15 16
acceptance/sweep/run.sh native --out ~/runs/vision --docproc DIR default '{}' t0 '{"temperature":0}'
acceptance/sweep/run.sh reprobe --out ~/runs/vision --docproc DIR --concurrency 4 --only incomplete.shas
acceptance/sweep/run.sh confirm --out ~/runs/vision --docproc DIR --concurrency 4 --only confirm.shas
python3 -I acceptance/sweep/compare.py ~/runs/vision real-default real-t0
```

The re-probe sends at the row's image budget, ai-hosting's `max_soft_tokens` 1,120, which the row
sets; the request carries none. `--only` lists the images a set holds, one SHA-256 a line: for the
re-probe, the pages a run held incomplete, which the run's `inferences.jsonl` gives, held or not:

```sh
jq -r 'select(.capability=="vision" and .outcome=="incomplete").content_sha256[0]' \
  OUT/runs/<id>/inferences.jsonl | sort -u >incomplete.shas
```

and for the confirmation, the re-probe's `real-t0` incomplete pages with any oversized scans added:
`jq -r 'select(.label=="real-t0").incomplete_shas[]' reprobe.jsonl`. `compare.py` compares each
label's texts with a base label's on the images the base described.

## Transcription

`transcribe-eval` is a Go command over the library: it draws a docproc run's audio inputs by a
seeded shuffle, runs docinfer's chat-mode transcription over each drawn input's windows and plan,
and appends one JSON line per run with every window's outcome, finish reason, and tokens, keeping
each window's text and raw reply under `<texts>/<label>/`. Its measurement is package
`internal/transcribe`, whose test runs it against go-ai's `aitest` fake endpoint over a generated
docproc run. `transcribe-eval/run.sh` builds it and runs a pair of labels, ending with
`compare.py`'s comparison:

```sh
acceptance/transcribe-eval/run.sh eval --out ~/runs/audio --docproc DIR
acceptance/transcribe-eval/run.sh t0 --out ~/runs/audio --docproc DIR
python3 -I acceptance/transcribe-eval/score.py ~/architecture/docs-corpus ~/runs/audio/results.jsonl ~/runs/audio/texts t0-a t0-b max512 max0
```

Arguments after the options go to both runs, such as `-n 20 -seed 2 -max-windows 400`. `score.py`
reads the references from a docs-corpus checkout's `audio/short/*/*.truth.json`.

## The module

`sweep` and `transcribe-eval` live in their own Go module,
`github.com/standards-lab/docinfer/acceptance`, which is never tagged, so neither enters a
published module's zip or adds a requirement to one.
`mise run check` builds, vets, lints, and tests it with the other modules, shellchecks the scripts,
and parses the Python. In the workspace the scripts build against this checkout's library; the
check resolves the module's own requirements.
