#!/usr/bin/env bash
# Sourced by precommit-go.sh for evidence gates. Uses its repo_root and note/die.
# The sourced repo_root and literal Bash version probe are intentional.
# shellcheck disable=SC2154,SC2016

precommit_evidence_gate() {
	local cmd="$1" base bash4 cand path
	case "${cmd}" in
	perf-evidence)
		# The hot-path performance-evidence gate (test.yml "Verify hot-path
		# evidence"): a change touching storage/cypher, storage/postgres, collector,
		# reducer, query, runtime, workers, queues, etc. needs a tracked evidence
		# marker. The CI gate diffs the PR against its base; reproduce that here by
		# pinning the base to origin/main (its own HEAD~1 fallback would only see the
		# last commit and miss multi-commit branches). Needs bash >= 4 (the gate
		# uses associative arrays); the script's shebang resolves that from PATH.
		git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
		base="origin/main"
		git -C "${repo_root}" rev-parse --verify "${base}" >/dev/null 2>&1 || base="HEAD~1"
		# The gate uses associative arrays (bash >= 4). macOS ships bash 3.2 as
		# /bin/bash, so locate a 4+ interpreter explicitly rather than trusting the
		# script's `env bash` shebang.
		bash4=""
		for cand in bash /opt/homebrew/bin/bash /usr/local/bin/bash; do
			path="$(command -v "${cand}" 2>/dev/null || true)"
			[[ -n "${path}" ]] || continue
			if [[ "$("${path}" -c 'echo "${BASH_VERSINFO[0]}"' 2>/dev/null)" -ge 4 ]]; then
				bash4="${path}"
				break
			fi
		done
		if [[ -z "${bash4}" ]]; then
			note "skipping hot-path evidence gate: needs bash >= 4 (install it, e.g. 'brew install bash'); CI still enforces it"
			exit 0
		fi
		ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${repo_root}" ESHU_PERFORMANCE_EVIDENCE_BASE="${base}" "${bash4}" "${repo_root}/scripts/verify-performance-evidence.sh"
		;;
	telemetry)
		# The telemetry-coverage gate (verify-telemetry-coverage.yml): a new metric
		# or pipeline stage must be reflected in the X1 coverage doc. Like the
		# perf-evidence gate it diffs against the PR base, so pin it to origin/main
		# (the script's HEAD~1 fallback only sees the last commit).
		git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
		base="origin/main"
		git -C "${repo_root}" rev-parse --verify "${base}" >/dev/null 2>&1 || base="HEAD~1"
		ESHU_TELEMETRY_COVERAGE_BASE="${base}" "${repo_root}/scripts/verify-telemetry-coverage.sh"
		;;
	measurement-citations)
		# The measurement-ledger citation gate (static-contract-gates.yml "Verify
		# measurement-citations gate"): a newly added "<N>/<M> trials" or
		# "Measurement:" claim must cite a docs/internal/measurements.jsonl row.
		# Refresh the base ref; the verifier resolves its common ancestor with
		# HEAD to one commit for both ledger and claim comparisons.
		git -C "${repo_root}" fetch --no-tags origin \
			main:refs/remotes/origin/main >/dev/null 2>&1 \
			|| die "cannot refresh origin/main for measurement citations"
		ESHU_MEASUREMENT_CITATIONS_BASE=origin/main "${repo_root}/scripts/verify-measurement-citations.sh"
		;;
	esac
}
