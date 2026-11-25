# Acceptance

The evidence that docinfer runs against the live rows: the timed runs over docproc's kept run,
the vision and transcription measurements that set the request shapes, and the live tiers. The
raw results stay out of git, under `~/runs/docinfer`; each table names the file it is read from.
This page is a draft until the fresh run's section is filled.

## What ran

docinfer read docproc run `01a208e5-e581-7b95-b43a-0f91f8d3b40e` in `~/runs/docproc/final`, which
nothing writes into: 592 inputs, 11,188 outputs. Each run is `docinfer run --report` through
`~/runs/docinfer/run-acceptance.sh`, under `env -i` so no `AI_*` variable or gateway key reaches
it, as a detached `systemd-run --user` unit. The live tiers send dummy `AI_<ROLE>_TOKEN`s. Every
request goes straight to a row:

| Role | Row | Served model | docinfer's use |
|---|---|---|---|
| vision | `gb10-2:8001` | Gemma-4-26B-A4B-NVFP4 | image descriptions |
| audio | `framework:8002` | `google/gemma-4-E4B-it` | transcription, chat mode |
| embedding | `framework-2:8003` | multilingual-e5-large | embeddings; tokenize at the row's root (`http://framework-2:8003`) |
| agent | `gb10-1:8000` | `openai/gpt-oss-120b` | extraction, live tier only |

The rows are tuned by ai-hosting as general endpoints, accuracy first, then throughput, never to
docinfer's requests. docinfer shapes its requests to each row's measured limits: window length,
`max_tokens`, sampling, image size, and concurrency.

## Run 1 and its rerun

Binary from docinfer `19c37a0` with go-ai `00dbfdb` (`~/runs/docinfer/BUILD.txt`), config
`~/runs/docinfer/config-run1.json`: vision 16 in flight, audio 8 (chat mode), embedding 4,
`input_types` `passage: `/`query: `. Output in `~/runs/docinfer/acceptance`; logs
`~/runs/docinfer/logs/run-1.log` and `run-2.log`.

| | Run 1 | Run 2 (rerun) |
|---|---|---|
| Exit | 0 | 0 |
| Time | 8,961 s | 2.3 s |
| Vision | 4,718 images, 3,471 requests: 3,438 described, 1,278 incomplete, 2 refused | 0 requests, 4,718 held |
| Transcription | 1,212 windows, 24 requests: 24 transcribed | 0 requests |
| Embeddings | 34,507 embedded, 8 rejected; 7,385 requests | 0 requests, 34,515 held |
| Chunks | 34,515 | 34,515; lines identical but for `input` on parts the two large OneNote files share |
| Failures | 10 (2 vision refusals, 8 embedding rejections) | 10, replayed |

| Finding | Evidence | Fix |
|---|---|---|
| Transcription groups were keyed by part ID and stage alone, so every whole-file input (part `/`) shared one group; the first window closed it and ~1,188 later windows were dropped uncounted | run 1: 1,212 windows, 24 transcribed, 4 transcripts | `137a724` (`Input.Origin`; groups keyed by origin, part, and stage; `Unplaced` counted), `6d0c74f` (a window past its group's joined count is `Unplaced`) |
| e5's row refuses an input over 8,192 characters (vLLM's 16 a token × `max_model_len`) before tokenizing; mostly-space docproc texts gave chunks within 480 tokens but 12,777–59,747 characters | 8 embedding rejections (400): olmocr-bench `multi_column-09f9…`, `cfr-2024-title32-vol1.pdf` | `f121620` (`chunk.Splitter.Runes`; cmd `embeddings.chunk_runes` starting 8,192, the document prefix subtracted: 8,183 for e5) |
| A part two inputs share was named by whichever input's record line cmd had read last, so attribution depended on timing | run 2's `input` differences on the two large OneNote files | `73d600c` (each line names the `Origin` its result carries) |
| Vision replies looped to `max_tokens` | 670 of 3,471 distinct images incomplete: 657 trimmed loops, 13 out of tokens; 95 of them real CFR pages | `76581d0` before the run (a looping reply is trimmed and held incomplete, reason `repetition`); after it, ai-hosting's re-tune and the request shape in [Vision](#vision) |
| `msg_22.eml`'s two attachments refused as unloadable images | 2 vision refusals | none: a legitimate refusal |

## Vision

### Sampling at 280 image tokens

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
ai-hosting's re-tune the row caps at 4 in flight, so the acceptance config runs vision at 4 with no
re-sweep. cmd's starting value stays 4.

### ai-hosting's re-tune

ai-hosting's report (its PR #13, `7346d53`): the vision row's `max_soft_tokens` 280 → 1,120
(~1,100 prompt tokens a page), throughput peaking at 4 in flight, its cap 16 → 4, queue 32. The
audio row: a 30 s hard clip limit, 25 tokens a second of audio, cap 8 → 32, load target 32.

