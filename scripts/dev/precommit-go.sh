#!/usr/bin/env bash
# Local pre-commit helper for Eshu Go checks. Mirrors the CI gates that have
# repeatedly blocked PRs (golangci-lint, gofumpt, gosec G304, file-cap, and the
# capability surface-inventory drift) so they are caught at commit time instead
# of on GitHub.
#
# Usage: scripts/dev/precommit-go.sh <fmt|lint|lint-all|fmt-all|filecap|filecap-all|dirgate|dirgate-all|gosec|gosec-all|gosec-changed|govulncheck|nancy|surface|cache-paths> [files...]
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
# #9098: pin GOTOOLCHAIN to go/go.mod and clear GOROOT before any lint, gosec or
# tool install; see scripts/lib/go-toolchain.sh.
# shellcheck source=../lib/go-toolchain.sh
source "${script_root}/scripts/lib/go-toolchain.sh"
eshu_export_go_toolchain "${repo_root}"
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

# The 500-line file cap (filecap_skip, filecap_count_lines, filecap_nolint_directive,
# filecap_check_file) lives in a sourced helper; its design notes are there.
# shellcheck source=../lib/precommit-go-filecap.sh
source "${script_root}/scripts/lib/precommit-go-filecap.sh"

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
# plugin (the only linter needing an exact toolchain match) and prints its path.
stripped_config() {
	local out="${worktree_cache_dir}/golangci-nocustom.yml"
	awk '
		$0 ~ /^[[:space:]]*- filelength[[:space:]]*$/ { next }
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
		# Report how many packages were linted before doing it. `lint` with no
		# arguments, or with arguments that map to no Go package, exits 0
		# having run golangci-lint zero times -- which reads identically to a
		# clean lint of the whole tree. Printing the count is what separates
		# them, since this command's exit code alone cannot.
		collect_dirs "$@"
		printf 'lint: %d package(s) from %d path(s)\n' "${#dirs[@]}" "$#"
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
		# filecap_check_file above — but NOT the same set of files: this arm
		# sees every .go path the hook stages, including paths outside go/ that
		# filecap-all and the CI plugin never look at. The header block above
		# has the direction and the margin. Self-tested by
		# scripts/test-precommit-go-filecap.sh.
		status=0
		for f in "$@"; do
			filecap_check_file "${f}" || status=1
		done
		exit "${status}"
		;;
	dirgate)
		# Directory-size and naming gate (issue #6054), pre-commit variant:
		# judges the directories of the staged files against the merge base.
		# The commit message does not exist yet, so a growth trailer is only a
		# note here; pre-push and CI enforce it. See go/cmd/dirgate/README.md.
		bash "${repo_root}/scripts/verify-dirgate.sh" --pre-commit "$@"
		exit "$?"
		;;
	dirgate-all)
		# Whole-tree gate. The ci-gates runner invokes this no-arg variant (see
		# the go-dir-gate row in specs/ci-gates.d/gates-01.yaml). It compares
		# origin/main merged with HEAD against origin/main, and under GitHub
		# Actions the merge ref against its first parent.
		bash "${repo_root}/scripts/verify-dirgate.sh" --all
		exit "$?"
		;;
	gosec)
		# shellcheck source=../lib/precommit-go-gosec.sh
		source "${script_root}/scripts/lib/precommit-go-gosec.sh"
		gosec_arm_files "$@"
		;;
	gosec-changed)
		# shellcheck source=../lib/precommit-go-gosec.sh
		source "${script_root}/scripts/lib/precommit-go-gosec.sh"
		gosec_arm_changed
		;;
	surface)
		( source "${script_root}/scripts/lib/cgo-cflags.sh" && eshu_export_cgo_cflags && cd "${go_dir}" && go run ./cmd/capability-inventory -mode verify >/dev/null ) \
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
		# Refresh the base ref; the verifier resolves its common ancestor with
		# HEAD to one commit for both ledger and claim comparisons.
		git -C "${repo_root}" fetch --no-tags origin \
			main:refs/remotes/origin/main >/dev/null 2>&1 \
			|| die "cannot refresh origin/main for measurement citations"
		ESHU_MEASUREMENT_CITATIONS_BASE=origin/main "${repo_root}/scripts/verify-measurement-citations.sh"
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
	filecap-skip)
		# Answers one question and nothing else: does the bash side skip this
		# path? Exit 0 means skipped, 1 means it would be checked against the
		# cap. It exists so the authoritative Go plugin's own test can call the
		# bash implementation and compare, in
		# tools/golangci-lint-filelength/skip_parity_test.go. Without it the
		# two sides could only be compared by reading them, which is what let
		# them drift in the first place (#6104).
		[[ $# -eq 1 ]] || die "filecap-skip needs exactly one path"
		filecap_skip "$1"
		exit "$?"
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
		# Whole-module gosec (./...), the same scope as security-scan.yml's
		# authoritative scan. Kept as a manual command; the registry row runs gosec-changed.
		# shellcheck source=../lib/precommit-go-gosec.sh
		source "${script_root}/scripts/lib/precommit-go-gosec.sh"
		run_gosec gosec-all ./...
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
		die "unknown subcommand '${cmd}' (want fmt|lint|lint-all|fmt-all|filecap|filecap-all|dirgate|dirgate-all|gosec|gosec-all|gosec-changed|govulncheck|nancy|surface|perf-evidence|telemetry|measurement-citations|cache-paths)"
		;;
esac
