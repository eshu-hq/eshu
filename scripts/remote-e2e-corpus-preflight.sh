#!/bin/sh
set -eu

root=${ESHU_REMOTE_E2E_MOUNTED_ROOT:-/fixtures}

normalize_host_root() {
	value=$1
	while [ "${value#./}" != "$value" ]; do value=${value#./}; done
	while [ "${value%/}" != "$value" ]; do value=${value%/}; done
	printf '%s' "$value"
}

require_non_negative_integer() {
	name=$1
	value=$2
	[ -n "$value" ] || return 0
	case "$value" in
		*[!0123456789]*)
			echo "remote e2e corpus preflight failed: $name must be a non-negative integer, got $value" >&2
			exit 1
			;;
	esac
}

[ -d "$root" ] || {
	echo "remote e2e corpus preflight failed: mounted root $root is missing" >&2
	exit 1
}

host_root=${ESHU_FILESYSTEM_HOST_ROOT:-./tests/fixtures/ecosystems}
mode=${ESHU_REMOTE_E2E_CORPUS_MODE:-smoke}
min_count=${ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT:-0}
max_count=${ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT:-}
expected_count=${ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT:-}

require_non_negative_integer ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT "$min_count"
require_non_negative_integer ESHU_REMOTE_E2E_MAX_REPOSITORY_COUNT "$max_count"
require_non_negative_integer ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT "$expected_count"

case "$mode" in
	smoke | representative | full) ;;
	*)
		echo "remote e2e corpus preflight failed: ESHU_REMOTE_E2E_CORPUS_MODE must be one of smoke, representative, full" >&2
		exit 1
		;;
esac

candidate_count=0
git_count=0
for entry in "$root"/* "$root"/.[!.]* "$root"/..?*; do
	[ -d "$entry" ] || continue
	candidate_count=$((candidate_count + 1))
	[ -e "$entry/.git" ] && git_count=$((git_count + 1))
done

echo "remote e2e corpus preflight: host_root=$host_root mounted_root=$root mode=$mode candidate_repository_roots=$candidate_count git_repository_roots=$git_count"

normalized_host_root=$(normalize_host_root "$host_root")
effective_min_count=$min_count
effective_max_count=$max_count
if [ "$mode" = "representative" ]; then
	[ "$effective_min_count" != "0" ] || effective_min_count=20
	[ -n "$effective_max_count" ] || effective_max_count=50
	[ "$git_count" -gt 0 ] || {
		echo "representative-corpus mode requires at least one Git repository root under $root" >&2
		exit 1
	}
fi

if [ "$mode" = "full" ]; then
	case "$normalized_host_root" in
		tests/fixtures/ecosystems | */tests/fixtures/ecosystems)
			echo "full-corpus mode requires ESHU_FILESYSTEM_HOST_ROOT to point at the remote corpus, not the default fixtures" >&2
			exit 1
			;;
	esac
	[ "$min_count" != "0" ] || [ -n "$expected_count" ] || {
		echo "full-corpus mode requires ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT or ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT" >&2
		exit 1
	}
	[ "$git_count" -gt 0 ] || {
		echo "full-corpus mode requires at least one Git repository root under $root" >&2
		exit 1
	}
fi

if [ "$effective_min_count" != "0" ] && [ "$candidate_count" -lt "$effective_min_count" ]; then
	if [ "$mode" = "representative" ]; then
		echo "representative-corpus mode requires at least $effective_min_count candidate repository roots; got $candidate_count" >&2
	else
		echo "remote e2e corpus preflight failed: candidate_repository_roots=$candidate_count below minimum $effective_min_count" >&2
	fi
	exit 1
fi

if [ -n "$effective_max_count" ] && [ "$candidate_count" -gt "$effective_max_count" ]; then
	if [ "$mode" = "representative" ]; then
		echo "representative-corpus mode allows at most $effective_max_count candidate repository roots; got $candidate_count" >&2
	else
		echo "remote e2e corpus preflight failed: candidate_repository_roots=$candidate_count above maximum $effective_max_count" >&2
	fi
	exit 1
fi

if [ -n "$expected_count" ] && [ "$candidate_count" -ne "$expected_count" ]; then
	echo "remote e2e corpus preflight failed: candidate_repository_roots=$candidate_count expected $expected_count" >&2
	exit 1
fi
