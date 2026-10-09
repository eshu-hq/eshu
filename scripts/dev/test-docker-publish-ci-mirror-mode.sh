#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="${1:-${repo_root}/.github/workflows/docker-publish.yml}"

job_condition() {
  local job="$1"
  awk -v wanted="  ${job}:" '
    /^  [a-zA-Z][a-zA-Z0-9-]*:$/ { in_job = ($0 == wanted); in_if = 0 }
    in_job && /^    if:/ { in_if = 1; sub(/[[:space:]]+#.*$/, ""); print; next }
    in_if && /^      / { sub(/[[:space:]]+#.*$/, ""); print; next }
    in_if { exit }
  ' "${workflow}"
}

job_body() {
  local job="$1"
  awk -v wanted="  ${job}:" '
    /^  [a-zA-Z][a-zA-Z0-9-]*:$/ {
      if (in_job) exit
      in_job = ($0 == wanted)
    }
    in_job { print }
  ' "${workflow}"
}

step_body() {
  local wanted="      - name: $2"
  awk -v wanted="${wanted}" '
    /^      - (name|uses):/ {
      if (in_step) exit
      in_step = ($0 == wanted)
    }
    in_step { print }
  ' <<< "$1"
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

rg -q '^  workflow_dispatch:$' "${workflow}" || fail 'manual dispatch missing'
rg -q '^        default: release$' "${workflow}" || fail 'normal dispatch default changed'

# An added job cannot silently bypass the mirror-mode partition.
expected_jobs="$(printf '%s\n' changes verify-apk-floors build-and-push-image \
  promote-moving-tags verify-reproducibility attach-release-sbom \
  package-and-push-chart publish-ci-service-mirrors \
  verify-public-ci-service-mirrors | LC_ALL=C sort)"
actual_jobs="$(awk '
  /^jobs:$/ { in_jobs = 1; next }
  in_jobs && /^  [a-zA-Z][a-zA-Z0-9-]*:$/ {
    job = $1
    sub(/:$/, "", job)
    print job
  }
' "${workflow}" | LC_ALL=C sort)"
[[ "${actual_jobs}" == "${expected_jobs}" ]] || fail 'workflow jobs differ from the guarded seven-plus-two partition'

for job in changes verify-apk-floors build-and-push-image promote-moving-tags \
  verify-reproducibility attach-release-sbom package-and-push-chart; do
  condition="$(job_condition "${job}")"
  [[ "${condition}" == *"github.event_name != 'workflow_dispatch' || inputs.mode == 'release'"* ]] ||
    fail "${job} does not exclude mirror dispatches and allow release dispatches"
done
sbom_condition="$(job_condition attach-release-sbom)"
[[ "${sbom_condition}" == *"needs.changes.outputs.image == 'true' && github.ref_type == 'tag'"* ]] ||
  fail 'release SBOM job no longer requires an image tag release'

for spec in 'publish-ci-service-mirrors:ci-mirrors-publish' \
  'verify-public-ci-service-mirrors:ci-mirrors-verify-public'; do
  job="${spec%%:*}"
  mode="${spec#*:}"
  condition="$(job_condition "${job}")"
  [[ "${condition}" == *"github.event_name == 'workflow_dispatch' && inputs.mode == '${mode}'"* ]] ||
    fail "${job} does not require its exact manual mode"
  [[ "${condition}" == *"github.repository == 'eshu-hq/eshu'"* ]] ||
    fail "${job} does not require the parent repository"
  [[ "${condition}" == *"github.ref == 'refs/heads/fix/ci-owned-image-mirror-20261009'"* ]] ||
    fail "${job} cannot run on the reviewed bootstrap branch"
done

publisher_body="$(job_body publish-ci-service-mirrors)"
verifier_body="$(job_body verify-public-ci-service-mirrors)"
for job in publish-ci-service-mirrors verify-public-ci-service-mirrors; do
  body="$(job_body "${job}")"
  crane_step="$(step_body "${body}" 'Install pinned crane')"
  rg -Fqx -- '        run: GOBIN="${RUNNER_TEMP}" go install github.com/google/go-containerregistry/cmd/crane@v0.20.6' \
    <<< "${crane_step}" || fail "${job} lacks the pinned crane install command"
  crane_line="$(rg -n -m1 '^      - name: Install pinned crane$' <<< "${body}")"
  rg -Uq '^      - name: Install ripgrep\n        run: scripts/ci/install-apt-packages.sh ripgrep$' \
    <<< "${body}" || fail "${job} lacks the pinned ripgrep installer"
  installer_line="$(rg -n -m1 '^      - name: Install ripgrep$' <<< "${body}")"
  test_line="$(rg -n -m1 '^      - name: Test publisher safety contract$' <<< "${body}")"
  [[ -n "${test_line}" && "${crane_line%%:*}" -lt "${test_line%%:*}" &&
    "${installer_line%%:*}" -lt "${test_line%%:*}" ]] ||
    fail "${job} installs a tool after its safety test"
  safety_step="$(step_body "${body}" 'Test publisher safety contract')"
  rg -q '^        run: bash scripts/dev/test-publish-ci-image-mirrors.sh$' <<< "${safety_step}" ||
    fail "${job} does not run its publisher safety test"
done
copy_step="$(step_body "${publisher_body}" 'Copy exact upstream indexes')"
verify_step="$(step_body "${verifier_body}" 'Verify anonymous digests')"
login_step="$(step_body "${publisher_body}" 'Log in to GHCR')"
rg -q '^          CRANE_BIN: \$\{\{ runner.temp \}\}/crane$' <<< "${copy_step}" ||
  fail 'publisher copy step lacks the pinned crane binary binding'
rg -q '^          CRANE_BIN: \$\{\{ runner.temp \}\}/crane$' <<< "${verify_step}" ||
  fail 'public verifier step lacks the pinned crane binary binding'
copy_line="$(rg -n -m1 '^      - name: Copy exact upstream indexes$' <<< "${publisher_body}")"
login_line="$(rg -n -m1 '^      - name: Log in to GHCR$' <<< "${publisher_body}")"
verify_line="$(rg -n -m1 '^      - name: Verify anonymous digests$' <<< "${verifier_body}")"
publish_test_line="$(rg -n -m1 '^      - name: Test publisher safety contract$' <<< "${publisher_body}")"
verify_test_line="$(rg -n -m1 '^      - name: Test publisher safety contract$' <<< "${verifier_body}")"
[[ -n "${copy_line}" && -n "${login_line}" &&
  "${publish_test_line%%:*}" -lt "${login_line%%:*}" &&
  "${login_line%%:*}" -lt "${copy_line%%:*}" ]] ||
  fail 'publisher safety test and login must precede the copy'
[[ -n "${verify_line}" && "${verify_test_line%%:*}" -lt "${verify_line%%:*}" ]] ||
  fail 'public safety test must precede verification'
rg -q '^        run: bash scripts/dev/publish-ci-image-mirrors.sh publish$' <<< "${copy_step}" ||
  fail 'publisher job does not run the pinned publisher'
rg -q '^          EXPECTED_REVIEWED_SHA: \$\{\{ inputs.expected_sha \}\}$' <<< "${copy_step}" ||
  fail 'publisher job does not bind the reviewed SHA input'
rg -q '^      packages: write$' <<< "${publisher_body}" ||
  fail 'publisher job lacks package-write permission'
rg -q '^        uses: docker/login-action@v3$' <<< "${login_step}" ||
  fail 'publisher job lacks GHCR login'
rg -q '^          password: \$\{\{ secrets.GITHUB_TOKEN \}\}$' <<< "${login_step}" ||
  fail 'publisher login lacks the job-scoped GitHub token'

rg -q '^        run: bash scripts/dev/publish-ci-image-mirrors.sh verify-public$' <<< "${verify_step}" ||
  fail 'public verifier job does not run anonymous verification'
rg -q '^      contents: read$' <<< "${verifier_body}" ||
  fail 'public verifier job lacks read-only contents permission'
if rg -q '^      packages:|docker/login-action|secrets.GITHUB_TOKEN' <<< "${verifier_body}"; then
  fail 'public verifier job can access GHCR credentials or packages permission'
fi

if [[ "$#" -eq 0 ]]; then
  scratch="$(mktemp -d)"
  trap 'rm -r -- "${scratch}"' EXIT
  sed "/^  changes:/,/^  verify-apk-floors:/s/inputs.mode == 'release'/inputs.mode == 'ci-mirrors-publish'/" \
    "${workflow}" > "${scratch}/bad.yml"
  if bash "$0" "${scratch}/bad.yml" > /dev/null 2>&1; then
    fail 'seeded mirror-dispatch violation was not detected'
  fi
  awk '
    /^  changes:$/ { in_changes = 1 }
    in_changes && /^    if:/ {
      print "    if: true"
      print "      # github.event_name != '\''workflow_dispatch'\'' || inputs.mode == '\''release'\''"
      in_changes = 0
      next
    }
    { print }
  ' "${workflow}" > "${scratch}/comment-only-release.yml"
  if bash "$0" "${scratch}/comment-only-release.yml" > /dev/null 2>&1; then
    fail 'seeded comment-only release isolation was not detected'
  fi
  cp "${workflow}" "${scratch}/extra.yml"
  printf '\n  unguarded-extra-job:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo unexpected\n' \
    >> "${scratch}/extra.yml"
  if bash "$0" "${scratch}/extra.yml" > /dev/null 2>&1; then
    fail 'seeded unguarded tenth job was not detected'
  fi

  sed '/^      - name: Install ripgrep$/,+1d' "${workflow}" > "${scratch}/no-rg.yml"
  if bash "$0" "${scratch}/no-rg.yml" > /dev/null 2>&1; then
    fail 'seeded missing ripgrep installer was not detected'
  fi
  awk '
    /^      - name: Install ripgrep$/ { held = $0; getline; held = held ORS $0; next }
    /^      - name: Test publisher safety contract$/ {
      print
      getline
      print
      print held
      held = ""
      next
    }
    { print }
  ' "${workflow}" > "${scratch}/late-rg.yml"
  if bash "$0" "${scratch}/late-rg.yml" > /dev/null 2>&1; then
    fail 'seeded late ripgrep installer was not detected'
  fi
  sed '/^          CRANE_BIN: /d' "${workflow}" > "${scratch}/no-crane-bin.yml"
  if bash "$0" "${scratch}/no-crane-bin.yml" > /dev/null 2>&1; then
    fail 'seeded missing crane binary binding was not detected'
  fi
  sed '/^      - name: Install pinned crane$/,+1d' "${workflow}" > "${scratch}/no-crane-install.yml"
  if bash "$0" "${scratch}/no-crane-install.yml" > /dev/null 2>&1; then
    fail 'seeded missing pinned crane installation was not detected'
  fi
  sed 's|run: bash scripts/dev/test-publish-ci-image-mirrors.sh|run: echo bypassed|' \
    "${workflow}" > "${scratch}/no-safety-test.yml"
  if bash "$0" "${scratch}/no-safety-test.yml" > /dev/null 2>&1; then
    fail 'seeded missing publisher safety-test call was not detected'
  fi
  awk '
    /^      - name: Copy exact upstream indexes$/ {
      print "      - name: Disabled decoy copy"
      print "        if: false"
      print "        run: bash scripts/dev/publish-ci-image-mirrors.sh publish"
      print
      in_copy = 1
      next
    }
    in_copy && /^        run: bash scripts\/dev\/publish-ci-image-mirrors.sh publish$/ {
      print "        run: echo bypassed"
      in_copy = 0
      next
    }
    { print }
  ' "${workflow}" > "${scratch}/decoy-copy.yml"
  if bash "$0" "${scratch}/decoy-copy.yml" > /dev/null 2>&1; then
    fail 'seeded disabled decoy copy hid a bypassed publisher operation'
  fi
  sed "/^  attach-release-sbom:/,/^  package-and-push-chart:/s/github.ref_type == 'tag'/github.ref_type == 'branch'/" \
    "${workflow}" > "${scratch}/sbom-branch.yml"
  awk '
    /^  attach-release-sbom:$/ { in_sbom = 1 }
    in_sbom && /^    if: >-$/ {
      print
      print "      # needs.changes.outputs.image == '\''true'\'' && github.ref_type == '\''tag'\''"
      in_sbom = 0
      next
    }
    { print }
  ' "${scratch}/sbom-branch.yml" > "${scratch}/sbom-comment.yml"
  if bash "$0" "${scratch}/sbom-comment.yml" > /dev/null 2>&1; then
    fail 'seeded release SBOM tag-guard removal was not detected'
  fi

  sed 's|run: bash scripts/dev/publish-ci-image-mirrors.sh publish|run: echo bypassed|' \
    "${workflow}" > "${scratch}/no-publish.yml"
  if bash "$0" "${scratch}/no-publish.yml" > /dev/null 2>&1; then
    fail 'seeded missing publisher call was not detected'
  fi
  sed 's|run: bash scripts/dev/publish-ci-image-mirrors.sh verify-public|run: echo bypassed|' \
    "${workflow}" > "${scratch}/no-verify.yml"
  if bash "$0" "${scratch}/no-verify.yml" > /dev/null 2>&1; then
    fail 'seeded missing public verification call was not detected'
  fi
  sed 's/EXPECTED_REVIEWED_SHA: \${{ inputs.expected_sha }}/EXPECTED_REVIEWED_SHA: \${{ github.sha }}/' \
    "${workflow}" > "${scratch}/unfenced.yml"
  if bash "$0" "${scratch}/unfenced.yml" > /dev/null 2>&1; then
    fail 'seeded unreviewed SHA binding was not detected'
  fi
  sed "/^  publish-ci-service-mirrors:/,/^  verify-public-ci-service-mirrors:/s/^      packages: write$/      packages: read/" \
    "${workflow}" > "${scratch}/no-write.yml"
  if bash "$0" "${scratch}/no-write.yml" > /dev/null 2>&1; then
    fail 'seeded missing publisher permission was not detected'
  fi
  sed "/^  publish-ci-service-mirrors:/,/^  verify-public-ci-service-mirrors:/s|uses: docker/login-action@v3|uses: actions/checkout@v5|" \
    "${workflow}" > "${scratch}/no-login.yml"
  if bash "$0" "${scratch}/no-login.yml" > /dev/null 2>&1; then
    fail 'seeded missing publisher login was not detected'
  fi
  cp "${workflow}" "${scratch}/verify-login.yml"
  printf '      - uses: docker/login-action@v3\n' >> "${scratch}/verify-login.yml"
  if bash "$0" "${scratch}/verify-login.yml" > /dev/null 2>&1; then
    fail 'seeded verifier GHCR login was not detected'
  fi
  awk '
    { print }
    /^  verify-public-ci-service-mirrors:$/ { in_verifier = 1 }
    in_verifier && /^      contents: read$/ { print "      packages: write"; in_verifier = 0 }
  ' "${workflow}" > "${scratch}/verify-write.yml"
  if bash "$0" "${scratch}/verify-write.yml" > /dev/null 2>&1; then
    fail 'seeded verifier package-write permission was not detected'
  fi
fi

printf 'PASS: mirror jobs preserve calls, SHA fence, and least-privilege permissions; release jobs stay excluded\n'
