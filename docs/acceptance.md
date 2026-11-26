# Acceptance

The evidence that docinfer runs against the live rows: the timed run over docs-corpus v0.2.0, the
vision and transcription measurements that set the request shapes, and the live tiers. The raw
results stay out of git, under `~/runs/docs-corpus/baseline/v0.2.0/docinfer`: the run and its
rerun in `acceptance`, the re-probe in `reprobe`, the confirmation in `confirm`. Each table names
the file it is read from. The earlier runs, over the retired `~/docs` pool, are kept as their
[findings](#findings-from-the-retired-docs-runs-2026-11-25).

## docs-corpus v0.2.0

docinfer ran over docproc's baseline run of
[docs-corpus v0.2.0](https://github.com/standards-lab/docs-corpus/blob/main/docs/baseline.md), the
page that holds the tag's figures: 299 files, read in 2.3 h and reread from the cache in 1.4 s.
Vision described 2,551 of 2,558 images, 6 incomplete and 1 refused; transcription 1,054 of 1,055
windows, 1 incomplete; embeddings 18,814 chunks, none rejected. None failed; the run's 2 failures
are the refusal and the incomplete window.

### What ran

| | |
|---|---|
| docproc run | `01a218c9-8847-7ec5-b98e-cf5803c6a989` (docproc v0.9.0, cmd/docproc v0.2.7), in `~/runs/docs-corpus/baseline/v0.2.0/docproc` |
| docinfer runs | run `01a218e4-ddaa-7101-949b-0c9030a50690` and its rerun, `01a21964-5204-75c7-9d78-7f13ce1279db` |
| Build | docinfer `bef66ff` (`v0.3.0-6-gbef66ff`), cmd/docinfer `cmd/docinfer/v0.2.0-4-gbef66ff` (pseudo-version `v0.2.1-0.20261126172518-bef66ff13b6a`), with go-ai v0.5.0 and docproc v0.9.0, go1.27.4, built by `acceptance/run.sh` (`BUILD.txt`) |
| Machine | the laptop, `terminal`: Intel Core Ultra 9 185H, 22 threads, Omarchy on kernel 7.2.15-1-omarchy |
| Command | `acceptance/run.sh --work ~/runs/docs-corpus/baseline/v0.2.0/docinfer/acceptance --docproc ~/runs/docs-corpus/baseline/v0.2.0/docproc --vision 32 --audio 48 --embedding 16`, twice |
| Configuration | the run directory's `acceptance/config/config.json`, which `run.sh` writes: vision 32 in flight, transcription 48 (chat mode), embeddings 16, e5's `passage: `/`query: ` prefixes, 1,024 dimensions, tokens counted through the embedding alias; every other setting at cmd/docinfer's starting value |
| Gateway | `http://gb10-2:3000`, docinfer's consumer key, each capability at its alias's `/<alias>/v1` |

The concurrencies are the row caps acceptance leaves. Every request went to its alias's row, per
the gateway's request log:

| Alias | Row | Served model | docinfer's use |
|---|---|---|---|
| `vision` | `gb10-2:8001` | `nvidia/Gemma-4-26B-A4B-NVFP4` | image descriptions |
| `audio` | `framework:8002` | `google/gemma-4-E4B-it` | transcription, chat mode |
| `embedding` | `framework-2:8003` | `intfloat/multilingual-e5-large` | embeddings and their token counts |

### The run and its rerun

From `acceptance/logs/run-1.log` and `run-2.log`; vectors and runes from `acceptance/check-run.py`
over each run's directory.

| | Run | Rerun |
|---|---|---|
| Run ID | `01a218e4-ddaa-7101-949b-0c9030a50690` | `01a21964-5204-75c7-9d78-7f13ce1279db` |
| Started | 2026-11-26T12:26:21-05:00 | 2026-11-26T14:45:34-05:00 |
| Exit | 0 | 0 |
| Wall time | 8,330.5 s (2.3 h) | 1.4 s |
| Vision | 2,558 images: 2,551 described, 6 incomplete, 1 refused, 0 failed; 2,454 requests, 104 held | 0 requests, 2,558 held; outcomes unchanged |
| Transcription | 1,055 windows: 1,054 transcribed, 1 incomplete, 0 refused, 0 failed, 0 unplaced; 1,043 requests, 12 held; 56 transcripts, 874 seams matched, 125 unmatched | 0 requests, 1,055 held; outcomes unchanged |
| Embeddings | 1,964 texts and 299 metadata: 18,814 embedded, 0 rejected; 5,001 requests, 966 held | 0 requests, 18,814 held |
| Totals | 299 inputs, 6,068 outputs, 18,814 chunks, 2 failures (the refusal and the incomplete window) | the same |
| Vectors | 18,814, all 1,024 dimensions; worst \|norm − 1\| 8.86e-08 | the same |
| Chunk lines | 18,814; 20,046,162 covered runes, 19,644,307 distinct | identical as a set: both runs' sorted `chunks.jsonl` hash to sha256 `19ffce9f…` |

Covered runes by kind, the same in both runs:

| Kind | Lines | Distinct | Runes | Distinct runes |
|---|---|---|---|---|
| metadata | 299 | 297 | 34,620 | 34,423 |
| output | 13,523 | 12,945 | 15,273,177 | 14,898,558 |
| transcript | 265 | 261 | 346,783 | 346,588 |
| vision | 4,727 | 4,426 | 4,391,582 | 4,364,738 |
| **total** | **18,814** | **17,929** | **20,046,162** | **19,644,307** |

The gateway's route timeout answered 4 vision attempts 504 during the run; docinfer retried each,
and every image was answered.

### The Azure check

No request was answered by Azure, in the run, the rerun, the [re-probe](#re-probe-at-1120-image-tokens),
or the [confirmation](#confirmation). The check reads the gateway's request log, Postgres's
`request_logs` on gb10-2, through `docker exec gateway-postgres-1 psql`: the rows whose
`agentgateway_user` is `docinfer`, from each run's start to its end, grouped by alias,
`gen_ai_provider_name`, the upstream in `attributes_json`'s `endpoint`, and HTTP status. An Azure
provider writes `azure` in `gen_ai_provider_name` (ai-hosting's `docs/gateway.md`, "Overflow"), a
row `custom`.

| Run | Rows | Provider | Endpoint | Azure rows |
|---|---|---|---|---|
| Run | 8,502: audio 1,043, embedding 5,001, vision 2,458 (2,453 200, one 400, four 504) | `custom` | the alias's row | 0 |
| Rerun | 0 | | | 0 |
| Re-probe | 76 vision, all 200 | `custom` | `gb10-2:8001` | 0 |
| Confirmation | 37 vision, all 200 | `custom` | `gb10-2:8001` | 0 |

No `docinfer` row from 12:26:21 to 15:03:35 names a provider but `custom` or holds `azure` in its
attributes.

### Incomplete and refused

Every one is classified, and none is docinfer's fault, so the run surfaced no new defect.

| Item | Outcome | Reason |
|---|---|---|
| `email/mimekit/jwz.mbox`, message 60, attachment 1 | refused (400, `Failed to load image: image file is truncated`) | a legitimate refusal: the attachment is truncated |
| `email/mimekit/jwz.mbox`, message 56, attachment 2 (`a44fac59…`) | incomplete, `repetition` | a looping reply, held incomplete; described at both samplings in the re-probe |
| `pdf/large/cfr-2024-title32-vol1.pdf`, `c7ba76ec…` | incomplete, `repetition` | a looping reply; described at both samplings in the re-probe |
| `pdf/large/cfr-2024-title32-vol1.pdf`, `9f30f767…` | incomplete, `max_output_tokens` | a table page; incomplete at both samplings in the re-probe, described in the confirmation |
| `pdf/scanned/olmocr-bench/tables-fa18a15c…_pg2_pg1.pdf` | incomplete, `max_output_tokens` | a long table past 2,048 tokens, a known limit ([Decisions](#decisions)) |
| `pdf/scanned/olmocr-bench/old_scans-70.pdf`, `old_scans-71.pdf` | incomplete, `repetition` and `max_output_tokens` | the 8,000-pixel newspaper scans, a known limit |
| `video/tears-of-steel/tos-clip-180s-480p-subs.mkv`, window 5 | incomplete | the capped window, its trimmed text kept and joined ([Transcription](#transcription)) |

### Recipes

The recipes these runs and measurements were made with are committed in
[`acceptance/`](../acceptance/README.md), each taking its directories and endpoint as arguments and
sending to the model gateway by default. The retired `~/docs` runs used earlier copies of them,
under the names in the first column.

| Recipe there | Committed as | Run as |
|---|---|---|
| `run-acceptance.sh` | [`acceptance/run.sh`](../acceptance/run.sh) | `acceptance/run.sh --work DIR --docproc DIR`, the rerun the same again |
| `tools/check-run.py` | [`acceptance/check-run.py`](../acceptance/check-run.py) | `python3 -I acceptance/check-run.py OUT/runs/<id>` |
| `tools/sweep/` `run-sweep.sh`, `run-native.sh`, `run-reprobe.sh`, `run-confirm.sh` | [`acceptance/sweep/`](../acceptance/sweep): `run.sh sweep`, `native`, `reprobe`, `confirm` | `acceptance/sweep/run.sh reprobe --out DIR --docproc DIR --only FILE` |
| `tools/sweep/compare.py` | [`acceptance/sweep/compare.py`](../acceptance/sweep/compare.py), its base label an argument | `python3 -I acceptance/sweep/compare.py DIR BASE LABEL...` |
| `tools/transcribe-eval/` `run-eval.sh`, `run-t0.sh` | [`acceptance/transcribe-eval/`](../acceptance/transcribe-eval): `run.sh eval`, `t0` | `acceptance/transcribe-eval/run.sh eval --out DIR --docproc DIR` |
| `tools/transcribe-eval/compare.py`, `score.py` | the same names, `score.py` reading a docs-corpus checkout's truth files | `python3 -I acceptance/transcribe-eval/score.py CORPUS RESULTS TEXTS LABEL...` |

The `~/docs` runs and measurements sent straight to the rows; the v0.2.0 runs and measurements went
through the gateway.

## Vision

### Sampling at 280 image tokens

From the retired `~/docs` pool, 2026-11-25; not rerun over docs-corpus v0.2.0.

The 32-image seed-2 sample at 4 in flight, `max_tokens` 2,048, before ai-hosting's re-tune
(`~/runs/docinfer/tools/sweep/incomplete-probe.jsonl`, `native-probe.jsonl`):

| Native sampling | Described | Incomplete | Images/min | Read |
|---|---|---|---|---|
| row default | 28 | 4 | 8.50 | |
| `temperature` 0 | 26 | 6 | 7.84 | best on tables and two-column text |
| + `repetition_penalty` 1.1 | 25 | 7 | 7.51 | a new loop on a real page |
| + `frequency_penalty` 0.3 | 31 | 1 | 10.12 | garbles tables, hides loops |
| + `frequency_penalty` 0.1 | 26 | 6 | 7.16 | garbles digits |

No penalty is safe; temperature 0 was taken (`eab052e`), and later reversed.

### Concurrency sweep

From the retired `~/docs` pool, 2026-11-25; not rerun over docs-corpus v0.2.0.

160 images drawn with seed 1, temperature 0, at 280 image tokens
(`~/runs/docinfer/tools/sweep/results.jsonl`, `sweep.log`). No HTTP error or failure at any value.

| In flight | Images/min | Described | Incomplete | p50 s | p95 s |
|---|---|---|---|---|---|
| 4 | 8.44 | 123 | 37 | 21.9 | 73.8 |
| 6 | 11.27 | 123 | 37 | 22.9 | 82.4 |
| 8 | 14.38 | 128 | 32 | 25.9 | 91.2 |
| 10 | 16.39 | 127 | 33 | 27.8 | 99.3 |
| 12 | 18.61 | 130 | 30 | 30.4 | 107.5 |
| 14 | 19.07 | 124 | 36 | 30.8 | 114.6 |
| 15 | 19.71 | 126 | 34 | 33.6 | 116.7 |
| 16 | 20.39 | 126 | 34 | 34.9 | 120.4 |

The winner was 16 (15 is 3.3% behind, outside 3%), and run 1 ran at it. It is superseded: after
ai-hosting's re-tune the row caps at 4 in flight, and the retired runs ran vision at 4 with no
re-sweep; the v0.2.0 run sent 32 through the gateway, the cap acceptance leaves. cmd's starting
value stays 4.

### ai-hosting's re-tune

ai-hosting's report (its PR #13, `7346d53`): the vision row's `max_soft_tokens` 280 → 1,120
(~1,100 prompt tokens a page), throughput peaking at 4 in flight, its cap 16 → 4, queue 32. The
audio row: a 30 s hard clip limit, 25 tokens a second of audio, cap 8 → 32, load target 32.

### Re-probe at 1,120 image tokens

Rerun over docs-corpus v0.2.0: 4 in flight through the gateway, `max_tokens` 2,048, images sent
whole, on 2026-11-26 (`~/runs/docs-corpus/baseline/v0.2.0/docinfer/reprobe`: `reprobe.jsonl`,
`reprobe.log`, `texts-sample-*`, `texts-real-*`). The seed-2 sample is drawn from the v0.2.0
docproc run; the real pages are the run's 6 incomplete vision images (`incomplete.shas`, drawn
from `inferences.jsonl` as [`acceptance/README.md`](../acceptance/README.md#vision) gives).

| Set | Sampling | Described | Incomplete | Images/min |
|---|---|---|---|---|
| seed-2 sample, 32 | temperature 0 | 29 | 3 | 6.97 |
| seed-2 sample, 32 | row default | 32 | 0 | 9.23 |
| real pages, 6 | temperature 0 | 2 | 4 | 3.30 |
| real pages, 6 | row default | 2 | 4 | 3.28 |

The sample's 3 incomplete at temperature 0 are CFR pages that loop (`repetition`); at the row
default none loops. Of the real pages, the jwz.mbox attachment and the CFR page `c7ba76ec…` that
looped in the run are described at both samplings; the 4 incomplete at both are the CFR table page
`9f30f767…` (`repetition` at temperature 0, `max_output_tokens` at the row default), the
`tables-fa18a15c…` crop-weather table, and the two newspaper scans, `old_scans-70` and
`old_scans-71`, 8,000-pixel images past 2,048 tokens. On the sample's 29 images both described,
the texts' median similarity is 0.987, 1 below 0.5.

The retired `~/docs` re-probe, over the 110 real pages run 1 held incomplete at 280 image tokens,
found the sample 28 described and 4 incomplete at either sampling, and the real pages 97 and 13 at
temperature 0, 108 and 2 at the row default.

### Image bound

`Vision.MaxPixels` (`e83fa68`) bounds an image's area: a PNG or JPEG over it is downscaled by area
average in its source format, keeping its aspect, in memory for the request only; the original is
untouched, and the bound is in the identity. The copy carries the source's orientation and color
metadata (`d6c1315`). cmd's `vision.max_pixels` starts at 2,580,480 (`38df198`): Gemma 4's
processor at 1,120 soft tokens is 1,120 × 9 patches × 16² pixels, aspect-preserving, so the bound
drops nothing the model would keep. Of retired run 1's 3,471 images (`~/docs`, 2026-11-25), 26
PNGs exceed it, none oriented or with an ICC profile; 19 carry sRGB, 4 cHRM.

### Confirmation

Rerun over docs-corpus v0.2.0: Gemma 4's card sampling (`temperature` 1.0, `top_p` 0.95, `top_k`
64), `max_pixels` 2,580,480, 4 in flight through the gateway, on 2026-11-26
(`~/runs/docs-corpus/baseline/v0.2.0/docinfer/confirm`: `confirm.jsonl`, `confirm.log`,
`confirm.shas`). The pages are the re-probe's temperature-0 incomplete pages and the oversized
scans, `old_scans-69`, `-70`, and `-71`.

| Set | Described | Incomplete | Images/min |
|---|---|---|---|
| 2 pages incomplete at temperature 0 in the re-probe (CFR `9f30f767…`, `tables-fa18a15c…`) and 3 oversized scans | 2 | 3 | 2.64 |
| seed-2 sample, 32 | 32 | 0 | 9.32 |

The CFR table page is described in 886 tokens, and the 4,375 × 5,389 scan in 1,077; the crop-weather
table and both 8,000-pixel newspaper scans stay incomplete past 2,048 tokens. The sample's 32 are
all described, the 3 CFR pages that loop at temperature 0 among them.

Downscaling no longer changes the newspaper scans' time. Sent whole in the re-probe, `42a7e670…`
took 92.9 s and `a136d2a1…` 109.1 s at temperature 0 (`reprobe/texts-real-t0/index.jsonl`,
`took_ms`); downscaled here, 84.3 s and 113.7 s (`confirm/texts-confirm-pages/index.jsonl`). The
retired `~/docs` confirmation, over 11 CFR pages and the same 3 scans, described 11 and held 3
incomplete, the sample 29 and 3, and timed the scans six-fold faster downscaled, 190 s against
1,200 s whole.

### Decisions

| Setting | Value | Why |
|---|---|---|
| `vision.native` | `{"temperature":1.0,"top_p":0.95,"top_k":64}`, sent explicitly (`38df198`) | at 1,120 image tokens the row's default loops less than temperature 0 on real pages |
| `max_tokens` | 2,048, held | the retired `~/docs` re-probe's real pages complete within it |
| `vision.max_pixels` | 2,580,480 (`38df198`) | Gemma 4's processor rule; oversized scans no longer sent whole |
| concurrency | 4 | the row's cap |
| `response_header_timeout` | 600 s, cmd's starting value for vision's endpoint (`3948bce`); the others 180 s | a non-streaming reply's header waits for the whole reply; the row runs ~90–110 ms a token at 4 in flight, and retired run 3 saw 204 s for 1,816 tokens |
| the two newspaper scans | incomplete, a known limit | their text runs past 2,048 tokens |

## Transcription

From the retired `~/docs` pool, 2026-11-25; not rerun over docs-corpus v0.2.0.

602 distinct windows of 49 inputs drawn with seed 1, 32 in flight, chat mode
(`~/runs/docinfer/tools/transcribe-eval`: `results.jsonl`, `eval.log`, `texts/`).

| Label | `max_tokens` | Sampling | Wall s | Incomplete | Completion tokens: max, median |
|---|---|---|---|---|---|
| `max512` | 512 | row default | 293 | 2 | 512, 87 |
| `max0` | none | row default | 256 | 0 | 689, 86 |
| `t0-a` | 512 | `temperature` 0 | 283 | 0 | 137, 85 |
| `t0-b` | 512 | `temperature` 0 | 279 | 0 | 137, 86 |

- **Capped against uncapped.** One uncapped reply passed 512 tokens; both capped-out windows were
  invented Hindi or Nepali monologues on near-silent video windows, invented uncapped too.
- **Repeatability.** At the row default, 19 of 602 texts are equal across `max512` and `max0`
  (median similarity 0.97, 36 below 0.5); at temperature 0, 477 of 602 are equal across `t0-a`
  and `t0-b` (4 below 0.5).
- **Similarity.** Here and in [the re-probe](#re-probe-at-1120-image-tokens), difflib's
  `SequenceMatcher` ratio, `autojunk` off, over each window's or page's two texts (`*.txt` but
  `*.reply.txt`), pairs both empty skipped, the page pairs those both runs described per their
  `index.jsonl`.
- **Error against references.** 28 single-window clips with truth files, WER, or CER for zh, ja,
  and ko, from `python3 -I score.py results.jsonl texts t0-a t0-b max512 max0`:

| Dataset | Clips | `t0-a` | `t0-b` | `max512` | `max0` |
|---|---|---|---|---|---|
| fleurs-ar (WER) | 3 | 0.188 | 0.188 | 0.203 | 0.246 |
| fleurs-hi (WER) | 3 | 0.132 | 0.132 | 0.143 | 0.165 |
| fleurs-ja (CER) | 3 | 0.097 | 0.084 | 0.129 | 0.090 |
| fleurs-ru (WER) | 3 | 0.127 | 0.127 | 0.091 | 0.127 |
| fleurs-zh (CER) | 3 | 0.124 | 0.095 | 0.248 | 0.314 |
| librispeech (WER) | 4 | 0.015 | 0.015 | 0.029 | 0.000 |
| minds14-es (WER) | 3 | 0.309 | 0.309 | 0.279 | 0.294 |
| minds14-fr (WER) | 3 | 0.588 | 0.588 | 0.353 | 0.529 |
| minds14-ko (CER) | 3 | 0.102 | 0.102 | 0.114 | 0.125 |
| **overall** | 28 | **0.151** | **0.145** | **0.165** | **0.184** |

| Setting | Value | Why |
|---|---|---|
| `transcription.max_tokens` | 512 (go-ai `e6bd824`; docinfer `d40156e`, `fb0f87b`) | bounds a runaway reply; the longest reply at temperature 0 is 137 tokens |
| `transcription.native` | `{"temperature":0}` (go-ai `e375fbd`; docinfer `253fdc1`, `c960bc1`) | repeatable, and lower error overall |
| concurrency | 32 | the audio row's load target |
| a reply capped at `max_tokens` | its text kept, trimmed of the repeat, joined, and held incomplete (escalation 8: go-ai `35208a1`; docinfer `d6236ad`, transcription request 3) | in run 4, a 27 s window of normal speech looped to the cap and joined empty |

A capped window's tokens are counted: go-ai's `IncompleteTranscriptError` carries the reply's
usage (`9742456`), and docinfer counts it (`b8c4cf2`), found at BRIEF and fixed before SHIP.

## Live tiers

| Tier | Commit | Result on the rows |
|---|---|---|
| go-ai root tier, input-type and tokenize scenarios | go-ai `00dbfdb` | pass |
| go-ai `TestTranscribeChat`, after the cap and card instruction | go-ai `e6bd824`, before `e375fbd` | pass on `framework:8002`; `TestTranscribeRoute` skips, the row has no route |
| docinfer base tier, extraction on the agent row included | `8103538`, `3a43d3d` | pass |
| cmd's `TestCLI_RunAndReport` | `8103538`, `3a43d3d` | pass |
| go-ai root tier and `tools/infer`'s live tier, the transcription scenarios after `ChatNative` included | go-ai `35208a1`, after `e375fbd` | pass; `TestTranscribeRoute` skips, the row has no route |
| docinfer base tier: extraction on the agent row, the transcription route, and chat | `65c99d9` with go-ai `35208a1` | pass |
| cmd's `TestCLI_RunAndReport` | `65c99d9` with go-ai `35208a1` | pass |

The last three rows ran 2026-11-25 ~20:16, all four rows direct with dummy tokens
(`~/runs/docinfer/logs/live-tiers-2026-11-25.log`).

## Findings from the retired ~/docs runs, 2026-11-25

Runs 1–9 read docproc run `01a208e5-e581-7b95-b43a-0f91f8d3b40e` over the retired `~/docs` pool,
from 2026-11-25 05:39 to 21:04, sending straight to the rows: run 1 at vision 16, the fresh runs
3–9 at vision 4, and runs 5 on without onenote.rs's generated pages. Their results are retired; the
findings they gave, and the fixes, stand.

| Finding | Evidence | Fix |
|---|---|---|
| Transcription groups were keyed by part ID and stage alone, so every whole-file input (part `/`) shared one group; the first window closed it and ~1,188 later windows were dropped uncounted | run 1: 1,212 windows, 24 transcribed, 4 transcripts | `137a724` (`Input.Origin`; groups keyed by origin, part, and stage; `Unplaced` counted), `6d0c74f` (a window past its group's joined count is `Unplaced`) |
| e5's row refuses an input over 8,192 characters (vLLM's 16 a token × `max_model_len`) before tokenizing; mostly-space docproc texts gave chunks within 480 tokens but 12,777–59,747 characters | 8 embedding rejections (400) in run 1: olmocr-bench `multi_column-09f9…`, `cfr-2024-title32-vol1.pdf` | `f121620` (`chunk.Splitter.Runes`; cmd `embeddings.chunk_runes` starting 8,192, the document prefix subtracted: 8,183 for e5) |
| A part two inputs share was named by whichever input's record line cmd had read last, so attribution depended on timing | run 2's `input` differences on the two large OneNote files | `73d600c` (each line names the `Origin` its result carries) |
| Vision replies looped to `max_tokens` | run 1: 670 of 3,471 distinct images incomplete, 657 trimmed loops and 13 out of tokens, 95 of them real CFR pages | `76581d0` before the run (a looping reply is trimmed and held incomplete, reason `repetition`); after it, ai-hosting's re-tune and the request shape in [Vision](#vision) |
| `msg_22.eml`'s two attachments refused as unloadable images | 2 vision refusals in runs 1 and 6 | none: a legitimate refusal |
| A non-streaming vision reply's header waits for the whole reply, past cmd's 180 s | run 3's exit 1; 204 s seen for 1,816 tokens at ~90–110 ms a token | the acceptance config's 600 s for run 4 on; `3948bce` (cmd starts vision's endpoint at 600 s, the others at 180 s) |
| onenote.rs's generated timing sections are pseudo-word pages, not realistic content, and noise in the measurements | 1,998 of 4,718 image events; 1,158 of run 1's vision loops | escalation 7: their images removed from the input from run 5 on, the baselines recomputed without them |
| Run 6's 3 vision images incomplete | `old_scans-70` (`repetition`) and `old_scans-71` (`max_output_tokens`), the 8,000-pixel newspaper scans; olmocr-bench `tables-fa18a15c…_pg2_pg1.pdf` (`max_output_tokens`), a long real crop-weather table cut at 2,048 tokens | none: held text, not failures; a page past `max_tokens` 2,048 is a known limit ([Decisions](#decisions)) |
| A transcription window of normal speech looped to the 512 cap at temperature 0, was held incomplete, and joined empty | `tos-clip-180s-480p-subs.mkv` window 5 (27 s), run 4; sent alone, the row answers in 39 tokens | escalation 8: go-ai `35208a1` (`IncompleteTranscriptError.Text`, chat mode), docinfer `d6236ad` (the capped window holds its text trimmed of the loop, joins it, and stays incomplete; transcription request 3) |
