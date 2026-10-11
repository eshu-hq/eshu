#!/usr/bin/env bash
# Hermetic cases for the PR-identity selector used by required-gates.yml.
#
# The bug this guards against was invisible locally: the selector lived inline in
# the workflow, nothing exercised it, and `base.ref == <default branch>` silently
# returned zero for every stacked lane. The step then exited 1, the aggregate job
# never wrote code=, and the publisher fell to its catch-all. A red that was not
# a failing gate -- a selection that matched nothing, and a consumer that could
# not tell "no answer" from "bad answer".
#
# The fixtures below are the real shape of `commits/<sha>/pulls`: it returns
# every OPEN pr CONTAINING the commit, which on a stacked branch is the lane
# itself plus every lane above it in the train. That is what makes base.ref
# unusable and head.sha correct. Since #7950 the resolver reads the open pull
# request list instead, which holds every lane; these payloads are subsets of it
# and still pin the head.sha selection. The fork cases at the end drive the fetch.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
resolver="${repo_root}/scripts/resolve-required-gates-pr.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

pulls="${tmp_dir}/pulls.json"
failures=0

pr() { # number head_sha base_ref state
	printf '{"number":%s,"state":"%s","head":{"sha":"%s"},"base":{"ref":"%s"}}' "$1" "$4" "$2" "$3"
}

run_resolver() { # head_sha
	HEAD_SHA="$1" ESHU_PULLS_JSON="${pulls}" "${resolver}"
}

expect_number() { # label head_sha want
	local label="$1" sha="$2" want="$3" got rc
	got="$(run_resolver "${sha}" 2>/dev/null)" && rc=0 || rc=$?
	if [[ "${rc}" -ne 0 ]]; then
		echo "FAIL ${label}: resolver exited ${rc}, expected PR ${want}" >&2
		failures=$((failures + 1))
		return
	fi
	if [[ "${got}" != "${want}" ]]; then
		echo "FAIL ${label}: resolved PR ${got}, expected ${want}" >&2
		failures=$((failures + 1))
		return
	fi
	echo "ok - ${label}"
}

expect_fail_closed() { # label head_sha
	local label="$1" sha="$2" rc
	if run_resolver "${sha}" >/dev/null 2>&1; then
		echo "FAIL ${label}: resolver succeeded; it must fail closed" >&2
		failures=$((failures + 1))
		return
	fi
	echo "ok - ${label}"
}

# ---------------------------------------------------------------------------
# A five-PR stacked train: 6332 targets main, each later lane targets the one
# below it.
#
# THE TOPOLOGY MATTERS AND IS EASY TO GET WRONG. `commits/<sha>/pulls` returns
# the open PRs whose branch CONTAINS that commit -- the lane itself plus every
# lane ABOVE it in the train. Lanes BELOW do not contain it and are absent. So
# the payload for the bottom lane's head holds the whole train, while the
# payload for an upper lane's head holds only that lane and the ones above it,
# none of which is main-based.
#
# That asymmetry is the entire bug: for an upper lane there is no main-based PR
# in the set at all, so `base.ref == main` matched nothing. Building a fixture
# that puts the bottom lane in an upper lane's payload would misrepresent the
# API and teach the wrong failure mode.
# ---------------------------------------------------------------------------
bottom_lane_payload() { # every lane contains the bottom lane's head
	printf '[%s,%s,%s,%s,%s]\n' \
		"$(pr 6332 aaaa111 main open)" \
		"$(pr 6333 bbbb222 codex/lane-json open)" \
		"$(pr 6334 cccc333 codex/lane-hcl open)" \
		"$(pr 6335 dddd444 codex/lane-dbt open)" \
		"$(pr 6336 eeee555 codex/lane-elixir open)" >"${pulls}"
}

upper_lane_payload() { # only lane 6333 and the lanes above it
	printf '[%s,%s,%s,%s]\n' \
		"$(pr 6333 bbbb222 codex/lane-json open)" \
		"$(pr 6334 cccc333 codex/lane-hcl open)" \
		"$(pr 6335 dddd444 codex/lane-dbt open)" \
		"$(pr 6336 eeee555 codex/lane-elixir open)" >"${pulls}"
}

top_lane_payload() { # the top of the train contains only itself
	printf '[%s]\n' "$(pr 6336 eeee555 codex/lane-elixir open)" >"${pulls}"
}

