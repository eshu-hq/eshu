#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wrapper_source="${ESHU_MEASUREMENT_CITATIONS_WRAPPER_SOURCE:-${repo_root}/scripts/dev/precommit-go.sh}"
verifier_source="${ESHU_MEASUREMENT_CITATIONS_VERIFIER_SOURCE:-${repo_root}/scripts/verify-measurement-citations.sh}"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT

git init -q "${fixture}/feature"
git -C "${fixture}/feature" config user.name "Eshu Test"
git -C "${fixture}/feature" config user.email "test@example.invalid"
mkdir -p "${fixture}/feature/docs/internal" "${fixture}/feature/scripts/dev" \
  "${fixture}/feature/.github/workflows"
printf '%s\n' 'golangci-lint@v2.12.2' >"${fixture}/feature/.github/workflows/test.yml"
printf '%s\n' 'gosec@v2.27.1' >"${fixture}/feature/.github/workflows/security-scan.yml"
printf '%s\n' '{"id":"9999-common","value":0,"trials":10}' \
  >"${fixture}/feature/docs/internal/measurements.jsonl"
cp "${wrapper_source}" "${fixture}/feature/scripts/dev/precommit-go.sh"
cp "${verifier_source}" \
  "${fixture}/feature/scripts/verify-measurement-citations.sh"
chmod +x "${fixture}/feature/scripts/verify-measurement-citations.sh"
git -C "${fixture}/feature" add .
git -C "${fixture}/feature" commit -q -m baseline
git -C "${fixture}/feature" branch -M main
git clone -q --bare "${fixture}/feature" "${fixture}/origin.git"
git -C "${fixture}/feature" remote add origin "${fixture}/origin.git"
git -C "${fixture}/feature" fetch -q origin
git -C "${fixture}/feature" checkout -q -b feature
printf '%s\n' 'A cited result: 0/10 trials (ledger:9999-common).' \
  >"${fixture}/feature/finding.md"
git -C "${fixture}/feature" add finding.md
git -C "${fixture}/feature" commit -q -m 'cite common row on feature'

git clone -q "${fixture}/origin.git" "${fixture}/main-writer"
git -C "${fixture}/main-writer" config user.name "Eshu Test"
git -C "${fixture}/main-writer" config user.email "test@example.invalid"
printf '%s\n' '{"id":"9999-main-only","value":1,"trials":10}' \
  >>"${fixture}/main-writer/docs/internal/measurements.jsonl"
git -C "${fixture}/main-writer" add docs/internal/measurements.jsonl
git -C "${fixture}/main-writer" commit -q -m 'append row on main after branch split'
git -C "${fixture}/main-writer" push -q origin main

if ! (cd "${fixture}/feature" && bash scripts/dev/precommit-go.sh measurement-citations) \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  sed -n '1,30p' "${fixture}/gate.err" >&2
  printf 'production wrapper rejected a feature that preserves every common-ancestor ledger row\n' >&2
  exit 1
fi

for mode in explicit ci direct; do
  case "$mode" in
    explicit)
      if ! ESHU_MEASUREMENT_CITATIONS_BASE=origin/main \
          ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/feature" \
          "${fixture}/feature/scripts/verify-measurement-citations.sh" \
          >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
        printf 'explicit moving base incorrectly rejected the feature\n' >&2
        sed -n '1,30p' "${fixture}/gate.err" >&2
        exit 1
      fi
      ;;
    ci)
      if ! env -u ESHU_MEASUREMENT_CITATIONS_BASE GITHUB_BASE_REF=main \
          ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/feature" \
          "${fixture}/feature/scripts/verify-measurement-citations.sh" \
          >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
        printf 'CI base incorrectly rejected the feature\n' >&2
        sed -n '1,30p' "${fixture}/gate.err" >&2
        exit 1
      fi
      ;;
    direct)
      if ! env -u ESHU_MEASUREMENT_CITATIONS_BASE -u GITHUB_BASE_REF \
          ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/feature" \
          "${fixture}/feature/scripts/verify-measurement-citations.sh" \
          >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
        printf 'direct invocation incorrectly rejected the feature\n' >&2
        sed -n '1,30p' "${fixture}/gate.err" >&2
        exit 1
      fi
      ;;
  esac
done

origin_url="$(git -C "${fixture}/feature" remote get-url origin)"
git -C "${fixture}/feature" remote set-url origin "${fixture}/missing-origin.git"
if (cd "${fixture}/feature" && bash scripts/dev/precommit-go.sh measurement-citations) \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  printf 'wrapper accepted a stale origin/main after fetch failed\n' >&2
  exit 1
fi
rg -q 'cannot refresh origin/main' "${fixture}/gate.err"
git -C "${fixture}/feature" remote set-url origin "$origin_url"

if ESHU_MEASUREMENT_CITATIONS_BASE=origin/missing \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/feature" \
    "${fixture}/feature/scripts/verify-measurement-citations.sh" \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  printf 'missing explicit base was accepted\n' >&2
  exit 1
fi
rg -q 'base origin/missing is unavailable' "${fixture}/gate.err"

git clone -q --depth 1 "file://${fixture}/feature" "${fixture}/shallow"
git -C "${fixture}/shallow" fetch -q --depth 1 origin \
  main:refs/remotes/origin/main
if ESHU_MEASUREMENT_CITATIONS_BASE=origin/main \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/shallow" \
    "${fixture}/shallow/scripts/verify-measurement-citations.sh" \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  printf 'shallow history without a common ancestor was accepted\n' >&2
  exit 1
fi
rg -q 'no common ancestor' "${fixture}/gate.err"
if ! env -u ESHU_MEASUREMENT_CITATIONS_BASE GITHUB_BASE_REF=main \
    ESHU_MEASUREMENT_CITATIONS_REPO_ROOT="${fixture}/shallow" \
    "${fixture}/shallow/scripts/verify-measurement-citations.sh" \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  printf 'CI could not restore a shallow PR checkout before comparison\n' >&2
  sed -n '1,30p' "${fixture}/gate.err" >&2
  exit 1
fi

# A common-ancestor row remains protected even after main moves forward.
printf '%s\n' '{"id":"9999-changed","value":0,"trials":10}' \
  >"${fixture}/feature/docs/internal/measurements.jsonl"
git -C "${fixture}/feature" add docs/internal/measurements.jsonl
git -C "${fixture}/feature" commit -q -m 'modify common ledger row'
if (cd "${fixture}/feature" && bash scripts/dev/precommit-go.sh measurement-citations) \
    >"${fixture}/gate.out" 2>"${fixture}/gate.err"; then
  printf 'production wrapper accepted a changed common-ancestor ledger row\n' >&2
  exit 1
fi
rg -q "row '9999-common' was deleted" "${fixture}/gate.err"

printf 'measurement-citations wrapper test passed\n'
