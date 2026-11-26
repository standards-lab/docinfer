#!/usr/bin/env bash
# lib.sh holds the functions the acceptance scripts share; each script sources it. It runs nothing
# itself.
# See acceptance/README.md

acceptance_dir() {
	cd "${BASH_SOURCE[0]%/*}" && pwd -P
}

die() {
	echo "$(basename "$0"): $*" >&2
	exit 2
}

gateway_token() {
	if [ -n "${AI_GATEWAY_TOKEN:-}" ]; then
		printf '%s' "$AI_GATEWAY_TOKEN"
		return
	fi
	(cd "$(acceptance_dir)/.." && fnox get AI_GATEWAY_TOKEN)
}

build_tool() {
	local name=$1 dest=$2
	mkdir -p "$(dirname "$dest")"
	go -C "$(acceptance_dir)" build -o "$dest" "./$name"
}

need_value() {
	[ $# -ge 2 ] && [ -n "$2" ] || die "$1 needs a value"
}
