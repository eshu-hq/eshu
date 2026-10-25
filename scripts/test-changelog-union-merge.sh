#!/usr/bin/env bash
#
# test-changelog-union-merge.sh - seeded two-branch proof that the repository's
# .gitattributes line `CHANGELOG.md merge=union` removes the keep-both
# conflicts that parallel PRs hit under `## Unreleased` (#9096, part of #9092).
#
# Each case builds a throwaway repo, commits a CHANGELOG.md with an
# `## Unreleased` heading, then has two branches add a different entry in the
# same spot and rebases or merges one onto the other.
#
#   control   no merge attribute: the rebase and the merge MUST conflict. This
#             proves the seed reproduces the real conflict, so the attribute
#             is what removes it.
#   union     the repository's real .gitattributes is copied in: the same
#             rebase and merge MUST apply cleanly, keep both entries, and pass
#             scripts/verify-changelog-unreleased.sh.
#
# Two insertion spots are seeded: directly under `## Unreleased` (where new
# entries land) and at the end of the file (where appenders land).
#
# The union cases read the real .gitattributes, so deleting the attribute line
# turns them RED.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
validator="${repo_root}/scripts/verify-changelog-unreleased.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

# Keep the scratch repo independent of the host's git configuration.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=seed GIT_AUTHOR_EMAIL=seed@example.invalid
export GIT_COMMITTER_NAME=seed GIT_COMMITTER_EMAIL=seed@example.invalid
export GIT_EDITOR=true

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# seed_repo <dir> <attributes-file|""> builds a repo whose main holds a base
# CHANGELOG.md, with the attributes file committed on main when one is given.
seed_repo() {
  local dir="$1" attrs="$2"
  mkdir -p "${dir}"
  git -C "${dir}" init -q -b main
  printf '%s\n' '# Changelog' '' '## Unreleased' '' \
    '### Base section' '' '- **Base entry one.** Existing text.' '' \
    '- **Base entry two.** Existing text.' >"${dir}/CHANGELOG.md"
  if [[ -n "${attrs}" ]]; then
    cp "${attrs}" "${dir}/.gitattributes"
    git -C "${dir}" add .gitattributes
  fi
  git -C "${dir}" add CHANGELOG.md
  git -C "${dir}" commit -q -m base
}

# add_entry <dir> <spot> <title> appends an entry, either right under the
# `## Unreleased` heading ("top") or at the end of the file ("end"), and
# commits it on the current branch.
add_entry() {
  local dir="$1" spot="$2" title="$3"
  local tmp="${dir}/CHANGELOG.md.new"
  if [[ "${spot}" == top ]]; then
    awk -v t="${title}" '{ print } /^## Unreleased$/ { print ""; print "### " t; print ""; print "- **" t ".** Added by " t "." }' \
      "${dir}/CHANGELOG.md" >"${tmp}"
    mv "${tmp}" "${dir}/CHANGELOG.md"
  else
    printf '\n%s\n' "- **${title}.** Added by ${title}." >>"${dir}/CHANGELOG.md"
  fi
  git -C "${dir}" commit -q -am "add ${title}"
}

# run_case <label> <attrs|""> <spot> <op> <expect: conflict|clean>
run_case() {
  local label="$1" attrs="$2" spot="$3" op="$4" expect="$5"
  local dir="${tmp_root}/${label}"
  seed_repo "${dir}" "${attrs}"
  git -C "${dir}" checkout -q -b feature-a
  add_entry "${dir}" "${spot}" "Alpha change"
  git -C "${dir}" checkout -q main
  git -C "${dir}" checkout -q -b feature-b
  add_entry "${dir}" "${spot}" "Bravo change"
  local rc=0
  if [[ "${op}" == rebase ]]; then
    git -C "${dir}" rebase feature-a >/dev/null 2>&1 || rc=$?
  else
    git -C "${dir}" merge --no-edit feature-a >/dev/null 2>&1 || rc=$?
  fi
  if [[ "${expect}" == conflict ]]; then
    if [[ "${rc}" -ne 0 ]] && rg -q '^<<<<<<<' "${dir}/CHANGELOG.md"; then
      record_pass "${label}: ${op} conflicts without the attribute"
    else
      record_fail "${label}: expected a ${op} conflict (rc=${rc})"
    fi
    return
  fi
  if [[ "${rc}" -ne 0 ]]; then
    record_fail "${label}: ${op} did not apply cleanly (rc=${rc})"
    return
  fi
  if rg -q 'Alpha change\.\*\*' "${dir}/CHANGELOG.md" &&
    rg -q 'Bravo change\.\*\*' "${dir}/CHANGELOG.md"; then
    record_pass "${label}: ${op} applied with no conflict and kept both entries"
  else
    record_fail "${label}: ${op} lost an entry"
  fi
  if [[ ! -f "${validator}" ]] || ! bash "${validator}" "${dir}/CHANGELOG.md" >/dev/null 2>&1; then
    record_fail "${label}: merged file does not pass verify-changelog-unreleased.sh"
  else
    record_pass "${label}: merged file passes verify-changelog-unreleased.sh"
  fi
}

attrs_file="${repo_root}/.gitattributes"
for spot in top end; do
  for op in rebase merge; do
    run_case "control-${spot}-${op}" "" "${spot}" "${op}" conflict
    run_case "union-${spot}-${op}" "${attrs_file}" "${spot}" "${op}" clean
  done
done

# The real attribute must be the exact rule the union cases depend on, anchored
# to the repository root: a pattern with no slash matches the basename at any
# depth and would also union the SDK changelogs, which the gate never reads.
if rg -q '^/CHANGELOG\.md[[:space:]]+merge=union([[:space:]]|$)' "${attrs_file}"; then
  record_pass "repo .gitattributes declares /CHANGELOG.md merge=union anchored to the root"
else
  record_fail "repo .gitattributes does not declare /CHANGELOG.md merge=union anchored to the root"
fi

# git itself must resolve the root file to union and every other tracked
# CHANGELOG.md (the SDK changelogs) to not-union.
attr_of() { git -C "${repo_root}" check-attr merge -- "$1" | awk -F': ' '{ print $3 }'; }
if [[ "$(attr_of CHANGELOG.md)" == union ]]; then
  record_pass "git check-attr: root CHANGELOG.md is union"
else
  record_fail "git check-attr: root CHANGELOG.md is not union"
fi
nested="$(git -C "${repo_root}" ls-files -- '*/CHANGELOG.md')"
if [[ -z "${nested}" ]]; then
  record_fail "no nested CHANGELOG.md is tracked, so the anchoring case checks nothing"
fi
while IFS= read -r path; do
  [[ -n "${path}" ]] || continue
  if [[ "$(attr_of "${path}")" == union ]]; then
    record_fail "git check-attr: ${path} is union (the root pattern must be anchored)"
  else
    record_pass "git check-attr: ${path} is not union"
  fi
done <<<"${nested}"

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
