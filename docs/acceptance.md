# Acceptance

The evidence that docinfer runs against the live rows: the timed runs over docproc's kept run,
the vision and transcription measurements that set the request shapes, and the live tiers. The
raw results stay out of git, under `~/runs/docinfer`: runs 1 and 2 in `acceptance`, the fresh
runs 3–8 in `acceptance-2`, the architect's new directory. Each table names the file it is read
from.

## What ran

docinfer read docproc run `01a208e5-e581-7b95-b43a-0f91f8d3b40e` in `~/runs/docproc/final`, which
nothing writes into: 592 inputs, 11,188 outputs; runs 1–4 read it. Runs 5–8 read
`~/runs/docproc/final-no-synthetic`, the same run less the images of onenote.rs's two generated
timing sections: 9,190 outputs. Those pseudo-word pages are not realistic content and were noise
in the vision measurements, so escalation 7 removed them ([the fresh run](#the-fresh-run-runs-38)).
Each run's binary is in `~/runs/docinfer/BUILD.txt` and its section's table. Each run is `docinfer run --report` through
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

1,158 of run 1's vision loops were onenote.rs's generated pages, removed from the input by
escalation 7 ([the fresh run](#the-fresh-run-runs-38)).

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
described a page, 97 pages, the texts' median similarity is 0.996, 1 below 0.5. The sample's incomplete images are
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

Downscaling cuts the newspaper scans' time six-fold. Sent whole at the row default in the
re-probe, `a136d2a1…` took 1,193.9 s and `42a7e670…` 1,213.9 s
(`~/runs/docinfer/tools/sweep/texts-real-default/index.jsonl`, `took_ms`); downscaled here, 190.4 s
and 188.2 s (`texts-confirm-pages/index.jsonl`).

### Decisions

| Setting | Value | Why |
|---|---|---|
| `vision.native` | `{"temperature":1.0,"top_p":0.95,"top_k":64}`, sent explicitly (`38df198`) | at 1,120 image tokens the row's default loops less than temperature 0 on real pages |
| `max_tokens` | 2,048, held | the re-probe's real pages complete within it |
| `vision.max_pixels` | 2,580,480 (`38df198`) | Gemma 4's processor rule; oversized scans no longer sent whole |
| concurrency | 4 | the row's cap |
| `response_header_timeout` | 600 s, cmd's starting value for vision's endpoint (`3948bce`); the others 180 s | a non-streaming reply's header waits for the whole reply; the row runs ~90–110 ms a token at 4 in flight, and run 3 saw 204 s for 1,816 tokens |
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

## The fresh run (runs 3–8)

Output in `~/runs/docinfer/acceptance-2`, which no earlier run's cached vision reaches; each run
resumes from the directory's cache. Config `~/runs/docinfer/config/config.json`: vision 4, audio
32 (chat mode), embedding 4, vision's `response_header_timeout` 600 s from run 4 on, every other
setting at cmd's starting value. Builds in `~/runs/docinfer/BUILD.txt`; logs and exits
`~/runs/docinfer/logs/run-3.log` … `run-8.log` (`.exit` beside each but run 4's). All on
2026-11-25.

| Run | docinfer + go-ai | Input | Start | Exit | Time | What it did |
|---|---|---|---|---|---|---|
| 3 | `d6c1315` + `e375fbd` | `final` | 14:58:52 | 1 | 3,524 s | stopped at 59 min: one vision request outlasted cmd's 180 s response header timeout on all three attempts |
| 4 | the same, config 600 s | `final` | 16:31:58 | stopped | ~750 s, to the log's last write | stopped by the session to remove the generated OneNote images (escalation 7); its in-flight window recorded `context canceled`, nothing failed held |
| 5 | `6589b4b` + `6745310` | `final-no-synthetic` | 16:45:14 | 0 | 10,488 s | finished the images; transcription request 2 |
| 6 | `65c99d9` + `35208a1` | `final-no-synthetic` | 19:50:57 | 0 | 743 s | transcription request 3 (escalation 8): 1,208 windows re-sent, every image held; Behavior 14 is judged here |
| 7 | the same | the same | 20:03:50 | 0 | 2.1 s | the rerun, Behavior 15 |
| 8 | the same | the same | 20:04:18 | 0 | 2.1 s | a second rerun, measured on the rows' counters |

The input from run 5 on is `~/runs/docproc/final-no-synthetic` (its `README.txt`): `final`'s run
record less the 1,998 image output events of `onenote/large/Large Desktop.one` and
`Large OneDrive.one`, every other line byte for byte; `content/`, `index/`, and the report are
symlinks to `final`'s, which stays untouched. Images go from 4,718 lines and 3,471 distinct to
2,720 and 2,573 (898 distinct removed), outputs from 11,188 to 9,190; the two files' text is still
chunked and embedded.

| Finding | Evidence | Fix |
|---|---|---|
| A non-streaming vision reply's header waits for the whole reply, past cmd's 180 s | run 3's exit 1 (`run-3.log`); 204 s seen for 1,816 tokens at ~90–110 ms a token | the acceptance config's 600 s for run 4 on; `3948bce` (cmd starts vision's endpoint at 600 s, the others at 180 s) |
| onenote.rs's generated timing sections are pseudo-word pages, not realistic content, and noise in the measurements | 1,998 of 4,718 image events; 1,158 of run 1's vision loops | escalation 7: their images removed from the input (`final-no-synthetic`); the baselines below recomputed without them |
| Run 6's 3 vision images incomplete | `old_scans-70` (`repetition`) and `old_scans-71` (`max_output_tokens`), the 8,000-pixel newspaper scans; olmocr-bench `tables-fa18a15c…_pg2_pg1.pdf` (`max_output_tokens`), a long real crop-weather table cut at 2,048 tokens | none: held text, not failures; a page past `max_tokens` 2,048 is a known limit ([Decisions](#decisions)) |
| A transcription window of normal speech looped to the 512 cap at temperature 0, was held incomplete, and joined empty | `tos-clip-180s-480p-subs.mkv` window 5 (27 s), run 4; sent alone, the row answers in 39 tokens | escalation 8: go-ai `35208a1` (`IncompleteTranscriptError.Text`, chat mode), docinfer `d6236ad` (the capped window holds its text trimmed of the loop, joins it, and stays incomplete; transcription request 3) |

Run 6's counters (`run-6.log`):

| Capability | Items | Outcomes | Requests |
|---|---|---|---|
| Vision | 2,720 images | 2,715 described, 3 incomplete, 2 refused; all held | 0 |
| Transcription | 1,212 windows | 1,211 transcribed, 1 incomplete, 0 unplaced; 50 transcripts, 1,032 seams matched, 130 unmatched | 1,208 |
| Embeddings | 4,327 texts, 592 metadata | 31,218 embedded, 0 rejected; 31,082 held | 21 |

### Behavior 14, on run 6

Vectors and runes from `python3 -I ~/runs/docinfer/tools/check-run.py` over run 6's directory,
`acceptance-2/runs/01a21555-8d4e-78f2-8ecc-b3ed304c70e8`.

| Criterion | Target | Run 6 |
|---|---|---|
| Exit | 0 | 0 |
| Refusals and failures | each classified | 3, all classified: `msg_22.eml`'s 2 attachments refused as unloadable (400, `Failed to load image`), a legitimate refusal; `tos-clip-180s-480p-subs.mkv` window 5 incomplete, reason `repetition`, its trimmed text kept (the held file `acceptance-2/results/transcription/76/76b27e62….json` carries `"incomplete":"repetition"`) |
| Vectors | 1,024 dimensions, unit norm | 31,218, all 1,024; worst \|norm − 1\| 9.07e-08 |
| Report | renders | `acceptance-2/runs/01a21555-8d4e-78f2-8ecc-b3ed304c70e8/report/index.html` |
| Covered runes | traced against the spike's 41,299,926 | 35,620,708 over chunk lines, 31,229,662 distinct; traced below |
| Images | traced against the spike's 3,470 | 2,573 distinct (2,720 lines): the spike's 3,470 − the 898 removed is 2,572, and 1 more, as run 1 read 3,471 to the spike's 3,470 |
| Windows | traced against the spike's 1,254; each transcribed or counted | 1,212 lines, 1,208 distinct, as in run 1, against the spike's 1,259 lines and 1,254 distinct (`counts-1.md`); traced by input [below](#windows-by-input); 1,211 transcribed, 1 incomplete, 0 unplaced |
| Time | recorded, not gated | run 3 3,524 s (exit 1 at the 180 s header timeout), run 4 ~12 min (stopped), run 5 10,488 s, run 6 743 s; runs 3–5, which described the images, 14,761 s (4.1 h) together, with vision for the generated images paid in runs 3 and 4 |

### Windows by input

Audio window output lines in the spike's docproc record,
`~/docinfer-finding/docproc/runs/01a1b82d-54aa-73a2-81db-67577e9cb5da/record.jsonl` (1,259), and
`final`'s (1,212); every other input has the same count in both.

| Input | Spike | `final` |
|---|---|---|
| `audio/long/librivox-stella-maberly.m4b` | 645 | 606 |
| `video/ami-es2002d/ami-es2002d-mix-headset.wav` | 107 | 102 |
| `video/prelinger-duck-and-cover/duck-and-cover-theora.ogv` | 23 | 21 |
| `audio/long/ami-es2002a-mix-headset.wav` | 50 | 49 |
| `onenote/onenote-rs/New Section Group/Open Notebook.onetoc2` | 1 | 0 |
| `audio/long/earnings21-4330115.mp3` | 96 | 97 |
| **all inputs** | **1,259** | **1,212** |

docproc's audio stage changed between the two runs (stage config `a4aae033…` to `98dbaf4f…`).
`final`'s window plan records a 30 s `limit` and a different silence detection: for the
audiobook, 3,378 silences become 4,365 and the median window 27.7 s becomes 28.4 s, so fewer
windows. Why docproc changed its windowing is not recorded in either run.

### Covered runes by kind

Runes over chunk lines. The spike's are summed by `source.kind` over its chunk record,
`~/docinfer-finding/docinfer-full/runs/01a1b954-a1ac-7147-bcf2-0778352063ef/chunks.jsonl`, the
run `~/experiments/spike-ai-pipeline/docs/findings/docinfer-results/counts-1.md` names (its lines
carry no `digest`, so lines only); its lines match the findings' "Texts and chunks by kind" table
(`docs/findings/docinfer.md`). Run 1 read the spike's corpus, the generated images included.

| Kind | Spike lines | Spike runes | Run 1 lines | Run 1 runes | Run 6 lines | Run 6 runes |
|---|---|---|---|---|---|---|
| metadata | 592 | 194,280 | 592 | 67,501 | 592 | 67,501 |
| output | 18,485 | 30,597,410 | 25,422 | 30,708,770 | 25,436 | 30,701,934 |
| transcript | 265 | 411,391 | 8 | 7,948 | 302 | 418,032 |
| vision | 9,595 | 10,096,845 | 8,493 | 6,021,129 | 4,888 | 4,433,241 |
| **total** | **28,937** | **41,299,926** | **34,515** | **36,805,348** | **31,218** | **35,620,708** |

| Kind | Spike to run 1 | Run 1 to run 6 |
|---|---|---|
| metadata | −126,779: run 1's metadata text is shorter; for `README.md` the spike's names the file, path argument, path, size, mode, modified and created times, route, route reason, and detected MIME type, run 1's only the file name, size, route, and route reason, by design: `MetadataText` (`run.go`) keeps the part's name, size, and content type and leaves out its path and times, which differ between machines and runs | unchanged |
| output | +111,360: chunking to 480 tokens by the row's tokenizer, where the spike cut 2,000-rune chunks, adds lines and raises covered runes slightly | the chunks e5 rejected in run 1, re-split under `chunk_runes` (`f121620`): +14 lines, −6,836 runes; embedding rejections 8 to 0 |
| transcript | −403,443: run 1's grouping defect, 24 of 1,212 windows transcribed ([Run 1](#run-1-and-its-rerun)) | transcripts complete (`137a724`, `6d0c74f`), window 5's trimmed text kept (`d6236ad`) |
| vision | −4,075,716: a different model, sampling, and image budget, and run 1's trimmed loops | the generated images' text removed (4,322 lines, 2,161,782 runes); the rest, 4,171 lines and 3,859,347 runes, rises to 4,888 and 4,433,241 at 1,120 image tokens and the card sampling |
| **total** | **−4,494,578** | **−1,184,640** |

### Behavior 15, on runs 7 and 8

| Criterion | Target | Result |
|---|---|---|
| Rerun's requests | no inference or tokenize request, the model listing aside | run 7: 0 in every capability (`run-7.log`); across run 8, `framework-2:8003`'s `http_requests_total` for `/tokenize` and `/v1/embeddings`, and the audio and vision rows' `vllm:request_success_total`, unchanged (`~/runs/docinfer/logs/run-8-counters.txt`, before and after) |
| Rerun's chunk lines | identical, `input` included | identical as a set, byte for byte, `input` included: runs 6, 7, and 8's sorted lines all hash to sha256 `71be448e…` (`cmp <(sort A) <(sort B)`); the file order differs, as inputs finish concurrently and an input's metadata and output lines interleave differently |
| Time | recorded, not gated | run 7 2.1 s, run 8 2.1 s |
