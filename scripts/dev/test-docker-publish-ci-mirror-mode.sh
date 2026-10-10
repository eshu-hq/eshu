#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="${1:-${repo_root}/.github/workflows/docker-publish.yml}"

job_condition() {
  local job="$1"
  awk -v wanted="  ${job}:" '
    /^  ["\047]?[A-Za-z_][A-Za-z0-9_-]*["\047]?:[[:space:]]*(#.*)?$/ { in_job = ($0 == wanted); in_if = 0 }
    in_job && /^    if:/ { in_if = 1; sub(/[[:space:]]+#.*$/, ""); print; next }
    in_if && /^      / { sub(/[[:space:]]+#.*$/, ""); print; next }
    in_if { exit }
  ' "${workflow}"
}

job_body() {
  local job="$1"
  awk -v wanted="  ${job}:" '
    /^  ["\047]?[A-Za-z_][A-Za-z0-9_-]*["\047]?:[[:space:]]*(#.*)?$/ {
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

require_unconditional_step() {
  local step="$1" name="$2"
  [[ -n "${step}" ]] || fail "${name} step is missing"
  if rg -q '^        (if|continue-on-error)[[:space:]]*:' <<< "${step}"; then
    fail "${name} step can be skipped or its failure ignored"
  fi
}

expected_release_condition() {
  local guard="github.event_name != 'workflow_dispatch' || inputs.mode == 'release'"
  case "$1" in
    changes)
      printf '%s\n' "    if: ${guard}"
      ;;
    verify-apk-floors)
      printf '%s\n' '    if: >-' "      (${guard}) &&" \
        "      (github.event_name == 'merge_group' || (github.event_name == 'pull_request' && needs.changes.outputs.apkfloors == 'true'))"
      ;;
    build-and-push-image|verify-reproducibility)
      printf '%s\n' '    if: >-' "      (${guard}) &&" \
        "      (github.event_name == 'merge_group' || needs.changes.outputs.image == 'true')"
      ;;
    promote-moving-tags)
      printf '%s\n' '    if: >-' "      (${guard}) &&" \
        "      needs.changes.outputs.image == 'true' &&" \
        "      needs.build-and-push-image.result == 'success' &&" \
        "      ((github.event_name == 'push' && github.ref == 'refs/heads/main') || github.event_name == 'workflow_dispatch')"
      ;;
    attach-release-sbom)
      printf '%s\n' '    if: >-' "      (${guard}) &&" \
        "      needs.changes.outputs.image == 'true' && github.ref_type == 'tag'"
      ;;
    package-and-push-chart)
      printf '%s\n' '    if: >-' "      (${guard}) &&" \
        "      (github.event_name == 'merge_group' || needs.changes.outputs.chart == 'true')"
      ;;
    *) fail "unknown release job: $1" ;;
  esac
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
  in_jobs && /^  ["\047]?[A-Za-z_][A-Za-z0-9_-]*["\047]?:[[:space:]]*(#.*)?$/ {
    job = $0
    sub(/[[:space:]]+#.*$/, "", job)
    sub(/^[[:space:]]+/, "", job)
    sub(/:[[:space:]]*$/, "", job)
    gsub(/["\047]/, "", job)
    print job
  }
' "${workflow}" | LC_ALL=C sort)"
[[ "${actual_jobs}" == "${expected_jobs}" ]] || fail 'workflow jobs differ from the guarded seven-plus-two partition'

for job in changes verify-apk-floors build-and-push-image promote-moving-tags \
  verify-reproducibility attach-release-sbom package-and-push-chart; do
  condition="$(job_condition "${job}")"
  [[ "${condition}" == "$(expected_release_condition "${job}")" ]] ||
    fail "${job} release condition differs from its guarded exact shape"
done
sbom_condition="$(job_condition attach-release-sbom)"
[[ "${sbom_condition}" == *"needs.changes.outputs.image == 'true' && github.ref_type == 'tag'"* ]] ||
  fail 'release SBOM job no longer requires an image tag release'

for spec in 'publish-ci-service-mirrors:ci-mirrors-publish' \
  'verify-public-ci-service-mirrors:ci-mirrors-verify-public'; do
  job="${spec%%:*}"
  mode="${spec#*:}"
  condition="$(job_condition "${job}")"
  expected_condition="    if: >-
      github.event_name == 'workflow_dispatch' && inputs.mode == '${mode}' &&
      github.repository == 'eshu-hq/eshu' &&
      github.ref == 'refs/heads/main'"
  [[ "${condition}" == "${expected_condition}" ]] ||
    fail "${job} is not limited to its exact mode on eshu-hq/eshu main"
done

publisher_body="$(job_body publish-ci-service-mirrors)"
verifier_body="$(job_body verify-public-ci-service-mirrors)"
for job in publish-ci-service-mirrors verify-public-ci-service-mirrors; do
  body="$(job_body "${job}")"
  crane_step="$(step_body "${body}" 'Install pinned crane')"
  require_unconditional_step "${crane_step}" "${job} pinned crane install"
  rg -Fqx -- '        run: GOBIN="${RUNNER_TEMP}" scripts/ci/go-install-retry.sh github.com/google/go-containerregistry/cmd/crane@v0.20.6' \
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
  require_unconditional_step "$(step_body "${body}" 'Install ripgrep')" "${job} ripgrep install"
  require_unconditional_step "${safety_step}" "${job} publisher safety test"
  rg -q '^        run: bash scripts/dev/test-publish-ci-image-mirrors.sh$' <<< "${safety_step}" ||
    fail "${job} does not run its publisher safety test"
done
copy_step="$(step_body "${publisher_body}" 'Copy exact upstream indexes')"
verify_step="$(step_body "${verifier_body}" 'Verify anonymous digests')"
login_step="$(step_body "${publisher_body}" 'Log in to GHCR')"
require_unconditional_step "${copy_step}" 'publisher copy'
require_unconditional_step "${verify_step}" 'public verification'
require_unconditional_step "${login_step}" 'publisher GHCR login'
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

# Pin each mirror job verbatim, comments and blank lines aside. A step, env key,
# or permission ADDED to the job that holds packages: write and the GHCR login
# passes every targeted check above, so the whole job text must match a fixture.
for spec in 'publish-ci-service-mirrors:ci-image-mirror-publish-job.txt' \
  'verify-public-ci-service-mirrors:ci-image-mirror-verify-public-job.txt'; do
  job="${spec%%:*}"
  fixture="${repo_root}/scripts/dev/fixtures/${spec#*:}"
  [[ -f "${fixture}" ]] || fail "${job} pinned step-list fixture is missing"
  actual="$(job_body "${job}" | awk '!/^[[:space:]]*(#|$)/')"
  [[ "${actual}" == "$(< "${fixture}")" ]] ||
    fail "${job} differs from its pinned step list in scripts/dev/fixtures/${spec#*:}; a changed or added step, env key, or permission needs a reviewed fixture update"
done

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
  awk '
    /^    if: github.event_name != '\''workflow_dispatch'\'' \|\| inputs.mode == '\''release'\''$/ {
      print $0 " || true"
      next
    }
    { print }
  ' "${workflow}" > "${scratch}/release-bypass.yml"
  if bash "$0" "${scratch}/release-bypass.yml" > /dev/null 2>&1; then
    fail 'seeded release condition boolean bypass was not detected'
  fi
  cp "${workflow}" "${scratch}/extra.yml"
  printf '\n  unguarded-extra-job:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo unexpected\n' \
    >> "${scratch}/extra.yml"
  if bash "$0" "${scratch}/extra.yml" > /dev/null 2>&1; then
    fail 'seeded unguarded tenth job was not detected'
  fi
  # Job keys with an underscore, quotes, or a trailing comment must not slip past
  # the partition guard (review-7888 F1).
  for spelling in 'extra_job:' '"quoted-extra-job":' 'extra-job: # comment'; do
    cp "${workflow}" "${scratch}/extra-spelling.yml"
    printf '\n  %s\n    permissions:\n      packages: write\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo unexpected\n' \
      "${spelling}" >> "${scratch}/extra-spelling.yml"
    if out="$(bash "$0" "${scratch}/extra-spelling.yml" 2>&1)"; then
      fail "seeded job header spelling '${spelling}' was not detected"
    fi
    [[ "${out}" == *'workflow jobs differ'* ]] ||
      fail "seeded job header spelling '${spelling}' failed for a reason other than the partition guard"
  done

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
  awk '
    { print }
    /^      - name: Copy exact upstream indexes$/ { print "        if: false" }
  ' "${workflow}" > "${scratch}/disabled-copy.yml"
  if bash "$0" "${scratch}/disabled-copy.yml" > /dev/null 2>&1; then
    fail 'seeded disabled real publisher copy was not detected'
  fi
  awk '
    { print }
    /^      - name: Copy exact upstream indexes$/ { print "        if : false" }
  ' "${workflow}" > "${scratch}/disabled-copy-spaced-colon.yml"
  if bash "$0" "${scratch}/disabled-copy-spaced-colon.yml" > /dev/null 2>&1; then
    fail 'seeded spaced-colon disabled publisher copy was not detected'
  fi
  awk '
    { print }
    /^      - name: Verify anonymous digests$/ { print "        if: false" }
  ' "${workflow}" > "${scratch}/disabled-verify.yml"
  if bash "$0" "${scratch}/disabled-verify.yml" > /dev/null 2>&1; then
    fail 'seeded disabled real public verification was not detected'
  fi
  awk '
    { print }
    /^      - name: Test publisher safety contract$/ { print "        if: false" }
  ' "${workflow}" > "${scratch}/disabled-safety.yml"
  if bash "$0" "${scratch}/disabled-safety.yml" > /dev/null 2>&1; then
    fail 'seeded disabled publisher safety test was not detected'
  fi
  awk '
    { print }
    /^      - name: Copy exact upstream indexes$/ { print "        continue-on-error: true" }
  ' "${workflow}" > "${scratch}/ignored-copy-error.yml"
  if bash "$0" "${scratch}/ignored-copy-error.yml" > /dev/null 2>&1; then
    fail 'seeded ignored publisher copy error was not detected'
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

  # An ADDED step, env key, or permission must trip the step-list pin and no
  # other check: each case also asserts the pin's own message.
  expect_pin_violation() {
    local file="$1" label="$2" out
    if out="$(bash "$0" "${file}" 2>&1)"; then
      fail "seeded ${label} was not detected"
    fi
    [[ "${out}" == *'differs from its pinned step list'* ]] ||
      fail "seeded ${label} failed for a reason other than the step-list pin"
  }
  awk '
    /^      - name: Copy exact upstream indexes$/ {
      print "      - name: Added publisher step"
      print "        run: echo added"
    }
    { print }
  ' "${workflow}" > "${scratch}/publisher-added-step.yml"
  expect_pin_violation "${scratch}/publisher-added-step.yml" 'step added to the publisher job'
  awk '
    /^  verify-public-ci-service-mirrors:$/ {
      print "      - name: Added trailing publisher step"
      print "        run: echo added"
    }
    { print }
  ' "${workflow}" > "${scratch}/publisher-trailing-step.yml"
  expect_pin_violation "${scratch}/publisher-trailing-step.yml" 'trailing step added to the publisher job'
  awk '
    /^      - name: Verify anonymous digests$/ {
      print "      - name: Added verifier step"
      print "        run: echo added"
    }
    { print }
  ' "${workflow}" > "${scratch}/verifier-added-step.yml"
  expect_pin_violation "${scratch}/verifier-added-step.yml" 'step added to the verifier job'
  cp "${workflow}" "${scratch}/verifier-trailing-step.yml"
  printf '      - name: Added trailing verifier step\n        run: echo added\n' \
    >> "${scratch}/verifier-trailing-step.yml"
  expect_pin_violation "${scratch}/verifier-trailing-step.yml" 'trailing step added to the verifier job'
  awk '
    { print }
    /^          EXPECTED_REVIEWED_SHA: / { print "          EXTRA_TOKEN: ${{ secrets.GITHUB_TOKEN }}" }
  ' "${workflow}" > "${scratch}/publisher-added-env.yml"
  expect_pin_violation "${scratch}/publisher-added-env.yml" 'env key added to the publisher copy step'
  awk '
    /^  publish-ci-service-mirrors:$/ { in_publisher = 1 }
    /^  verify-public-ci-service-mirrors:$/ { in_publisher = 0 }
    { print }
    in_publisher && /^      packages: write$/ { print "      id-token: write" }
  ' "${workflow}" > "${scratch}/publisher-added-permission.yml"
  expect_pin_violation "${scratch}/publisher-added-permission.yml" 'permission added to the publisher job'
fi

printf 'PASS: mirror jobs preserve calls, SHA fence, and least-privilege permissions; release jobs stay excluded\n'
