#!/usr/bin/env bash
#
# dockerhub-login-test-triggers.sh - seeded RED/GREEN cases for the queue-path
# trigger set, the workflow file extensions, the variable and argument probes
# and the parser-precision edits of scripts/verify-dockerhub-login.sh (#7886).
# Sourced, never run, from scripts/test-verify-dockerhub-login.sh after the
# precision batch. It uses queue_tree, qjob, steps_run, pscript, ghcr_digest and
# queue_wf from the earlier libs and the test's own expect_green.
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

# --- F2 and F3: the probes of the review, as seeded cases -------------------------
queue_tree probes-red
d="${QUEUE_DIR}"
pscript "${d}" fixture-r-tool '#!/usr/bin/env bash\ntool="$1"\nshift\n"$tool" "$@"\n'
qjob "${d}" r-tool-from-dollar-1-runs-a-hub-pull "$(steps_run 'bash scripts/fixture-r-tool.sh docker pull alpine:3.21')"
pscript "${d}" fixture-r-cat '#!/usr/bin/env bash\nRT="$(cat /tmp/runtime-name)"\n"$RT" frobnicate x\ndocker ps\n'
qjob "${d}" r-command-substitution-beside-docker "$(steps_run 'bash scripts/fixture-r-cat.sh')"
pscript "${d}" fixture-r-read '#!/usr/bin/env bash\nread -r RT </tmp/runtime-name\n"$RT" frobnicate x\ndocker ps\n'
qjob "${d}" r-read-beside-docker "$(steps_run 'bash scripts/fixture-r-read.sh')"
pscript "${d}" fixture-r-direct '#!/usr/bin/env bash\n"$1" pull alpine:3.21\n'
qjob "${d}" r-bare-docker-to-a-script-that-invokes-dollar-1 "$(steps_run 'bash scripts/fixture-r-direct.sh docker')"
expect_queue_jobs_red probe-variable-and-argument-pulls-fail "${d}" r-tool-from-dollar-1-runs-a-hub-pull \
  r-command-substitution-beside-docker r-read-beside-docker r-bare-docker-to-a-script-that-invokes-dollar-1

# --- F4: the precision edits keep the real reach and drop only the data ----------
queue_tree precision-edits-red
d="${QUEUE_DIR}"
pscript "${d}" fixture-e-continued-hub '#!/usr/bin/env bash\ndocker compose \\\n\t-f docker-compose.fixture-hub.yaml \\\n\tup -d\n'
qjob "${d}" e-continued-compose-line-names-a-hub-file "$(steps_run 'bash scripts/fixture-e-continued-hub.sh')"
pscript "${d}" fixture-e-comment-and-code '#!/usr/bin/env bash\n# see docker-compose.fixture-ghcr.yaml\ndocker compose -f docker-compose.fixture-hub.yaml up -d\n'
qjob "${d}" e-comment-names-one-file-and-the-command-a-hub-file "$(steps_run 'bash scripts/fixture-e-comment-and-code.sh')"
pscript "${d}" fixture-e-comment-backslash '#!/usr/bin/env bash\n# a note that ends in a backslash \\\nbash scripts/fixture-q-run-hub.sh\n'
qjob "${d}" e-command-after-a-comment-that-ends-in-a-backslash "$(steps_run 'bash scripts/fixture-e-comment-backslash.sh')"
# An override file that sets a Hub image of its own, and an environment-only
# override whose base -f file names a Hub image, are still flagged: every -f file
# the script names is checked.
printf 'services:\n  db:\n    image: postgres:18\n' >"${d}/docker-compose.fixture-override-hub.yaml"
pscript "${d}" fixture-e-override-sets-hub '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-ghcr.yaml -f docker-compose.fixture-override-hub.yaml up -d\n'
qjob "${d}" e-override-file-sets-a-hub-image "$(steps_run 'bash scripts/fixture-e-override-sets-hub.sh')"
printf 'services:\n  db:\n    environment:\n      A: "1"\n' >"${d}/docker-compose.fixture-override-env.yaml"
pscript "${d}" fixture-e-override-over-hub '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-hub.yaml -f docker-compose.fixture-override-env.yaml up -d\n'
qjob "${d}" e-environment-only-override-of-a-hub-base "$(steps_run 'bash scripts/fixture-e-override-over-hub.sh')"
printf 'services:\n  db: {}\n' >"${d}/docker-compose.fixture-empty-service.yaml"
pscript "${d}" fixture-e-empty-service '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-empty-service.yaml up -d\n'
qjob "${d}" e-empty-service-map-stays-unresolved "$(steps_run 'bash scripts/fixture-e-empty-service.sh')"
printf 'services:\n  db: false\n' >"${d}/docker-compose.fixture-false-service.yaml"
pscript "${d}" fixture-e-false-service '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-false-service.yaml up -d\n'
qjob "${d}" e-non-map-service-stays-unresolved "$(steps_run 'bash scripts/fixture-e-false-service.sh')"
expect_queue_jobs_red precision-edits-keep-the-real-reach "${d}" e-continued-compose-line-names-a-hub-file \
  e-comment-names-one-file-and-the-command-a-hub-file e-command-after-a-comment-that-ends-in-a-backslash \
  e-empty-service-map-stays-unresolved e-non-map-service-stays-unresolved \
  e-override-file-sets-a-hub-image e-environment-only-override-of-a-hub-base
queue_tree precision-edits-green
d="${QUEUE_DIR}"
pscript "${d}" fixture-e-comment-only '#!/usr/bin/env bash\n# the Hub variant is docker-compose.fixture-hub.yaml\necho ok\n'
qjob "${d}" e-comment-only-compose-name "$(steps_run 'bash scripts/fixture-e-comment-only.sh')"
pscript "${d}" fixture-e-continued-ghcr '#!/usr/bin/env bash\ndocker compose \\\n\t-f docker-compose.fixture-ghcr.yaml \\\n\tup -d\n'
qjob "${d}" e-continued-compose-line-with-a-ghcr-file "$(steps_run 'bash scripts/fixture-e-continued-ghcr.sh')"
pscript "${d}" fixture-e-comment-backslash-ok '#!/usr/bin/env bash\n# a note that ends in a backslash \\\necho ok\n'
qjob "${d}" e-comment-ending-in-a-backslash-does-not-join "$(steps_run 'bash scripts/fixture-e-comment-backslash-ok.sh')"
# A service that only overrides another file's service names no image: a
# non-empty map with no image, build or extends passes; an empty map and a
# non-map service stay unresolved.
pscript "${d}" fixture-e-override-ghcr '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-ghcr.yaml -f docker-compose.fixture-override.yaml up -d\n'
printf 'services:\n  db:\n    environment:\n      A: "1"\n    command: ["true"]\n' >"${d}/docker-compose.fixture-override.yaml"
qjob "${d}" e-override-only-service-with-environment "$(steps_run 'bash scripts/fixture-e-override-ghcr.sh')"
expect_green precision-edits-drop-only-data "${d}"
