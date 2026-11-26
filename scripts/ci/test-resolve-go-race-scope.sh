#!/usr/bin/env bash
# Hermetic contract tests for scripts/ci/resolve-go-race-scope.sh and
# scripts/ci/run-go-race-shard.sh. Uses temp repos and a stub `go` binary.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
resolver="${repo_root}/scripts/ci/resolve-go-race-scope.sh"
runner="${repo_root}/scripts/ci/run-go-race-shard.sh"

pass=0
fail=0
ok() {
	printf 'ok - %s\n' "$1"
	pass=$((pass + 1))
}
bad() {
	printf 'NOT OK - %s\n' "$1" >&2
	fail=$((fail + 1))
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# Create a minimal Go tree matching PR #11464 and PR #11465 shapes.
fake_repo="${tmp}/repo"
mkdir -p \
	"${fake_repo}/go/internal/query/supply/chain/impact" \
	"${fake_repo}/go/internal/storage/postgres" \
	"${fake_repo}/go/internal/alpha"
printf 'package impact\n' >"${fake_repo}/go/internal/query/supply/chain/impact/multi_support_parity_live_test.go"
printf 'package postgres\n' >"${fake_repo}/go/internal/storage/postgres/a_test.go"
printf 'package alpha\n' >"${fake_repo}/go/internal/alpha/a_test.go"

assert_scope() {
	local name="$1" event="$2" want_scope="$3" want_shards="$4" want_pkgs="$5"
	shift 5
	local list_file="${tmp}/changed.txt"
	printf '%s\n' "$@" >"${list_file}"
	local out scope shards pkgs
	out="$(bash "${resolver}" --event "${event}" --repo-root "${fake_repo}" --changed-files "${list_file}")"
	scope="$(printf '%s\n' "${out}" | sed -n 's/^scope=//p')"
	shards="$(printf '%s\n' "${out}" | sed -n 's/^shards=//p')"
	pkgs="$(printf '%s\n' "${out}" | sed -n 's/^packages=//p')"
	if [[ "${scope}" == "${want_scope}" && "${shards}" == "${want_shards}" && "${pkgs}" == "${want_pkgs}" ]]; then
		ok "${name}"
	else
		bad "${name}: got scope=${scope} shards=${shards} packages=${pkgs}, want scope=${want_scope} shards=${want_shards} packages=${want_pkgs}"
	fi
}

# 1. PR #11464 shape: single *_live_test.go file -> single runner, 1 package.
assert_scope "PR #11464 single _live_test.go scopes to 1 shard and 1 pkg" \
	pull_request changed-tests '[1]' './internal/query/supply/chain/impact' \
	'go/internal/query/supply/chain/impact/multi_support_parity_live_test.go'

# 2. PR #11465 shape: 3 *_test.go files in same package -> deduplicated to 1 pkg.
assert_scope "PR #11465 multiple _test.go in same pkg dedupes to 1 pkg" \
	pull_request changed-tests '[1]' './internal/storage/postgres' \
	'go/internal/storage/postgres/supply_chain_impact_winners_keyed_export_test.go' \
	'go/internal/storage/postgres/supply_chain_impact_winners_keyed_fanout_live_test.go' \
	'go/internal/storage/postgres/supply_chain_impact_winners_narrow_write_proof_live_test.go'

# 3. Multiple packages, all *_test.go -> sorted deduplicated list.
assert_scope "multi-package _test.go-only PR sorts and dedupes packages" \
	pull_request changed-tests '[1]' './internal/alpha ./internal/storage/postgres' \
	'go/internal/storage/postgres/a_test.go' \
	'go/internal/alpha/a_test.go'

# 4. Mixed *_test.go + production .go -> full 8 shards.
assert_scope "mixed _test.go and production .go stays full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/alpha/a_test.go' \
	'go/internal/alpha/a.go'

# 5. *_test.go + testdata fixture -> full 8 shards.
assert_scope "_test.go plus testdata fixture stays full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/alpha/a_test.go' \
	'go/internal/alpha/testdata/sample.json'

# 5b. testdata/*_test.go fixture -> full 8 shards (testdata is not a Go package).
mkdir -p "${fake_repo}/go/internal/alpha/testdata"
printf 'package sample\n' >"${fake_repo}/go/internal/alpha/testdata/fixture_test.go"
assert_scope "go/**/testdata/*_test.go fixture fails closed to full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/alpha/testdata/fixture_test.go'

# 6. *_test.go + markdown -> full 8 shards.
assert_scope "_test.go plus markdown stays full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/alpha/a_test.go' \
	'docs/public/guide.md'

# 7. sdk/go/*_test.go -> full 8 shards (separate module).
assert_scope "sdk/go _test.go stays full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'sdk/go/collector/client_test.go'

# 8. Non-PR events always stay full.
for ev in push schedule workflow_dispatch; do
	assert_scope "event=${ev} always stays full" \
		"${ev}" full '[1,2,3,4,5,6,7,8]' '' \
		'go/internal/alpha/a_test.go'
done

# 9. Deleted package directory fails closed to full.
assert_scope "deleted package directory fails closed to full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/deletedpkg/removed_test.go'

# 10. Parent traversal path fails closed to full.
assert_scope "parent traversal path fails closed to full" \
	pull_request full '[1,2,3,4,5,6,7,8]' '' \
	'go/internal/alpha/../alpha/a_test.go'

# 11. Empty diff fails closed to full.
empty_file="${tmp}/empty.txt"
: >"${empty_file}"
out="$(bash "${resolver}" --event pull_request --repo-root "${fake_repo}" --changed-files "${empty_file}")"
if [[ "${out}" == *$'scope=full'* && "${out}" == *$'shards=[1,2,3,4,5,6,7,8]'* ]]; then
	ok "empty diff fails closed to full"
else
	bad "empty diff should fail closed to full, got: ${out}"
fi

# 12. >300 files fails closed to full.
many_file="${tmp}/many.txt"
: >"${many_file}"
for i in $(seq 1 301); do
	printf 'go/internal/alpha/f%d_test.go\n' "${i}" >>"${many_file}"
done
out="$(bash "${resolver}" --event pull_request --repo-root "${fake_repo}" --changed-files "${many_file}")"
if [[ "${out}" == *$'scope=full'* && "${out}" == *$'shards=[1,2,3,4,5,6,7,8]'* ]]; then
	ok ">300 files fails closed to full"
else
	bad ">300 files should fail closed to full, got: ${out}"
fi

# 13. Real git 2-parent merge commit (mirrors actions/checkout@v5 on pull_request).
git_repo="${tmp}/gitrepo"
mkdir -p "${git_repo}/go/internal/alpha"
git -C "${git_repo}" init -q -b main
git -C "${git_repo}" config user.email test@example.com
git -C "${git_repo}" config user.name test
git -C "${git_repo}" config commit.gpgsign false
printf 'package alpha\n' >"${git_repo}/go/internal/alpha/a.go"
git -C "${git_repo}" add .
git -C "${git_repo}" commit -q -m base
git -C "${git_repo}" checkout -q -b pr-branch
printf 'package alpha\n' >"${git_repo}/go/internal/alpha/a_test.go"
git -C "${git_repo}" add .
git -C "${git_repo}" commit -q -m "add test"
# Single-parent commit (non-merge) must fail closed to full.
out="$(bash "${resolver}" --event pull_request --repo-root "${git_repo}")"
if [[ "${out}" == *$'scope=full'* ]]; then
	ok "non-merge HEAD fails closed to full"
else
	bad "non-merge HEAD must fail closed to full, got: ${out}"
fi
# Now create a synthetic merge commit like GitHub's refs/pull/N/merge.
git -C "${git_repo}" checkout -q main
git -C "${git_repo}" merge -q --no-ff pr-branch -m "merge pr"
out="$(bash "${resolver}" --event pull_request --repo-root "${git_repo}")"
if [[ "${out}" == *$'scope=changed-tests'* && "${out}" == *$'shards=[1]'* && "${out}" == *$'packages=./internal/alpha'* ]]; then
	ok "real 2-parent merge commit resolves changed-tests scope"
else
	bad "real 2-parent merge commit failed to resolve changed-tests, got: ${out}"
fi

# 13b. Rename prod .go -> *_test.go must fail closed to full (--no-renames).
git -C "${git_repo}" checkout -q -b pr-rename-prod
git -C "${git_repo}" mv go/internal/alpha/a.go go/internal/alpha/moved_from_prod_test.go
git -C "${git_repo}" commit -q -m "rename prod to test"
git -C "${git_repo}" checkout -q main
git -C "${git_repo}" merge -q --no-ff pr-rename-prod -m "merge rename prod"
out="$(bash "${resolver}" --event pull_request --repo-root "${git_repo}")"
if [[ "${out}" == *$'scope=full'* && "${out}" == *$'shards=[1,2,3,4,5,6,7,8]'* ]]; then
	ok "git mv prod.go -> _test.go fails closed to full"
else
	bad "git mv prod.go -> _test.go must fail closed to full, got: ${out}"
fi

# 13c. Move *_test.go across packages must test both source and destination packages.
git -C "${git_repo}" reset -q --hard HEAD^1
mkdir -p "${git_repo}/go/internal/beta"
printf 'package beta\n' >"${git_repo}/go/internal/beta/b.go"
git -C "${git_repo}" add go/internal/beta/b.go
git -C "${git_repo}" commit -q -m "add beta pkg"
git -C "${git_repo}" checkout -q -b pr-move-test
git -C "${git_repo}" mv go/internal/alpha/a_test.go go/internal/beta/a_test.go
git -C "${git_repo}" commit -q -m "move test across pkgs"
git -C "${git_repo}" checkout -q main
git -C "${git_repo}" merge -q --no-ff pr-move-test -m "merge move test"
out="$(bash "${resolver}" --event pull_request --repo-root "${git_repo}")"
if [[ "${out}" == *$'scope=changed-tests'* && "${out}" == *$'shards=[1]'* && "${out}" == *$'packages=./internal/alpha ./internal/beta'* ]]; then
	ok "git mv _test.go across packages selects both source and destination"
else
	bad "git mv _test.go across packages must select both packages, got: ${out}"
fi

# 14. Exercise run-go-race-shard.sh with a stub `go` binary.
bin_dir="${tmp}/bin"
mkdir -p "${bin_dir}"
log_file="${tmp}/go-args.log"
cat >"${bin_dir}/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "list" ]]; then
	printf './pkg1\n./pkg2\n./pkg3\n./pkg4\n'
	exit 0
fi
printf '%s\n' "$*" >>"${GO_LOG_FILE}"
EOF
chmod +x "${bin_dir}/go"

: >"${log_file}"
(
	cd "${fake_repo}/go"
	PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${log_file}" \
		GITHUB_EVENT_NAME=pull_request GO_RACE_SCOPE=changed-tests \
		GO_RACE_PACKAGES='./internal/alpha ./internal/storage/postgres' \
		bash "${runner}" >/dev/null
)
if [[ "$(cat "${log_file}")" == 'test -count=1 -race -timeout 900s -p 2 ./internal/alpha ./internal/storage/postgres' ]]; then
	ok "run-go-race-shard.sh changed-tests invokes go test -race on changed packages only"
else
	bad "run-go-race-shard.sh changed-tests unexpected args: $(cat "${log_file}")"
fi

# Empty GO_RACE_PACKAGES in changed-tests mode must fail closed.
if (
	cd "${fake_repo}/go"
	PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${log_file}" \
		GITHUB_EVENT_NAME=pull_request GO_RACE_SCOPE=changed-tests \
		GO_RACE_PACKAGES='' \
		bash "${runner}" >/dev/null 2>&1
); then
	bad "run-go-race-shard.sh must fail closed when GO_RACE_PACKAGES is empty"
else
	ok "run-go-race-shard.sh fails closed on empty GO_RACE_PACKAGES"
fi

# Full mode runs LPT-sharded go list ./...
: >"${log_file}"
weights_stub="${tmp}/weights.tsv"
cat >"${weights_stub}" <<'EOF'
# Heavy packages must be separated across shards (whereas NR % 2 puts pkg1+pkg3 together).
./pkg1	100.00
./pkg3	90.00
EOF
(
	cd "${fake_repo}/go"
	PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${log_file}" \
		GO_RACE_WEIGHTS_FILE="${weights_stub}" \
		GITHUB_EVENT_NAME=pull_request GO_RACE_SCOPE=full \
		SHARD_INDEX=1 SHARD_TOTAL=2 \
		bash "${runner}" >/dev/null
	PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${log_file}" \
		GO_RACE_WEIGHTS_FILE="${weights_stub}" \
		GITHUB_EVENT_NAME=pull_request GO_RACE_SCOPE=full \
		SHARD_INDEX=2 SHARD_TOTAL=2 \
		bash "${runner}" >/dev/null
)
shard1_line="$(sed -n '1p' "${log_file}")"
shard2_line="$(sed -n '2p' "${log_file}")"
if [[ "${shard1_line}" == 'test -count=1 -race -timeout 900s -p 2 ./pkg1' && "${shard2_line}" == 'test -count=1 -race -timeout 900s -p 2 ./pkg2 ./pkg3 ./pkg4' ]]; then
	ok "run-go-race-shard.sh full mode balances heavy packages across shards via LPT"
else
	bad "run-go-race-shard.sh full mode unexpected LPT split: shard1=[${shard1_line}] shard2=[${shard2_line}]"
fi

# 15. Exact partition invariant over all weighted packages + 200 synthetic packages
# (hermetic: never invokes the host `go` binary because test.yml's `changes` job
# runs before setup-go).
selector="${repo_root}/scripts/ci/select-go-race-shard.py"
real_weights="${repo_root}/specs/go-race-shard-weights.tsv"
all_pkgs="${tmp}/all-pkgs.txt"
{
	awk -F '\t' '!/^[[:space:]]*#/ && NF==2 { print $1 }' "${real_weights}"
	for i in $(seq -w 1 200); do
		printf 'github.com/eshu-hq/eshu/go/internal/synth/pkg%s\n' "${i}"
	done
} | LC_ALL=C sort -u >"${all_pkgs}"
union_pkgs="${tmp}/union-pkgs.txt"
: >"${union_pkgs}"
for idx in 1 2 3 4 5 6 7 8; do
	python3 "${selector}" --weights "${real_weights}" --shard-index "${idx}" --shard-total 8 <"${all_pkgs}" >>"${union_pkgs}"
done
if diff -u "${all_pkgs}" <(LC_ALL=C sort "${union_pkgs}") >/dev/null &&
	[[ "$(wc -l <"${all_pkgs}")" -eq "$(wc -l <"${union_pkgs}")" ]]; then
	ok "select-go-race-shard.py forms an exact disjoint partition across all 8 shards"
else
	bad "select-go-race-shard.py partition mismatch across 8 shards"
fi

# Verify heaviest package (internal/cigates) is separated from second-heaviest (internal/content/artifacts).
# Uses awk (not rg) so this test stays hermetic on ubuntu-24.04 runners where ripgrep is not installed.
shard1_real="${tmp}/shard1-real.txt"
python3 "${selector}" --weights "${real_weights}" --shard-index 1 --shard-total 8 <"${all_pkgs}" >"${shard1_real}"
if awk -v want='github.com/eshu-hq/eshu/go/internal/cigates' '$0 == want { found = 1 } END { exit !found }' "${shard1_real}" &&
	! awk -v want='github.com/eshu-hq/eshu/go/internal/content/artifacts' '$0 == want { found = 1 } END { exit !found }' "${shard1_real}"; then
	ok "select-go-race-shard.py isolates internal/cigates from internal/content/artifacts"
else
	bad "select-go-race-shard.py failed to separate top heavy packages"
fi

# 16. Fail closed on invalid shard args, missing weights path, and malformed/non-finite/duplicate TSV rows.
if python3 "${selector}" --weights "${real_weights}" --shard-index 9 --shard-total 8 <"${all_pkgs}" >/dev/null 2>&1; then
	bad "select-go-race-shard.py must reject out-of-range shard-index"
else
	ok "select-go-race-shard.py rejects out-of-range shard-index"
fi
if python3 "${selector}" --weights "${tmp}/does-not-exist.tsv" --shard-index 1 --shard-total 8 <"${all_pkgs}" >/dev/null 2>&1; then
	bad "select-go-race-shard.py must fail closed when explicit --weights file is missing"
else
	ok "select-go-race-shard.py fails closed when explicit --weights file is missing"
fi
bad_weights="${tmp}/bad-weights.tsv"
printf 'malformed-line-without-tab\n' >"${bad_weights}"
if python3 "${selector}" --weights "${bad_weights}" --shard-index 1 --shard-total 8 <"${all_pkgs}" >/dev/null 2>&1; then
	bad "select-go-race-shard.py must fail closed on malformed weights TSV"
else
	ok "select-go-race-shard.py fails closed on malformed weights TSV"
fi
for bad_val in nan inf -inf 0 -5; do
	printf 'github.com/eshu-hq/eshu/go/internal/a\t%s\n' "${bad_val}" >"${bad_weights}"
	if python3 "${selector}" --weights "${bad_weights}" --shard-index 1 --shard-total 8 <"${all_pkgs}" >/dev/null 2>&1; then
		bad "select-go-race-shard.py must reject non-finite or non-positive weight (${bad_val})"
	else
		ok "select-go-race-shard.py rejects weight=${bad_val}"
	fi
done
printf 'github.com/eshu-hq/eshu/go/internal/a\t10.00\ngithub.com/eshu-hq/eshu/go/internal/a\t20.00\n' >"${bad_weights}"
if python3 "${selector}" --weights "${bad_weights}" --shard-index 1 --shard-total 8 <"${all_pkgs}" >/dev/null 2>&1; then
	bad "select-go-race-shard.py must reject duplicate package weight entries"
else
	ok "select-go-race-shard.py rejects duplicate package weight entries"
fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
