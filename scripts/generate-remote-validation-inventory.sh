#!/usr/bin/env bash
# Generate the remote-validation slug-to-production-row inventory from the
# capability matrix. Run measurements stay in the human-authored Markdown
# artifacts; this generator never creates or backfills evidence.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
specs="${repo_root}/specs"
root="${repo_root}"

while (($# > 0)); do
	case "$1" in
	--specs)
		specs="${2:-}"
		shift 2
		;;
	--root)
		root="${2:-}"
		shift 2
		;;
	*)
		printf 'generate-remote-validation-inventory: unknown argument: %s\n' "$1" >&2
		exit 1
		;;
	esac
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}" 2>/dev/null || true' EXIT
export GOCACHE="${GOCACHE:-${repo_root}/.gocache}"

(
	cd "${repo_root}/go"
	go run ./cmd/capability-inventory \
		-mode remote-validation \
		-specs "${specs}" \
		-root "${root}" \
		-remote-validation-baseline "${tmp_dir}/remote-validation-baseline.txt" \
		-update
)
