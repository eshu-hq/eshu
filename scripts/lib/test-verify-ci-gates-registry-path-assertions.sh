#!/usr/bin/env bash
# Path-line assertions shared by scripts/test-verify-ci-gates-registry.sh.
#
# Extracted to keep that script under the repository's 500-line file cap. It is
# sourced, not executed, and uses the caller's `fail`.

# Anchored to the whole line: an unanchored substring match also accepts
# `# - "path"`, so commenting a filter line out - the most common way one gets
# "temporarily" disabled - would keep this guard green.
#
# No --quiet: it exits on the first match, and on a haystack larger than the
# pipe buffer printf is still writing, so it takes SIGPIPE and
# `set -o pipefail` turns the pipeline into 141. The guard then reports the
# line as MISSING precisely because it was PRESENT -- a match that arrives
# early enough to close the pipe. Draining the input costs nothing here and
# keeps the exit code meaning what it says. The same rule applies to every
# `printf ... | rg` guard in this test family: do not reintroduce --quiet.
require_path_line() {
	local haystack="$1" needle="$2" message="$3"
	printf '%s\n' "${haystack}" |
		rg --fixed-strings --line-regexp -- "      - \"${needle}\"" >/dev/null ||
		fail "${message}: expected the exact line \`      - \"${needle}\"\` (six spaces, double-quoted)"
}