# 1. A stacked lane whose base is another feature branch must resolve to itself,
#    out of a set in which every member is stacked. This is the case the old
#    base.ref filter returned ZERO for.
upper_lane_payload
expect_number "stacked lane with a non-main base resolves to itself" bbbb222 6333

top_lane_payload
expect_number "the top lane of the train resolves to itself" eeee555 6336

# 2. The bottom lane targets main, and its head is contained in all five open
#    PRs. It must resolve by head.sha to itself -- not merely to "the main-based
#    one", which is only accidentally the same answer here and stops being the
#    same answer as soon as two main-based PRs share a commit.
bottom_lane_payload
expect_number "bottom lane resolves by head sha out of the whole train" aaaa111 6332

# 3. Fail closed when nothing matches. A required status must never be published
#    against a guessed PR.
expect_fail_closed "unknown head sha fails closed" ffff999

# 4. Fail closed when more than one open PR shares the head sha.
printf '[%s,%s]\n' "$(pr 7001 dupdup1 main open)" "$(pr 7002 dupdup1 codex/other open)" >"${pulls}"
expect_fail_closed "two open PRs sharing a head sha fail closed" dupdup1

# 5. A closed PR carrying the same head must not be selected.
printf '[%s,%s]\n' "$(pr 7100 cl0sed1 main closed)" "$(pr 7101 cl0sed1 codex/lane open)" >"${pulls}"
expect_number "a closed PR sharing the head is ignored" cl0sed1 7101

printf '[%s]\n' "$(pr 7200 only0ne main closed)" >"${pulls}"
expect_fail_closed "only a closed PR matching the head fails closed" only0ne

# ---------------------------------------------------------------------------
# Regression assertion: the OLD predicate must be demonstrably wrong on the very
# payload the fixed one handles. Without this the suite would still pass if
# someone reverted the selector to base.ref and the stacked fixtures happened to
# be dropped -- the guard would go quiet instead of going red.
# ---------------------------------------------------------------------------
# This is the payload the real failure occurred on: an upper lane, whose
# containing set is entirely stacked. The old filter returns 0 here, which is
# exactly what run 33308583604 recorded as "got 0".
upper_lane_payload
old_zero="$(jq --arg base main \
	'[.[] | select(.state == "open" and .base.ref == $base)] | length' <"${pulls}")"
if [[ "${old_zero}" -ne 0 ]]; then
	echo "FAIL regression control: expected the old base.ref filter to return 0 for an upper lane's all-stacked payload, got ${old_zero}" >&2
	failures=$((failures + 1))
else
	echo "ok - regression control: the old base.ref filter returns 0 on the payload that broke in CI"
fi
expect_number "the fixed selector resolves the payload the old one returned 0 for" bbbb222 6333

# The bottom lane is where the old filter LOOKED correct, and that is why the
# bug survived: main-based PRs kept passing, so the selector appeared to work
# for anyone who only ever opened PRs against main.
bottom_lane_payload
old_bottom="$(jq --arg base main \
	'[.[] | select(.state == "open" and .base.ref == $base)] | length' <"${pulls}")"
if [[ "${old_bottom}" -ne 1 ]]; then
	echo "FAIL regression control: expected the old filter to return 1 for the bottom lane, got ${old_bottom}" >&2
	failures=$((failures + 1))
else
	echo "ok - regression control: the old base.ref filter returns 1 for a main-based lane, which is why the bug stayed hidden"
fi

# ---------------------------------------------------------------------------
# Fork pull requests (#7950). The cases above feed the resolver a payload; these
# drive its own `gh api` fetch through a stub that answers like GitHub does, so a
# wrong endpoint, a dropped --paginate, or a page-merge bug turns them red.
#
# `commits/<sha>/pulls` lists only PRs whose head branch is in the base
# repository. For #7818, whose head lives in smash-og/eshu, it returned [] (live:
# `gh api repos/eshu-hq/eshu/commits/48d4f8c0d031/pulls` -> []), so the PR was
# treated as closed and required-gates-complete was never published on it. The
# base repository's open pull request list does include fork PRs.
# ---------------------------------------------------------------------------
stub_bin="${tmp_dir}/bin"
world="${tmp_dir}/world.json"
mkdir -p "${stub_bin}"
install -m 0755 "${repo_root}/scripts/lib/test-resolve-required-gates-pr-fake-gh.sh" "${stub_bin}/gh"

