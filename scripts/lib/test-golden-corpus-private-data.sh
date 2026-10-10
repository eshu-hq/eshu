#!/usr/bin/env bash
# Sourced by test-verify-golden-corpus-gate.sh after the helper inventory is built.
# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
# The 12-digit arm catches a bare cloud account id. The lock cases used to age
# a guard past its budget with `touch -h -t <stamp>`; age now comes from a
# birth epoch embedded in the guard's own payload (pid:epoch), computed at
# runtime via `date +%s`, so no `touch -h -t` stamp (and no exclusion for one)
# remains in the scanned files.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(?<![0-9])[0-9]{12}(?![0-9])|/Users/|/home/[a-z]'
# Scan every scripts/lib/golden-corpus-*.sh lib via the glob-derived
# golden_corpus_libs array (built above, and already asserted non-empty), not a
# hand-maintained list of names: a hand-maintained list silently stopped
# covering golden-corpus-maintenance-drains.sh even though it now owns the live
# gate orchestration this PR moved out of the main script, so a future secret
# added there would pass this scan. Deriving the list from the same glob the
# orchestrator's own lib-count assertion uses means an added lib is covered
# automatically, with no new path entry to keep in sync. The orchestrator
# script itself and live-gate-lock.sh are not golden-corpus-*.sh named, so they
# still need an explicit entry.
for scanned in "${script}" \
	"${repo_root}/scripts/lib/live-gate-lock.sh" \
	"${golden_corpus_libs[@]}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${scanned}"; then
		fail "$(basename "${scanned}") looks like it contains private data"
	fi
done
