#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="${root}/.github/workflows/docker-publish.yml"
scratch="$(mktemp -d)"
trap 'rm -r -- "${scratch}"' EXIT

workflow_run() {
  local step="$1"
  STEP_NAME="${step}" yq -r '.jobs[] | .steps[]? | select(.name == strenv(STEP_NAME)) | .run' "${workflow}"
}

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
mkdir -p "${repo}/scripts/ci" "${repo}/deploy/helm/eshu"
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
git -C "${repo}" commit -qam docker
image_head="$(git -C "${repo}" rev-parse HEAD)"

run_selection() {
  local event="$1" head="$2" pr_base="${3:-${base}}"
  : > "${scratch}/output"
  (cd "${repo}" && GITHUB_OUTPUT="${scratch}/output" GITHUB_EVENT_NAME="${event}" \
    GITHUB_REF_TYPE=branch GITHUB_SHA="${head}" PR_BASE_SHA="${pr_base}" \
    PR_HEAD_SHA="${head}" BEFORE_SHA="${base}" bash -c "${selection_run}")
}
run_selection merge_group "${image_head}"
rg -q '^image=true$' "${scratch}/output"
rg -q '^chart=false$' "${scratch}/output"
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
rg -q '^chart=false$' "${scratch}/output"
rg -q '^apkfloors=false$' "${scratch}/output"
git -C "${repo}" add scripts/ci/select-docker-publish-lanes.sh
git -C "${repo}" commit -qm selector-helper
selector_head="$(git -C "${repo}" rev-parse HEAD)"
run_selection merge_group "${selector_head}"
rg -q '^image=true$' "${scratch}/output"
rg -q '^chart=true$' "${scratch}/output"
rg -q '^apkfloors=true$' "${scratch}/output"
git -C "${repo}" update-ref refs/remotes/origin/main "${selector_head}"

chart_run="$(workflow_run 'Push Helm chart to GHCR')"
[[ "${chart_run}" == 'bash scripts/ci/push-helm-chart.sh' ]] || {
  echo 'FAIL: chart step does not execute the production publisher' >&2
  exit 1
}
mkdir -p "${repo}/dist"
ln -s "${root}/scripts/ci/push-helm-chart.sh" "${repo}/scripts/ci/push-helm-chart.sh"
git -C "${repo}" add scripts/ci/push-helm-chart.sh
git -C "${repo}" commit -qm chart-helper
run_selection merge_group "$(git -C "${repo}" rev-parse HEAD)"
rg -q '^image=false$' "${scratch}/output"
rg -q '^chart=true$' "${scratch}/output"
rg -q '^apkfloors=false$' "${scratch}/output"
printf 'version: 1.2.3\n' > "${repo}/deploy/helm/eshu/Chart.yaml"
printf 'chart bytes\n' > "${repo}/dist/eshu-1.2.3.tgz"
cat > "${scratch}/helm" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  'registry login') exit 0 ;;
  'push '*)
    if [[ "${HELM_MODE}" == success ]]; then exit 0; fi
    exit 27
    ;;
  'pull '*)
    while [[ "$#" -gt 0 ]]; do
      if [[ "$1" == --destination ]]; then shift; destination="$1"; break; fi
      shift
    done
STUB
cat >> "${scratch}/helm" <<'STUB'
    if [[ "${HELM_MODE}" == missing ]]; then exit 1; fi
    if [[ "${HELM_MODE}" == duplicate ]]; then
      cp dist/eshu-1.2.3.tgz "${destination}/eshu-1.2.3.tgz"
    else
      printf 'different bytes\n' > "${destination}/eshu-1.2.3.tgz"
    fi
    ;;
  *) exit 2 ;;
esac
STUB
chmod +x "${scratch}/helm"
run_chart() {
  (cd "${repo}" && PATH="${scratch}:${PATH}" HELM_MODE="$1" CHART_PATH=deploy/helm/eshu \
    CHART_REGISTRY=oci://ghcr.io/eshu-hq/charts REGISTRY=ghcr.io \
    GITHUB_ACTOR=ci GITHUB_TOKEN=fake bash -c "${chart_run}")
}
run_chart success > "${scratch}/success.log"
run_chart duplicate > "${scratch}/duplicate.log"
rg -q 'already published with identical bytes' "${scratch}/duplicate.log"
if run_chart conflict > "${scratch}/conflict.log" 2>&1; then
  echo 'FAIL: changed published chart was accepted' >&2
  exit 1
fi
rg -q 'differs \(or is unreadable\)' "${scratch}/conflict.log"
if run_chart missing > "${scratch}/missing.log" 2>&1; then
  echo 'FAIL: unreadable published chart was accepted' >&2
  exit 1
fi
printf 'PASS: production workflow selector and chart publisher steps execute with expected event and duplicate behavior\n'
