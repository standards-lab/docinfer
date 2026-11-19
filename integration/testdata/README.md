# integration testdata

The media docinfer's integration tier sends through `Pipeline.Run` to the vision and audio
endpoints. Both files are go-ai's, copied unchanged from its `integration/testdata` at tag
`v0.4.0` (https://github.com/standards-lab/go-ai), the release docinfer requires, under the same
Apache-2.0 license as this repository. go-ai's `integration/testdata/README.md` tells where they
were made, in spike-harness-driver, and how:

- `shapes.png`: a red circle, a blue square, and a green triangle on white, 480 pixels square.
  `TestVision` runs it as an image output, wanting a described, non-empty text.
- `phrase.wav`: one speaker saying "The access code is seven four two nine.", 16 kHz mono 16-bit
  PCM, 6.05 seconds. `TestTranscription` runs it as an audio window, the one window of its
  group, in route and chat mode, wanting a transcribed, non-empty transcript.

With go-ai checked out beside this repository, this prints nothing when the copies match their
source:

```sh
for f in shapes.png phrase.wav; do
  git -C ../go-ai show v0.4.0:integration/testdata/$f | cmp - integration/testdata/$f
done
```