fork_7818='{"number":7818,"state":"open","head":{"ref":"fix/7762-runtime-ssh-client","sha":"48d4f8c0d0310b738b6cf0e2ee2fc7a5e1cec964","repo":{"full_name":"smash-og/eshu","fork":true}},"base":{"ref":"main","repo":{"full_name":"eshu-hq/eshu","fork":false}}}'
fork_sha=48d4f8c0d0310b738b6cf0e2ee2fc7a5e1cec964

world_with() { # extra PR objects after 150 open same-repo PRs, newest first
	jq -cn --argjson extra "[$(
		IFS=,
		echo "$*"
	)]" '[range(8100; 7950; -1) | {number: ., state: "open",
		head: {ref: "lane/\(.)", sha: "inrepo\(.)", repo: {full_name: "eshu-hq/eshu", fork: false}},
		base: {ref: "main", repo: {full_name: "eshu-hq/eshu", fork: false}}}] + $extra' >"${world}"
}

fetch_resolver() { # head_sha; the resolver's own fetch, through the stub
	PATH="${stub_bin}:${PATH}" GITHUB_REPOSITORY=eshu-hq/eshu ESHU_WORLD_JSON="${world}" \
		HEAD_SHA="$1" "${resolver}"
}

expect_fetch_number() { # label head_sha want
	local label="$1" sha="$2" want="$3" got rc
	got="$(fetch_resolver "${sha}" 2>/dev/null)" && rc=0 || rc=$?
	if [[ "${rc}" -ne 0 || "${got}" != "${want}" ]]; then
		echo "FAIL ${label}: resolver exited ${rc} with '${got}', expected PR ${want}" >&2
		failures=$((failures + 1))
		return
	fi
	echo "ok - ${label}"
}

expect_fetch_fail_closed() { # label head_sha
	local label="$1" sha="$2"
	if fetch_resolver "${sha}" >/dev/null 2>&1; then
		echo "FAIL ${label}: resolver succeeded; it must fail closed" >&2
		failures=$((failures + 1))
		return
	fi
	echo "ok - ${label}"
}

# 6. The fork PR sits on page 2 of the open list, behind 150 newer PRs, so the
#    resolver must page through all of it.
world_with "${fork_7818}"
fork_via_commits="$(PATH="${stub_bin}:${PATH}" GITHUB_REPOSITORY=eshu-hq/eshu ESHU_WORLD_JSON="${world}" \
	gh api "repos/eshu-hq/eshu/commits/${fork_sha}/pulls" | jq 'length')"
if [[ "${fork_via_commits}" -ne 0 ]]; then
	echo "FAIL regression control: commits/<sha>/pulls must model the live API and omit the fork PR, got ${fork_via_commits}" >&2
	failures=$((failures + 1))
else
	echo "ok - regression control: commits/<sha>/pulls returns 0 for the fork head, as it did live for #7818"
fi
expect_fetch_number "an open fork PR on page 2 resolves by head sha" "${fork_sha}" 7818

# 7. A same-repo PR still resolves through the same fetch.
expect_fetch_number "a same-repo PR on page 1 resolves through the open list" inrepo8042 8042

# 8. A fork PR and a same-repo PR sharing one head must stay ambiguous. A lookup
#    that only saw same-repo heads would pick 8042 here.
world_with "$(jq -c '.head.sha = "inrepo8042"' <<<"${fork_7818}")"
expect_fetch_fail_closed "a fork PR and a same-repo PR sharing a head fail closed" inrepo8042

# 9. A closed fork PR is not open, so nothing owns the head.
world_with "$(jq -c '.state = "closed"' <<<"${fork_7818}")"
expect_fetch_fail_closed "a closed fork PR fails closed" "${fork_sha}"

# 10. A PR opened between two page requests shifts the list, so the last PR of
#     one page comes back again at the top of the next. That is one PR, not two.
world_with "${fork_7818}" "${fork_7818}"
expect_fetch_number "a PR listed on two pages resolves once" "${fork_sha}" 7818

if [[ "${failures}" -ne 0 ]]; then
	echo "test-resolve-required-gates-pr: ${failures} case(s) failed" >&2
	exit 1
fi
echo "test-resolve-required-gates-pr: all cases pass"
