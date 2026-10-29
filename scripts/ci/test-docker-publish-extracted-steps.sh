#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="${root}/.github/workflows/docker-publish.yml"
scratch="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
trap 'rm -r -- "${scratch}"' EXIT

workflow_run() {
  local step="$1"
  STEP_NAME="${step}" yq -r '.jobs[] | .steps[]? | select(.name == strenv(STEP_NAME)) | .run' "${workflow}"
}

# The step must be exactly this command. run_selection below runs the same command
# literally, so the proof does not pass the workflow text to a shell as a variable
# (the Docker Hub login gate cannot tell what such a variable runs).
selection_run="$(workflow_run 'Decide release artifact lanes')"
[[ "${selection_run}" == 'bash scripts/ci/select-docker-publish-lanes.sh' ]] || {
  echo 'FAIL: selection step does not execute the production selector' >&2
  exit 1
}
# GitHub expression bindings are literal YAML bytes in this wiring check.
# shellcheck disable=SC2016
for binding in 'PR_BASE_SHA: ${{ github.event.pull_request.base.sha }}' \
  'PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}' \
  'BEFORE_SHA: ${{ github.event.before }}'; do
  rg -Fq "${binding}" "${workflow}" || { echo "FAIL: missing ${binding}" >&2; exit 1; }
done

repo="${scratch}/repo"
mkdir -p "${repo}/scripts/ci"
ln -s "${root}/scripts/ci/select-docker-publish-lanes.sh" "${repo}/scripts/ci/select-docker-publish-lanes.sh"
git -C "${repo}" init -q -b main
git -C "${repo}" config user.name CI
git -C "${repo}" config user.email ci@example.invalid
printf 'FROM scratch\n' > "${repo}/Dockerfile"
git -C "${repo}" add Dockerfile
git -C "${repo}" commit -qm base
base="$(git -C "${repo}" rev-parse HEAD)"
git -C "${repo}" update-ref refs/remotes/origin/main "${base}"
printf 'RUN true\n' >> "${repo}/Dockerfile"
git -C "${repo}" commit -qam image-change
image_head="$(git -C "${repo}" rev-parse HEAD)"

run_selection() {
  local event="$1" head="$2" pr_base="${3:-${base}}" ref_type="${4:-branch}"
  : > "${scratch}/output"
  (cd "${repo}" && GITHUB_OUTPUT="${scratch}/output" GITHUB_EVENT_NAME="${event}" \
    GITHUB_REF_TYPE="${ref_type}" GITHUB_SHA="${head}" PR_BASE_SHA="${pr_base}" \
    PR_HEAD_SHA="${head}" BEFORE_SHA="${base}" bash scripts/ci/select-docker-publish-lanes.sh)
}
# The Helm chart lane moved to eshu-hq/eshu-deploy under #8871: only the
# eshu-side image and apkfloors lanes remain selectable here.
for mode in tag workflow_dispatch; do
  if [[ "${mode}" == tag ]]; then
    run_selection push "${base}" "${base}" tag
  else
    run_selection workflow_dispatch "${base}"
  fi
  for lane in image apkfloors; do
    rg -q "^${lane}=true$" "${scratch}/output"
  done
done
run_selection merge_group "${image_head}"
rg -q '^image=true$' "${scratch}/output"
rg -q '^apkfloors=true$' "${scratch}/output"
mkdir -p "${repo}/go"
printf 'package sample\n' > "${repo}/go/sample.go"
git -C "${repo}" add go/sample.go
git -C "${repo}" commit -qm go
run_selection merge_group "$(git -C "${repo}" rev-parse HEAD)"
rg -q '^image=true$' "${scratch}/output" # Dockerfile is still in merge-base range.
git -C "${repo}" update-ref refs/remotes/origin/main "${image_head}"
run_selection merge_group "$(git -C "${repo}" rev-parse HEAD)"
rg -q '^image=false$' "${scratch}/output"
go_head="$(git -C "${repo}" rev-parse HEAD)"
run_selection pull_request "${go_head}" "${image_head}"
rg -q '^image=true$' "${scratch}/output"
git -C "${repo}" update-ref refs/remotes/origin/main "${go_head}"
run_selection merge_group "${go_head}"
rg -q '^image=false$' "${scratch}/output"
rg -q '^apkfloors=false$' "${scratch}/output"
git -C "${repo}" add scripts/ci/select-docker-publish-lanes.sh
git -C "${repo}" commit -qm selector-helper
selector_head="$(git -C "${repo}" rev-parse HEAD)"
run_selection merge_group "${selector_head}"
rg -q '^image=true$' "${scratch}/output"
rg -q '^apkfloors=true$' "${scratch}/output"
git -C "${repo}" update-ref refs/remotes/origin/main "${selector_head}"

# #7762: the runtime image tool verifier and its suite select the image lane,
# whose verify-reproducibility job runs both against the built rootfs.
# #8878: the scanner image verifier, its suite, and the shared
# rootfs-resolve library they source select the same lane, since the
# scanner-images gate depends on verify-reproducibility running via it.
for tool_script in scripts/verify-runtime-image-tools.sh scripts/test-verify-runtime-image-tools.sh scripts/verify-scanner-images.sh scripts/test-verify-scanner-images.sh scripts/lib/rootfs-resolve.sh; do
  tool_base="$(git -C "${repo}" rev-parse HEAD)"
  mkdir -p "${repo}/$(dirname "${tool_script}")"
  printf '#!/usr/bin/env bash\n' > "${repo}/${tool_script}"
  git -C "${repo}" add "${tool_script}"
  git -C "${repo}" commit -qm "${tool_script##*/}"
  run_selection pull_request "$(git -C "${repo}" rev-parse HEAD)" "${tool_base}"
  rg -q '^image=true$' "${scratch}/output"
  rg -q '^apkfloors=false$' "${scratch}/output"
done
# The reproducibility build asserts the canonical mirror digests from
# specs/ci-images.v1.json and feeds them to the runtime image build, so a
# change to either file must select the image lane (whose
# verify-reproducibility job consumes both). Otherwise the registry selects
# docker-image-reproducibility while every publish job skips, and the
# aggregator fails closed (#8397).
for repro_file in specs/ci-images.v1.json scripts/ci/build-reproducibility-image.sh; do
  repro_base="$(git -C "${repo}" rev-parse HEAD)"
  mkdir -p "${repo}/$(dirname "${repro_file}")"
  printf '# repro\n' > "${repo}/${repro_file}"
  git -C "${repo}" add "${repro_file}"
  git -C "${repo}" commit -qm "${repro_file##*/}"
  run_selection pull_request "$(git -C "${repo}" rev-parse HEAD)" "${repro_base}"
  rg -q '^image=true$' "${scratch}/output"
  rg -q '^apkfloors=false$' "${scratch}/output"
done
printf 'PASS: production workflow selector steps execute with expected event behavior\n'
