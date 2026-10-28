#!/usr/bin/env bash
# Fake `git` for test-collector-cutover-rollback.sh. It models only the one read
# the activations/GitOps rollback mode performs: `git -C <repo> show <sha>`,
# answered from the removal_diff file in $FAKE_STATE. Marker git_fail makes the
# read fail; a repo path that is not a directory fails like real git.
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
printf 'git %s\n' "$*" >>"${state}/calls.log"

repo=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	-C)
		repo="$2"
		shift 2
		;;
	*)
		break
		;;
	esac
done

if [[ "${1:-}" != "show" || $# -ne 2 ]]; then
	printf 'fake git: unsupported command %s\n' "$*" >&2
	exit 64
fi
if [[ -e "${state}/git_fail" ]]; then
	printf "fatal: ambiguous argument '%s': unknown revision\n" "$2" >&2
	exit 128
fi
if [[ ! -d "${repo}" ]]; then
	printf 'fatal: cannot change to %s: No such file or directory\n' "${repo}" >&2
	exit 128
fi
cat "${state}/removal_diff"
