#!/usr/bin/env bash
# Local pre-commit helper for Eshu Go checks. Mirrors the CI gates that have
# repeatedly blocked PRs (golangci-lint, gofumpt, gosec G304, file-cap, and the
# capability surface-inventory drift) so they are caught at commit time instead
# of on GitHub.
#
# Usage: scripts/dev/precommit-go.sh <fmt|lint|filecap|gosec|surface> [files...]
#
# Design notes:
#   - Tools are installed with the LOCAL `go` toolchain (which go.mod pins to
#     >= 1.26.4) via `go install`, at the versions CI uses. Do NOT rely on a
#     brew/system golangci-lint: a Go plugin must be built with the exact Go
#     build of the host binary, and a mismatched toolchain fails plugin.Open.
#   - golangci-lint runs against a config copy with the custom `filelength`
#     plugin stripped, because that plugin is the one piece that needs an exact
#     toolchain match. The 500-line cap is enforced separately by `filecap`, so
#     coverage is equivalent to CI without the cross-machine fragility.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
# Cache tools/config under the git common dir (works from a worktree, where
# .git is a file, not a directory; the common dir is shared and never committed).
git_common="$(git -C "${repo_root}" rev-parse --git-common-dir 2>/dev/null || echo "${repo_root}/.git")"
case "${git_common}" in /*) ;; *) git_common="${repo_root}/${git_common}" ;; esac
cache_dir="${git_common}/eshu-precommit"
mkdir -p "${cache_dir}"

# Tool versions — keep in lockstep with the CI install steps.
golangci_version="$(rg -o 'golangci-lint@v[0-9.]+' "${repo_root}/.github/workflows/test.yml" 2>/dev/null | head -1 | sed 's/.*@//')"
gosec_version="$(rg -o 'gosec@v[0-9.]+' "${repo_root}/.github/workflows/security-scan.yml" 2>/dev/null | head -1 | sed 's/.*@//')"
golangci_version="${golangci_version:-v2.12.2}"
gosec_version="${gosec_version:-v2.27.1}"

note() { printf 'precommit-go: %s\n' "$*" >&2; }
die() { printf 'precommit-go: %s\n' "$*" >&2; exit 1; }

# go_dirs prints the unique go/-relative package dirs (as ./path) for the staged
# Go files passed as args, so package-level tools run only on what changed.
go_dirs() {
	local f rel dirs=()
	for f in "$@"; do
		case "${f}" in
			go/*.go|go/**/*.go) ;;
			*) continue ;;
		esac
		rel="${f#go/}"
		dirs+=("./$(dirname "${rel}")")
	done
	printf '%s\n' "${dirs[@]:-}" | awk 'NF' | sort -u
}

# collect_dirs fills the global `dirs` array from go_dirs. Avoids `mapfile`
# (bash >= 4 only) so the hook runs on the macOS system bash 3.2.
collect_dirs() {
	dirs=()
	local d
	while IFS= read -r d; do
		[[ -n "${d}" ]] && dirs+=("${d}")
	done < <(go_dirs "$@")
}

ensure_golangci() {
	local bin="${cache_dir}/golangci-lint-${golangci_version}"
	if [[ ! -x "${bin}" ]]; then
		note "installing golangci-lint ${golangci_version} (one-time, local toolchain)"
		GOBIN="${cache_dir}" GOFLAGS=-mod=mod go install \
			"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${golangci_version}" \
			|| die "failed to install golangci-lint ${golangci_version}"
		mv "${cache_dir}/golangci-lint" "${bin}"
	fi
	printf '%s' "${bin}"
}

ensure_gosec() {
	local bin="${cache_dir}/gosec-${gosec_version}"
	if [[ ! -x "${bin}" ]]; then
		note "installing gosec ${gosec_version} (one-time, local toolchain)"
		GOBIN="${cache_dir}" GOFLAGS=-mod=mod go install \
			"github.com/securego/gosec/v2/cmd/gosec@${gosec_version}" \
			|| die "failed to install gosec ${gosec_version}"
		mv "${cache_dir}/gosec" "${bin}"
	fi
	printf '%s' "${bin}"
}

# stripped_config writes a golangci config copy without the custom filelength
# plugin (the only linter needing an exact toolchain match) and prints its path.
stripped_config() {
	local out="${cache_dir}/golangci-nocustom.yml"
	awk '
		$0 ~ /^[[:space:]]*- filelength[[:space:]]*$/ { next }
		/^    custom:/ { skip = 1; next }
		skip == 1 { if ($0 ~ /^    [A-Za-z]/) { skip = 0 } else { next } }
		{ print }
	' "${go_dir}/.golangci.yml" > "${out}"
	printf '%s' "${out}"
}

cmd="${1:-}"
shift || true

case "${cmd}" in
	fmt)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && "${bin}" fmt --diff --config "${cfg}" "${dirs[@]}" ) \
			|| die "gofumpt formatting differences — run: cd go && golangci-lint fmt"
		;;
	lint)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && "${bin}" run --config "${cfg}" "${dirs[@]}" )
		;;
	filecap)
		# 500-line cap (the filelength plugin's job), honouring //nolint:filelength.
		status=0
		for f in "$@"; do
			[[ "${f}" == *.go ]] || continue
			[[ -f "${repo_root}/${f}" ]] || continue
			rg -q 'nolint:filelength' "${repo_root}/${f}" && continue
			lines="$(wc -l < "${repo_root}/${f}")"
			if (( lines > 500 )); then
				note "${f}: ${lines} lines exceeds the 500-line cap (split it, or //nolint:filelength with a reason)"
				status=1
			fi
		done
		exit "${status}"
		;;
	gosec)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_gosec)"
		pkgs=()
		for d in "${dirs[@]}"; do pkgs+=("${d}/..."); done
		out="${cache_dir}/gosec.sarif"
		( cd "${go_dir}" && "${bin}" -severity=low -confidence=low -no-fail \
			-fmt=sarif -out "${out}" "${pkgs[@]}" >/dev/null 2>&1 )
		findings="$(jq '[.runs[].results[]] | length' "${out}" 2>/dev/null || echo 0)"
		if [[ "${findings}" -ne 0 ]]; then
			jq -r '.runs[].results[] | "  \(.ruleId) \(.locations[0].physicalLocation.artifactLocation.uri):\(.locations[0].physicalLocation.region.startLine)"' "${out}" >&2
			die "gosec: ${findings} finding(s) — fix or annotate with a leading // #nosec <RULE> -- <reason>"
		fi
		;;
	surface)
		( cd "${go_dir}" && go run ./cmd/capability-inventory -mode verify >/dev/null ) \
			|| die "capability surface inventory is stale — run: cd go && go run ./cmd/capability-inventory -mode generate"
		;;
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
		ESHU_PERFORMANCE_EVIDENCE_BASE="${base}" "${bash4}" "${repo_root}/scripts/verify-performance-evidence.sh"
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
	*)
		die "unknown subcommand '${cmd}' (want fmt|lint|filecap|gosec|surface|perf-evidence|telemetry)"
		;;
esac
