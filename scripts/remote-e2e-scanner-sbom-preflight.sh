#!/bin/sh
set -eu

root=${ESHU_SCANNER_WORKER_SBOM_MOUNTED_ROOT:-/scanner-fixtures}
min_candidates=${ESHU_SCANNER_WORKER_SBOM_MIN_CANDIDATES:-1}
mounted_root_configured=false
host_root_configured=false
[ -n "${ESHU_SCANNER_WORKER_SBOM_MOUNTED_ROOT:-}" ] && mounted_root_configured=true
[ -n "${ESHU_SCANNER_WORKER_SBOM_HOST_ROOT:-}" ] && host_root_configured=true

require_non_negative_integer() {
	name=$1
	value=$2
	case "$value" in
		"" | *[!0123456789]*)
			echo "scanner SBOM preflight failed: $name must be a non-negative integer" >&2
			exit 1
			;;
	esac
}

is_supported_manifest_name() {
	case "$1" in
		package-lock.json | npm-shrinkwrap.json | go.mod | Cargo.lock | composer.lock | packages.lock.json | Pipfile.lock | poetry.lock | Gemfile.lock | gradle.lockfile)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

should_skip_repository_dir() {
	case "$1" in
		.git | .hg | .svn | .terraform | node_modules | vendor)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

walk_manifest_candidates() {
	dir=$1
	for entry in "$dir"/* "$dir"/.[!.]* "$dir"/..?*; do
		[ -e "$entry" ] || continue
		[ ! -L "$entry" ] || continue
		name=${entry##*/}
		if [ -d "$entry" ]; then
			should_skip_repository_dir "$name" && continue
			walk_manifest_candidates "$entry"
			continue
		fi
		[ -f "$entry" ] || continue
		is_supported_manifest_name "$name" || continue
		count=$((count + 1))
	done
}

count_manifest_candidates() {
	count=0
	walk_manifest_candidates "$1"
	printf '%s' "$count"
}

require_non_negative_integer ESHU_SCANNER_WORKER_SBOM_MIN_CANDIDATES "$min_candidates"

[ -d "$root" ] || {
	echo "scanner SBOM preflight failed: scanner SBOM root is not mounted" >&2
	exit 1
}

supported_manifest_candidates=$(count_manifest_candidates "$root")

if [ "$supported_manifest_candidates" -eq 0 ]; then
	echo "scanner SBOM preflight failed: scanner SBOM root has no supported manifests" >&2
	exit 1
fi

if [ "$supported_manifest_candidates" -lt "$min_candidates" ]; then
	echo "scanner SBOM preflight failed: requires at least $min_candidates supported manifest candidates; got $supported_manifest_candidates" >&2
	exit 1
fi

echo "scanner SBOM preflight passed: host_root_configured=$host_root_configured mounted_root_configured=$mounted_root_configured supported_manifest_candidates=$supported_manifest_candidates"
