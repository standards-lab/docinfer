#!/usr/bin/env bash
# sweep/run.sh runs one of the vision measurements docs/acceptance.md reports, building the sweep
# command from this checkout into <out>/bin and appending each run's result line, its log, and
# its texts to the measurement directory.
#
# Usage: run.sh MEASUREMENT --out DIR --docproc DIR [--run ID] [--gateway URL | --base-url URL]
#               [--concurrency N] [--only FILE] [ARG...]
#
#   sweep C...       the concurrency sweep: 160 images drawn with seed 1, temperature 0, at each
#                    concurrency C given; results.jsonl, sweep.log
#   native L J...    the native-sampling probe: seed 2's 32 images, max_tokens 2,048, for each
#                    label L and native JSON object J given; native-probe.jsonl, native.log
#   reprobe          the re-probe: seed 2's 32 images, then, with --only, the images the file
#                    lists, each at temperature 0 and at the row's default sampling;
#                    reprobe.jsonl, reprobe.log
#   confirm          the confirmation at Gemma 4's card sampling and max_pixels 2,580,480: the
#                    images --only lists, then seed 2's 32 images; confirm.jsonl, confirm.log
#
#   --out DIR        the measurement directory: results, logs, texts-<label>/, and bin/
#   --docproc DIR    the docproc run directory the images are drawn from
#   --run ID         the docproc run's id; the latest run that ended when not given
#   --gateway URL    the gateway's root, vision at <URL>/vision/v1; default http://gb10-2:3000
#   --base-url URL   the vision endpoint's API root, in place of the gateway's alias
#   --concurrency N  requests in flight for native, reprobe, and confirm; default 4
#   --only FILE      a file of image SHA-256s, one a line
#
# Each request carries AI_VISION_TOKEN, or else AI_GATEWAY_TOKEN, from the environment or from
# this repository's fnox.toml through fnox. Each run's start and exit go to the log, and
# <measurement>.done holds the measurement's end.
# See acceptance/README.md
set -u
. "$(dirname "$0")/../lib.sh"

usage() {
	sed -n '/^# Usage:/,/^# Each request/{/^# Each request/d;s/^# \{0,1\}//;p}' "$0"
}

case ${1:-} in
-h | --help) usage; exit 0 ;;
sweep | native | reprobe | confirm) measurement=$1; shift ;;
*) usage >&2; die "unknown measurement: ${1:-none}" ;;
esac

out='' docproc='' run='' gateway=http://gb10-2:3000 base='' concurrency=4 only=''
rest=()
while [ $# -gt 0 ]; do
	case $1 in
	-h | --help) usage; exit 0 ;;
	--out) need_value "$@"; out=$2; shift 2 ;;
	--docproc) need_value "$@"; docproc=$2; shift 2 ;;
	--run) need_value "$@"; run=$2; shift 2 ;;
	--gateway) need_value "$@"; gateway=${2%/}; shift 2 ;;
	--base-url) need_value "$@"; base=$2; shift 2 ;;
	--concurrency) need_value "$@"; concurrency=$2; shift 2 ;;
	--only) need_value "$@"; only=$(realpath "$2"); shift 2 ;;
	--*) usage >&2; die "unknown option: $1" ;;
	*) rest+=("$1"); shift ;;
	esac
done
[ -n "$out" ] || { usage >&2; die "--out is required"; }
[ -n "$docproc" ] || { usage >&2; die "--docproc is required"; }
case $measurement in
sweep) [ ${#rest[@]} -gt 0 ] || die "sweep needs one or more concurrencies" ;;
native) [ ${#rest[@]} -gt 0 ] && [ $((${#rest[@]} % 2)) -eq 0 ] || die "native needs label and JSON pairs" ;;
confirm) [ -n "$only" ] || die "confirm needs --only" ;;
*) [ ${#rest[@]} -eq 0 ] || die "$measurement takes no arguments" ;;
esac

mkdir -p "$out"
out=$(cd "$out" && pwd -P)
build_tool sweep "$out/bin/sweep" || die "building sweep failed"
export AI_VISION_BASE_URL=${base:-$gateway/vision/v1}
if [ -z "${AI_VISION_TOKEN:-}" ]; then
	AI_VISION_TOKEN=$(gateway_token) || die "no gateway key: set AI_VISION_TOKEN or AI_GATEWAY_TOKEN, or install fnox's age key"
	export AI_VISION_TOKEN
fi

common=(-rundir "$docproc")
[ -z "$run" ] || common+=(-run "$run")
log=$out/$measurement.log
rm -f "$out/$measurement.done"

one() {
	local label=$1 results=$2
	shift 2
	mkdir -p "$out/texts-$label"
	{
		echo "$(date -Is) $label start"
		"$out/bin/sweep" "${common[@]}" -label "$label" -texts "$out/texts-$label" -out "$out/$results" "$@"
		echo "$(date -Is) $label exit $?"
	} >>"$log" 2>&1
}

card='{"temperature":1.0,"top_p":0.95,"top_k":64}'
case $measurement in
sweep)
	for c in "${rest[@]}"; do
		one "sweep-c$c" results.jsonl -concurrency "$c" -n 160 -seed 1 -native '{"temperature":0}'
	done
	;;
native)
	set -- "${rest[@]}"
	while [ $# -ge 2 ]; do
		one "$1" native-probe.jsonl -concurrency "$concurrency" -n 32 -seed 2 -max-tokens 2048 -native "$2"
		shift 2
	done
	;;
reprobe)
	one sample-t0 reprobe.jsonl -concurrency "$concurrency" -n 32 -seed 2 -native '{"temperature":0}'
	one sample-default reprobe.jsonl -concurrency "$concurrency" -n 32 -seed 2
	if [ -n "$only" ]; then
		one real-t0 reprobe.jsonl -concurrency "$concurrency" -n 1000000 -seed 1 -only "$only" -native '{"temperature":0}'
		one real-default reprobe.jsonl -concurrency "$concurrency" -n 1000000 -seed 1 -only "$only"
	fi
	;;
confirm)
	one confirm-pages confirm.jsonl -concurrency "$concurrency" -max-pixels 2580480 -native "$card" -n 1000000 -seed 1 -only "$only"
	one confirm-sample confirm.jsonl -concurrency "$concurrency" -max-pixels 2580480 -native "$card" -n 32 -seed 2
	;;
esac
date -Is >"$out/$measurement.done"