### Re-probe at 1,120 image tokens

4 in flight, `max_tokens` 2,048 (`~/runs/docinfer/tools/sweep/reprobe.jsonl`, `reprobe.log`,
`texts-sample-*`, `texts-real-*`). The real pages are run 1's 110 real incomplete pages, 95 of
them CFR (`real-incomplete.shas`).

| Set | Sampling | Described | Incomplete | Images/min |
|---|---|---|---|---|
| seed-2 sample, 32 | temperature 0 | 28 | 4 | 5.96 |
| seed-2 sample, 32 | row default | 28 | 4 | 6.08 |
| real pages, 110 | temperature 0 | 97 | 13 | 5.00 |
| real pages, 110 | row default | 108 | 2 | 5.44 |

At temperature 0, 11 of the 13 are CFR loops; at the row default the 2 are `old_scans-70` and
`old_scans-71`, 8,000-pixel newspaper scans past 2,048 tokens, and no CFR page loops. Where both
described a page, the texts' median similarity is 0.996. The sample's incomplete images are
OneNote synthetic pages either way.

### Image bound

`Vision.MaxPixels` (`e83fa68`) bounds an image's area: a PNG or JPEG over it is downscaled by area
average in its source format, keeping its aspect, in memory for the request only; the original is
untouched, and the bound is in the identity. The copy carries the source's orientation and color
metadata (`d6c1315`). cmd's `vision.max_pixels` starts at 2,580,480 (`38df198`): Gemma 4's
processor at 1,120 soft tokens is 1,120 × 9 patches × 16² pixels, aspect-preserving, so the bound
drops nothing the model would keep. Of the run's 3,471 images, 26 PNGs exceed it, none oriented
or with an ICC profile; 19 carry sRGB, 4 cHRM.

### Confirmation

Gemma 4's card sampling (`temperature` 1.0, `top_p` 0.95, `top_k` 64), `max_pixels` 2,580,480,
4 in flight (`~/runs/docinfer/tools/sweep/confirm.jsonl`, `confirm.log`, `confirm.shas`):

| Set | Described | Incomplete | Images/min |
|---|---|---|---|
| 11 CFR pages that looped at temperature 0, and 3 oversized scans | 11 | 3 | 4.41 |
| seed-2 sample, 32 | 29 | 3 | 5.76 |

10 of the 11 CFR pages are described; one table page loops (described at the row default in the
re-probe, so a sampling draw). The 4,375 × 5,389 scan is described; both 8,000-pixel newspaper
scans stay incomplete past 2,048 tokens. The sample's 3 incomplete are OneNote synthetic pages.

### Decisions

| Setting | Value | Why |
|---|---|---|
| `vision.native` | `{"temperature":1.0,"top_p":0.95,"top_k":64}`, sent explicitly (`38df198`) | at 1,120 image tokens the row's default loops less than temperature 0 on real pages |
| `max_tokens` | 2,048, held | the re-probe's real pages complete within it |
| `vision.max_pixels` | 2,580,480 (`38df198`) | Gemma 4's processor rule; oversized scans no longer sent whole |
| concurrency | 4 | the row's cap |
| the two newspaper scans | incomplete, a known limit | their text runs past 2,048 tokens |

## Transcription

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

## Live tiers

| Tier | Commit | Result on the rows |
|---|---|---|
| go-ai root tier, input-type and tokenize scenarios | go-ai `00dbfdb` | pass |
| go-ai transcription scenarios after `e6bd824` and `e375fbd` | | TODO: the rerun on the rows is not yet recorded |
| docinfer base tier, extraction on the agent row included | `8103538`, `3a43d3d` | pass |
| cmd's `TestCLI_RunAndReport` | `8103538`, `3a43d3d` | pass |

## Fresh run (run 3)

TODO: filled when run 3 and its rerun finish.

Binary from docinfer `d6c1315` with go-ai `e375fbd` (`~/runs/docinfer/BUILD.txt`), config
`~/runs/docinfer/config/config.json`: vision 4, audio 32 (chat mode), embedding 4, every other
setting at cmd's starting value. Output in `~/runs/docinfer/acceptance-2`, which no earlier run's
cached vision reaches; log `~/runs/docinfer/logs/run-3.log`, started 2026-11-25T14:58:52-05:00.

| Criterion | Target | Run 3 |
|---|---|---|
| Exit | 0 | TODO |
| Refusals and failures | each classified | TODO |
| Vectors | 1,024 dimensions, unit norm | TODO |
| Report | renders | TODO |
| Covered runes | traced against the spike's 41,299,926 | TODO |
| Images | traced against the spike's 3,470 | TODO |
| Windows | traced against the spike's 1,254; each transcribed or counted | TODO |
| Time | recorded, not gated | TODO |
| Rerun | sends no inference or tokenize request, the model listing aside | TODO |
| Rerun's chunk lines | identical, `input` included | TODO |
