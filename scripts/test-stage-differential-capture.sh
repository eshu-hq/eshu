#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/lib/stage-differential-capture.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT

attempt=7
downloads="${fixture}/downloads"
output="${fixture}/staged"
mkdir -p "${downloads}"

expect_failure() {
  local name="$1"
  shift
  if "$@" >"${fixture}/${name}.log" 2>&1; then
    echo "FAIL: ${name} unexpectedly passed" >&2
    exit 1
  fi
}

artifact_dir() {
  printf '%s/differential-capture-%s-%s\n' "${downloads}" "${attempt}" "$1"
}

for leg in pair1-nornicdb pair1-neo4j pair2-nornicdb pair2-neo4j; do
  dir="$(artifact_dir "${leg}")"
  mkdir -p "${dir}"
  printf '{"leg":"%s"}\n' "${leg}" >"${dir}/recording.jsonl"
done

# The join cannot silently accept a missing download or an empty capture.
rm -rf "$(artifact_dir pair2-neo4j)"
expect_failure missing_leg bash "${helper}" stage "${downloads}" "${output}" "${attempt}" success
rg -q 'does not exist' "${fixture}/missing_leg.log"
test ! -d "${output}/pair2/neo4j"
mkdir -p "$(artifact_dir pair2-neo4j)"
expect_failure empty_leg bash "${helper}" stage "${downloads}" "${output}" "${attempt}" success
rg -q 'no non-empty' "${fixture}/empty_leg.log"
expect_failure empty_capture bash "${helper}" check-leg "$(artifact_dir pair2-neo4j)"
rg -q 'no non-empty' "${fixture}/empty_capture.log"
mkdir "$(artifact_dir pair2-neo4j)/fake.jsonl"
expect_failure directory_is_not_recording bash "${helper}" check-leg "$(artifact_dir pair2-neo4j)"
rg -q 'no non-empty' "${fixture}/directory_is_not_recording.log"
rmdir "$(artifact_dir pair2-neo4j)/fake.jsonl"
printf '{"leg":"pair2-neo4j"}\n' >"$(artifact_dir pair2-neo4j)/recording.jsonl"

# A failed matrix capture remains blocking even if four uploads exist.
expect_failure failed_leg bash "${helper}" stage "${downloads}" "${output}" "${attempt}" failure
rg -q 'matrix result is failure' "${fixture}/failed_leg.log"
expect_failure cancelled_leg bash "${helper}" stage "${downloads}" "${output}" "${attempt}" cancelled
rg -q 'matrix result is cancelled' "${fixture}/cancelled_leg.log"
expect_failure stale_attempt bash "${helper}" stage "${downloads}" "${output}" 8 success
rg -q 'does not exist' "${fixture}/stale_attempt.log"

# A clean join reconstructs the four paths consumed by both coverage gates.
printf 'keep\n' >"${fixture}/unrelated"
bash "${helper}" stage "${downloads}" "${output}" "${attempt}" success
for leg in pair1-nornicdb pair1-neo4j pair2-nornicdb pair2-neo4j; do
  pair="${leg%-*}"
  backend="${leg#*-}"
  cmp -s "$(artifact_dir "${leg}")/recording.jsonl" "${output}/${pair}/${backend}/recording.jsonl"
done
test "$(cat "${fixture}/unrelated")" = keep
expect_failure stale_output bash "${helper}" stage "${downloads}" "${output}" "${attempt}" success
rg -q 'already exists' "${fixture}/stale_output.log"

echo 'PASS: differential capture staging is complete, isolated, and fail-closed'
