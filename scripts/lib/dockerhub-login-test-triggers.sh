#!/usr/bin/env bash
#
# dockerhub-login-test-triggers.sh - seeded RED/GREEN cases for the queue-path
# trigger set and the workflow file extensions of
# scripts/verify-dockerhub-login.sh (#7886). Sourced, never run, from
# scripts/test-verify-dockerhub-login.sh. It uses queue_tree, qjob, steps_run,
# ghcr_digest and queue_wf from the queue lib and the test's own expect_green.
#
# shellcheck disable=SC2154  # failures, run_verifier, ghcr_digest come from the test

hub_job='{"j": {"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18"}}, "steps": [{"run": "echo ok"}]}}'
hub_creds_job='{"j": {"runs-on": "ubuntu-latest", "services": {"db": {"image": "postgres:18", "credentials": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}}, "steps": [{"run": "echo ok"}]}}'

# write_wf <dir> <file> <name> <on json> <jobs json>: one workflow file.
write_wf() {
  yq e -n ".name = \"$3\" | .on = $4 | .jobs = $5" >"$1/.github/workflows/$2"
}

# expect_wf_red <case name> <dir> <fragment>...: one verifier run; every fragment
# (a file:job) must be named with the queue-path message.
expect_wf_red() {
  local name="$1" dir="$2" out frag
  shift 2
  if out="$(run_verifier "${dir}")"; then
    printf 'not ok - %s: verifier passed planted queue-path violations\n' "${name}"; failures=$((failures + 1))
    return 0
  fi
  for frag in "$@"; do
    if [[ "${out}" == *"${frag}: queue-path job"* ]]; then
      printf 'ok - %s: %s\n' "${name}" "${frag}"
    else
      printf 'not ok - %s: %s was not named as a queue-path job in:\n%s\n' "${name}" "${frag}" "${out}"; failures=$((failures + 1))
    fi
  done
}

# --- F5: .yaml workflow files are read ---------------------------------------------
queue_tree trig-yaml-red
d="${QUEUE_DIR}"
write_wf "${d}" fixture-yaml.yaml fixture-yaml '{"pull_request": null}' "${hub_job}"
write_wf "${d}" fixture-yaml-login.yaml fixture-yaml-login '{"workflow_dispatch": null}' '{"j": {"runs-on": "ubuntu-latest", "steps": [{"run": "docker pull postgres:18"}]}}'
if out="$(run_verifier "${d}")"; then
  printf 'not ok - yaml-workflows-are-scanned: verifier passed\n'; failures=$((failures + 1))
elif [[ "${out}" == *'fixture-yaml.yaml:j: queue-path job'* && "${out}" == *'fixture-yaml-login.yaml:j: no Docker Hub docker/login-action@v3 step'* ]]; then
  echo 'ok - yaml-workflows-are-scanned: queue rule and login rule both name the .yaml file'
else
  printf 'not ok - yaml-workflows-are-scanned: unexpected output:\n%s\n' "${out}"; failures=$((failures + 1))
fi
queue_tree trig-yaml-green
d="${QUEUE_DIR}"
write_wf "${d}" fixture-yaml.yaml fixture-yaml '{"pull_request": null}' '{"j": {"runs-on": "ubuntu-latest", "steps": [{"run": "docker pull '"${ghcr_digest}"'"}]}}'
expect_green yaml-queue-workflow-with-a-ghcr-pull-passes "${d}"

# --- F6: pull_request_target and workflow_run follow the queue path ----------------
queue_tree trig-follow-red
d="${QUEUE_DIR}"
write_wf "${d}" fixture-prt.yml fixture-prt '{"pull_request_target": null}' "${hub_job}"
write_wf "${d}" fixture-follow-1.yml follow-one '{"workflow_run": {"workflows": ["fixture-queue"], "types": ["completed"]}}' "${hub_job}"
write_wf "${d}" fixture-follow-2.yml follow-two '{"workflow_run": {"workflows": ["follow-one"], "types": ["completed"]}}' "${hub_job}"
write_wf "${d}" fixture-follow-3.yaml follow-three '{"workflow_run": {"workflows": ["follow-two"]}}' "${hub_job}"
expect_wf_red workflow-run-and-pull-request-target-are-queue-path "${d}" \
  fixture-prt.yml:j fixture-follow-1.yml:j fixture-follow-2.yml:j fixture-follow-3.yaml:j

queue_tree trig-follow-green
d="${QUEUE_DIR}"
# A workflow_run of a tag-only workflow keeps the login rule: a compliant Hub
# service with credentials passes.
write_wf "${d}" fixture-tag.yml fixture-tag '{"push": {"tags": ["v*"]}}' '{"j": {"runs-on": "ubuntu-latest", "steps": [{"run": "echo ok"}]}}'
write_wf "${d}" fixture-after-tag.yml after-tag '{"workflow_run": {"workflows": ["fixture-tag"], "types": ["completed"]}}' "${hub_creds_job}"
# Two workflow_run workflows that name each other and no queue-path workflow: a
# cycle, so no hang, and the login rule governs them.
write_wf "${d}" fixture-cycle-a.yml cycle-a '{"workflow_run": {"workflows": ["cycle-b"]}}' "${hub_creds_job}"
write_wf "${d}" fixture-cycle-b.yml cycle-b '{"workflow_run": {"workflows": ["cycle-a"]}}' "${hub_creds_job}"
# security-scan.yml's shape: a workflow_run of a queue-path workflow that pulls a
# ghcr.io image with packages: read.
write_wf "${d}" fixture-scan.yml fixture-scan '{"workflow_run": {"workflows": ["fixture-queue"], "types": ["completed"]}}' \
  '{"scan": {"runs-on": "ubuntu-latest", "permissions": {"contents": "read", "packages": "read"}, "steps": [{"uses": "docker/login-action@v3", "with": {"registry": "ghcr.io", "username": "${{ github.actor }}", "password": "${{ secrets.GITHUB_TOKEN }}"}}, {"run": "docker pull '"${ghcr_digest}"'"}]}}'
expect_green workflow-run-of-a-non-queue-workflow-and-the-ghcr-scan-shape-pass "${d}"
