#!/usr/bin/env bash
# transcribe-eval/run.sh runs one of the transcription measurements docs/acceptance.md reports,
# building the transcribe-eval command from this checkout into <out>/bin, appending each run's
# result line to <out>/results.jsonl and its texts to <out>/texts/<label>/, logging to
# <out>/eval.log, and ending with compare.py's comparison of the measurement's two labels.
#
# Usage: run.sh MEASUREMENT --out DIR --docproc DIR [--run ID] [--gateway URL | --base-url URL]
#               [ARG...]
#
#   eval             max_tokens 512 then none, at the row's default sampling: labels max512, max0
#   t0               max_tokens 512 at temperature 0, twice: labels t0-a, t0-b
#
#   --out DIR        the measurement directory: results.jsonl, eval.log, texts/, and bin/
#   --docproc DIR    the docproc run directory the audio inputs are drawn from
#   --run ID         the docproc run's id; the latest run that ended when not given
#   --gateway URL    the gateway's root, audio at <URL>/audio/v1; default http://gb10-2:3000
#   --base-url URL   the audio endpoint's API root, in place of the gateway's alias
#   ARG...           passed to both runs, such as -n 20 -seed 2 -max-windows 400
#
# Each request carries AI_AUDIO_TOKEN, or else AI_GATEWAY_TOKEN, from the environment or from
# this repository's fnox.toml through fnox. <measurement>.done holds the measurement's end.
# See acceptance/README.md
set -u
. "$(dirname "$0")/../lib.sh"

usage() {
	sed -n '/^# Usage:/,/^# Each request/{/^# Each request/d;s/^# \{0,1\}//;p}' "$0"
}

case ${1:-} in
-h | --help) usage; exit 0 ;;
eval | t0) measurement=$1; shift ;;
*) usage >&2; die "unknown measurement: ${1:-none}" ;;
esac

out='' docproc='' run='' gateway=http://gb10-2:3000 base=''
rest=()
while [ $# -gt 0 ]; do
	case $1 in
	-h | --help) usage; exit 0 ;;
	--out) need_value "$@"; out=$2; shift 2 ;;
	--docproc) need_value "$@"; docproc=$2; shift 2 ;;
	--run) need_value "$@"; run=$2; shift 2 ;;
	--gateway) need_value "$@"; gateway=${2%/}; shift 2 ;;
	--base-url) need_value "$@"; base=$2; shift 2 ;;
	*) rest+=("$1"); shift ;;
	esac
done
[ -n "$out" ] || { usage >&2; die "--out is required"; }
[ -n "$docproc" ] || { usage >&2; die "--docproc is required"; }

mkdir -p "$out"
out=$(cd "$out" && pwd -P)
here=$(cd "$(dirname "$0")" && pwd -P)
build_tool transcribe-eval "$out/bin/transcribe-eval" || die "building transcribe-eval failed"
export AI_AUDIO_BASE_URL=${base:-$gateway/audio/v1}
if [ -z "${AI_AUDIO_TOKEN:-}" ]; then
	AI_AUDIO_TOKEN=$(gateway_token) || die "no gateway key: set AI_AUDIO_TOKEN or AI_GATEWAY_TOKEN, or install fnox's age key"
	export AI_AUDIO_TOKEN
fi

args=(-rundir "$docproc" -out "$out/results.jsonl" -texts "$out/texts")
[ -z "$run" ] || args+=(-run "$run")
rm -f "$out/$measurement.done"

one() {
	local label=$1
	shift
	echo "== $label $(date -u +%FT%TZ)"
	"$out/bin/transcribe-eval" "${args[@]}" -label "$label" "$@" "${rest[@]}"
	echo "== $label exit $? $(date -u +%FT%TZ)"
}

{
	echo "== $measurement start $(date -u +%FT%TZ) args: ${rest[*]}"
	case $measurement in
	eval)
		one max512 -max-tokens 512
		one max0 -max-tokens 0
		python3 -I "$here/compare.py" "$out/results.jsonl" "$out/texts" max512 max0
		;;
	t0)
		one t0-a -max-tokens 512 -native '{"temperature":0}'
		one t0-b -max-tokens 512 -native '{"temperature":0}'
		python3 -I "$here/compare.py" "$out/results.jsonl" "$out/texts" t0-a t0-b
		;;
	esac
	echo "== end $(date -u +%FT%TZ)"
} >>"$out/eval.log" 2>&1
date -u +%FT%TZ >"$out/$measurement.done"
