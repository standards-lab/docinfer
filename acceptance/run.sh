#!/usr/bin/env bash
# run.sh runs docinfer's acceptance run: `docinfer run --report` over a docproc run directory,
# through the model gateway, timed with bash's `time`, under a scrubbed environment so only the
# configuration it writes and the gateway key reach the command. Run it again with the same --work
# for the rerun, which resumes from the output directory's cache.
#
# Usage: run.sh --work DIR --docproc DIR [--run ID] [--gateway URL] [--vision N] [--audio N]
#               [--embedding N] [--bin PATH]
#
#   --work DIR       the working directory: config/, bin/, logs/, BUILD.txt, and out/, docinfer's
#                    output directory
#   --docproc DIR    the docproc run directory docinfer reads
#   --run ID         the docproc run's id; the latest run that ended when not given
#   --gateway URL    the gateway's root, each capability at <URL>/<alias>/v1; default
#                    http://gb10-2:3000
#   --vision N       vision requests in flight; default 4
#   --audio N        transcription requests in flight; default 32
#   --embedding N    embedding requests in flight; default 4
#   --bin PATH       the docinfer binary to run; by default cmd/docinfer is built from this
#                    checkout into <work>/bin/docinfer, and its commit appended to BUILD.txt
#
# The key is AI_GATEWAY_TOKEN, from the environment or from this repository's fnox.toml through
# fnox, passed to the command as each endpoint's DOCINFER_ENDPOINTS_<NAME>_TOKEN and never
# written to disk. Each run claims the next number N and writes logs/run-N.log, with the command,
# the configuration, the command's output, and its time, and logs/run-N.exit. The exit status is
# the command's.
# See acceptance/README.md
set -u
. "$(dirname "$0")/lib.sh"

usage() {
	sed -n '/^# Usage:/,/^# The key/{/^# The key/d;s/^# \{0,1\}//;p}' "$0"
}

work='' docproc='' run='' gateway=http://gb10-2:3000 vision=4 audio=32 embedding=4 bin=''
while [ $# -gt 0 ]; do
	case $1 in
	-h | --help) usage; exit 0 ;;
	--work) need_value "$@"; work=$2; shift 2 ;;
	--docproc) need_value "$@"; docproc=$2; shift 2 ;;
	--run) need_value "$@"; run=$2; shift 2 ;;
	--gateway) need_value "$@"; gateway=${2%/}; shift 2 ;;
	--vision) need_value "$@"; vision=$2; shift 2 ;;
	--audio) need_value "$@"; audio=$2; shift 2 ;;
	--embedding) need_value "$@"; embedding=$2; shift 2 ;;
	--bin) need_value "$@"; bin=$2; shift 2 ;;
	*) usage >&2; die "unknown argument: $1" ;;
	esac
done
[ -n "$work" ] || { usage >&2; die "--work is required"; }
[ -n "$docproc" ] || { usage >&2; die "--docproc is required"; }
[ -d "$docproc" ] || die "no docproc run directory at $docproc"

mkdir -p "$work/config" "$work/logs"
work=$(cd "$work" && pwd -P)
docproc=$(cd "$docproc" && pwd -P)
logs=$work/logs

n=1
while [ -e "$logs/run-$n.log" ] || [ -e "$logs/run-$n.exit" ]; do n=$((n + 1)); done
set -o noclobber
until { : >"$logs/run-$n.log"; } 2>/dev/null; do n=$((n + 1)); done
set +o noclobber
log=$logs/run-$n.log

if [ -z "$bin" ]; then
	bin=$work/bin/docinfer
	repo=$(acceptance_dir)/..
	go -C "$repo/cmd/docinfer" build -o "$bin" . || die "building cmd/docinfer failed"
	printf 'run %s: docinfer %s%s, %s\n' "$n" "$(git -C "$repo" rev-parse --short HEAD)" \
		"$(git -C "$repo" diff --quiet HEAD -- . || echo ' (modified)')" "$(go version)" >>"$work/BUILD.txt"
fi
[ -x "$bin" ] || die "no docinfer binary at $bin"

token=$(gateway_token) || die "no gateway key: set AI_GATEWAY_TOKEN or install fnox's age key"
[ -n "$token" ] || die "the gateway key is empty"

jq -n --arg g "$gateway" --argjson v "$vision" --argjson a "$audio" --argjson e "$embedding" '{
	endpoints: {
		vision: {base_url: "\($g)/vision/v1", format: "completions"},
		audio: {base_url: "\($g)/audio/v1", format: "completions"},
		embedding: {base_url: "\($g)/embedding/v1", format: "completions"}
	},
	vision: {endpoint: "vision", model: "vision", concurrency: $v},
	transcription: {endpoint: "audio", model: "audio", mode: "chat", concurrency: $a},
	embeddings: {
		endpoint: "embedding", model: "embedding", dimensions: 1024, concurrency: $e,
		input_types: {document: {prefix: "passage: "}, query: {prefix: "query: "}},
		tokenizer: "embedding"
	}
}' >"$work/config/config.json" || die "writing the configuration failed"

args=(--config "$work/config" run "$docproc" --out "$work/out" --report)
[ -z "$run" ] || args+=(--run "$run")

start=$(date --iso-8601=seconds)
{
	echo "# run $n start $start"
	echo "# $bin ${args[*]}"
	sed 's/^/# config: /' "$work/config/config.json"
} >>"$log"

TIMEFORMAT='# time: real %3Rs user %3Us sys %3Ss cpu %P%%'
{
	time env -i PATH=/usr/bin:/bin HOME="$HOME" LANG=C.UTF-8 \
		DOCINFER_ENDPOINTS_VISION_TOKEN="$token" \
		DOCINFER_ENDPOINTS_AUDIO_TOKEN="$token" \
		DOCINFER_ENDPOINTS_EMBEDDING_TOKEN="$token" \
		"$bin" "${args[@]}"
	echo "$?" >"$logs/.run-$n.code"
} >>"$log" 2>&1

code=$(cat "$logs/.run-$n.code" 2>/dev/null || echo 255)
rm -f "$logs/.run-$n.code"
end=$(date --iso-8601=seconds)
echo "# run $n end $end exit $code" >>"$log"
printf 'exit=%s\nstart=%s\nend=%s\n' "$code" "$start" "$end" >"$logs/run-$n.exit"
exit "$code"
