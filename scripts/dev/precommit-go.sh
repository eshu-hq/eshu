#!/usr/bin/env bash
# Local pre-commit helper for Eshu Go checks. Mirrors the CI gates that have
# repeatedly blocked PRs (golangci-lint, gofumpt, gosec G304, file-cap, and the
# capability surface-inventory drift) so they are caught at commit time instead
# of on GitHub.
#
# Usage: scripts/dev/precommit-go.sh <fmt|lint|lint-all|fmt-all|filecap|filecap-all|dirgate|dirgate-all|dirgate-digest|gosec|gosec-all|govulncheck|nancy|surface|cache-paths> [files...]
#   lint-all / fmt-all run over the whole module (./...); the pre-pr gate
#   (scripts/dev/pre-pr.sh) uses them to mirror CI before the first push.
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
#   - Versioned tool binaries are shared across worktrees. Generated configs,
#     analyzer caches, and SARIF results are isolated to the current worktree.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || printf '%s\n' "${script_root}")"
go_dir="${repo_root}/go"
# Share immutable tool binaries through the git common dir, but keep mutable
# config and analyzer results under the current worktree's git dir. Sharing the
# latter lets one worktree reuse findings or overwrite config for another.
git_common="$(git -C "${repo_root}" rev-parse --git-common-dir 2>/dev/null || echo "${repo_root}/.git")"
case "${git_common}" in /*) ;; *) git_common="${repo_root}/${git_common}" ;; esac
git_dir="$(git -C "${repo_root}" rev-parse --git-dir 2>/dev/null || echo "${repo_root}/.git")"
case "${git_dir}" in /*) ;; *) git_dir="${repo_root}/${git_dir}" ;; esac
tool_cache_dir="${git_common}/eshu-precommit"
worktree_cache_dir="${git_dir}/eshu-precommit-state"
golangci_cache_dir="${worktree_cache_dir}/golangci-lint"
mkdir -p "${tool_cache_dir}" "${worktree_cache_dir}" "${golangci_cache_dir}"

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

# go_install_tool installs one pinned tool binary into the shared tool cache.
#
# The `env -u GOROOT` is load-bearing, not defensive noise. When go.mod's `go`
# directive is newer than the host `go` binary (go.mod pins 1.26.6 since #6112;
# a developer host may still be on 1.26.5), GOTOOLCHAIN=auto re-execs the
# downloaded newer toolchain, and that switched process exports GOROOT to every
# child it spawns — including the ci-gates runner, which shells each gate
# command out through /bin/sh with the environment it inherited.
#
# These installs deliberately run from the CALLER's working directory, which for
# every gate path is the repo root. There is no go.mod there, so the host `go`
# never sees the 1.26.6 directive and never switches toolchains: it builds with
# the 1.26.5 driver against the inherited 1.26.6 GOROOT, and every package fails
# with `compile: version "go1.26.6" does not match go tool version "go1.26.5"`.
# The gate then dies on the INSTALL, before the scanner ever runs.
#
# Clearing GOROOT restores the self-consistent host toolchain the design note
# above already intends. The `go` calls that run inside go/ (capability-inventory,
# nancy-local's `go list`) need no such treatment: they see the directive, switch
# to 1.26.6, and their driver and GOROOT agree.
#
# scripts/test-precommit-go-toolchain-isolation.sh is the regression guard;
# removing `env -u GOROOT` from any call site turns it red.
go_install_tool() {
	GOBIN="${tool_cache_dir}" GOFLAGS=-mod=mod env -u GOROOT go install "$@"
}

# ---------------------------------------------------------------------------
# 500-line file cap. ONE implementation, shared by the `filecap` (changed-files,
# run by the pre-commit hook and scripts/dev/pre-pr.sh) and `filecap-all`
# (whole-tree, run by the ci-gates runner) subcommands below. They used to carry
# separate bodies and drifted: `filecap` had none of the exemptions, so a commit
# touching any long _test.go file was rejected locally while CI and `filecap-all`
# both passed it, and the only way through was a //nolint:filelength marker that
# suppresses nothing in CI. Keep the logic here, not in the case arms.
# ---------------------------------------------------------------------------

# filecap_skip returns 0 for a Go path the cap intentionally does not apply to,
# mirroring the authoritative CI plugin's skip()
# (tools/golangci-lint-filelength/filelength.go): _test.go files, and any path
# with a `generated`, `vendor`, or `testdata` path SEGMENT.
#
# The plugin matches with strings.Contains(path, "/<seg>/") against an ABSOLUTE
# path, so a segment at the very start still has a separator in front of it.
# These callers pass repo-RELATIVE paths, where that leading separator is
# absent — hence the `<seg>/*` alternative beside `*/<seg>/*`. Without it a
# repo-root `testdata/` tree (this repo has one) would be capped locally and
# exempt in CI. The `*/<seg>/*` form is otherwise exactly equivalent to the
# plugin's substring test: `*` in a case pattern matches `/`, and the literal
# separators anchor the segment so `generated_foo/` and `vendored/` stay capped.
filecap_skip() {
	local f="$1"
	[[ "${f}" == *_test.go ]] && return 0
	case "${f}" in
		generated/*|*/generated/*) return 0 ;;
		vendor/*|*/vendor/*) return 0 ;;
		testdata/*|*/testdata/*) return 0 ;;
	esac
	return 1
}

# filecap_count_lines counts physical lines the way the plugin's bufio.Scanner
# does: a final line with no trailing newline still counts. `wc -l` counts
# newline CHARACTERS, so on such a file it reports one fewer and the local gate
# would be one line MORE permissive than CI. awk's NR counts the trailing
# partial line, matching the plugin exactly.
filecap_count_lines() {
	awk 'END { print NR }' "$1"
}

# filecap_check_file evaluates one repo-relative Go path against the cap,
# printing the violation. Returns 1 on violation, 0 otherwise.
filecap_check_file() {
	local f="$1" lines
	[[ "${f}" == *.go ]] || return 0
	filecap_skip "${f}" && return 0
	[[ -f "${repo_root}/${f}" ]] || return 0
	rg -q 'nolint:filelength' "${repo_root}/${f}" && return 0
	lines="$(filecap_count_lines "${repo_root}/${f}")"
	if (( lines > 500 )); then
		note "${f}: ${lines} lines exceeds the 500-line cap (split it, or //nolint:filelength with a reason)"
		return 1
	fi
	return 0
}

ensure_golangci() {
	local bin="${tool_cache_dir}/golangci-lint-${golangci_version}"
	if [[ ! -x "${bin}" ]]; then
		note "installing golangci-lint ${golangci_version} (one-time, local toolchain)"
		go_install_tool \
			"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${golangci_version}" \
			|| die "failed to install golangci-lint ${golangci_version}"
		mv "${tool_cache_dir}/golangci-lint" "${bin}"
	fi
	printf '%s' "${bin}"
}

ensure_gosec() {
	local bin="${tool_cache_dir}/gosec-${gosec_version}"
	if [[ ! -x "${bin}" ]]; then
		note "installing gosec ${gosec_version} (one-time, local toolchain)"
		go_install_tool \
			"github.com/securego/gosec/v2/cmd/gosec@${gosec_version}" \
			|| die "failed to install gosec ${gosec_version}"
		mv "${tool_cache_dir}/gosec" "${bin}"
	fi
	printf '%s' "${bin}"
}

ensure_govulncheck() {
	local bin="${tool_cache_dir}/govulncheck"
	# CI installs @latest on every run. Always reinstall @latest here too rather
	# than freezing the first-resolved binary in the cache — go's module/build
	# cache makes a no-change reinstall fast, and this keeps the local advisory
	# database tooling in lockstep with CI instead of silently drifting stale.
	note "installing govulncheck@latest (local toolchain)"
	go_install_tool \
		"golang.org/x/vuln/cmd/govulncheck@latest" \
		|| die "failed to install govulncheck"
	printf '%s' "${bin}"
}

ensure_nancy() {
	local bin="${tool_cache_dir}/nancy"
	# Always reinstall @latest (see ensure_govulncheck) to match CI and avoid
	# freezing a stale nancy in the cache.
	note "installing nancy@latest (local toolchain)"
	go_install_tool \
		"github.com/sonatype-nexus-community/nancy@latest" \
		|| die "failed to install nancy"
	printf '%s' "${bin}"
}

# stripped_config writes a golangci config copy without the custom filelength
# and dirgate
# plugin (the only linter needing an exact toolchain match) and prints its path.
stripped_config() {
	local out="${worktree_cache_dir}/golangci-nocustom.yml"
	awk '
		$0 ~ /^[[:space:]]*- (filelength|dirgate)[[:space:]]*$/ { next }
		/^    custom:/ { skip = 1; next }
		skip == 1 { if ($0 ~ /^    [A-Za-z]/) { skip = 0 } else { next } }
		{ print }
	' "${go_dir}/.golangci.yml" > "${out}"
	printf '%s' "${out}"
}

run_golangci() {
	GOLANGCI_LINT_CACHEPROG= GOLANGCI_LINT_CACHE="${golangci_cache_dir}" "$@"
}

cmd="${1:-}"
shift || true

case "${cmd}" in
	cache-paths)
		printf 'tool_cache_dir=%s\n' "${tool_cache_dir}"
		printf 'worktree_cache_dir=%s\n' "${worktree_cache_dir}"
		printf 'golangci_cache_dir=%s\n' "${golangci_cache_dir}"
		;;
	fmt)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && run_golangci "${bin}" fmt --diff --config "${cfg}" "${dirs[@]}" ) \
			|| die "gofumpt formatting differences — run: cd go && golangci-lint fmt"
		;;
	lint)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && run_golangci "${bin}" run \
			--allow-parallel-runners --config "${cfg}" "${dirs[@]}" )
		;;
	filecap)
		# 500-line cap (the filelength plugin's job), changed-files variant: the
		# pre-commit hook (.pre-commit-config.yaml go-file-cap) and
		# scripts/dev/pre-pr.sh's step_filecap both pass a file list here. Same
		# verdict as filecap-all on any given file — both go through
		# filecap_check_file above. Self-tested by
		# scripts/test-precommit-go-filecap.sh.
		status=0
		for f in "$@"; do
			filecap_check_file "${f}" || status=1
		done
		exit "${status}"
		;;
	dirgate)
		# Directory-size and naming gate (issue #6054), changed-files variant:
		# maps the staged files to their containing go/ package directories and
		# evaluates each (scripts/lib/dirgate-core.sh mirrors
		# tools/golangci-lint-dirgate exactly). No-op if nothing under go/
		# staged.
		bash "${repo_root}/scripts/verify-dirgate.sh" --files "$@"
		exit "$?"
		;;
	dirgate-all)
		# Whole-tree directory-size and naming gate. This no-arg variant is
		# what the ci-gates runner invokes (see specs/ci-gates.v1.yaml
		# go-dir-gate).
		bash "${repo_root}/scripts/verify-dirgate.sh" --all
		exit "$?"
		;;
	dirgate-digest)
		# Human-facing helper for authoring/updating a
		# scripts/lib/dirgate-grandfather.tsv row: prints the live count and
		# digest for a go/-relative directory, e.g.
		# `precommit-go.sh dirgate-digest internal/query`.
		bash "${repo_root}/scripts/verify-dirgate.sh" --digest "${1:-}"
		exit "$?"
		;;
	gosec)
		collect_dirs "$@"
		[[ ${#dirs[@]} -gt 0 ]] || exit 0
		bin="$(ensure_gosec)"
		pkgs=()
		for d in "${dirs[@]}"; do pkgs+=("${d}/..."); done
		out="${worktree_cache_dir}/gosec.sarif"
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
		# Like perf-evidence and telemetry it diffs against the PR base, so pin it
		# to origin/main (the script's HEAD~1 fallback only sees the last commit
		# and would miss earlier commits on a multi-commit branch).
		git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
		base="origin/main"
		git -C "${repo_root}" rev-parse --verify "${base}" >/dev/null 2>&1 || base="HEAD~1"
		ESHU_MEASUREMENT_CITATIONS_BASE="${base}" "${repo_root}/scripts/verify-measurement-citations.sh"
		;;
	lint-all)
		# Whole-module golangci-lint (./...), not just changed packages. Catches
		# cross-package consequences a changed-package run misses — e.g. code that
		# becomes unused when a sibling package stops referencing it. Used by the
		# pre-pr gate to mirror CI's "Lint Go" step.
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && run_golangci "${bin}" run \
			--allow-parallel-runners --config "${cfg}" ./... )
		;;
	fmt-all)
		bin="$(ensure_golangci)"
		cfg="$(stripped_config)"
		( cd "${go_dir}" && run_golangci "${bin}" fmt --diff --config "${cfg}" ./... ) \
			|| die "gofumpt formatting differences — run: cd go && golangci-lint fmt"
		;;
	filecap-all)
		# Whole-tree 500-line cap over every tracked Go file. This no-arg variant
		# is what the ci-gates runner invokes (it passes no file list). Shares
		# filecap_check_file with the `filecap` arm above, which is what mirrors
		# the authoritative CI plugin's skip()
		# (tools/golangci-lint-filelength/filelength.go).
		status=0
		while IFS= read -r f; do
			filecap_check_file "${f}" || status=1
		done < <(git -C "${repo_root}" ls-files 'go/*.go')
		exit "${status}"
		;;
	gosec-all)
		# Whole-module gosec (./...), mirroring security-scan.yml's authoritative
		# scan. Slower than the changed-file `gosec` subcommand (gosec is per-package
		# SSA-heavy on Go 1.26); used by the ci-gates runner where no file list is
		# passed. The local security lane in #4217 narrows this to changed packages.
		bin="$(ensure_gosec)"
		out="${worktree_cache_dir}/gosec-all.sarif"
		( cd "${go_dir}" && "${bin}" -severity=low -confidence=low -no-fail \
			-fmt=sarif -out "${out}" ./... >/dev/null 2>&1 )
		findings="$(jq '[.runs[].results[]] | length' "${out}" 2>/dev/null || echo 0)"
		if [[ "${findings}" -ne 0 ]]; then
			jq -r '.runs[].results[] | "  \(.ruleId) \(.locations[0].physicalLocation.artifactLocation.uri):\(.locations[0].physicalLocation.region.startLine)"' "${out}" >&2
			die "gosec: ${findings} finding(s) — fix or annotate with a leading // #nosec <RULE> -- <reason>"
		fi
		;;
	govulncheck)
		# Whole-module govulncheck against the Go vulnerability database,
		# mirroring security-scan.yml. `-scan package` (not the default symbol
		# mode) avoids the x/tools SSA panic on Go 1.26 generics. Needs network
		# to fetch the vuln database.
		bin="$(ensure_govulncheck)"
		( cd "${go_dir}" && "${bin}" -scan package ./... ) \
			|| die "govulncheck: vulnerabilities found (see output above)"
		;;
	nancy)
		# nancy is declared ADVISORY (specs/ci-gates.v1.yaml, #5791/#5804): OSS
		# Index currently 401s every anonymous request and no OSS Index
		# credentials exist anywhere in this repo, so it cannot reliably
		# perform a real scan today. The actual sleuth/classification logic
		# lives in scripts/dev/nancy-local.sh (unit-tested directly with fake
		# `go`/`nancy` binaries in scripts/test-nancy-local.sh); this case only
		# installs the real nancy binary and delegates.
		bin="$(ensure_nancy)"
		bash "${repo_root}/scripts/dev/nancy-local.sh" "${go_dir}" "${worktree_cache_dir}" "${bin}"
		;;
	*)
		die "unknown subcommand '${cmd}' (want fmt|lint|lint-all|fmt-all|filecap|filecap-all|dirgate|dirgate-all|dirgate-digest|gosec|gosec-all|govulncheck|nancy|surface|perf-evidence|telemetry|measurement-citations|cache-paths)"
		;;
esac
