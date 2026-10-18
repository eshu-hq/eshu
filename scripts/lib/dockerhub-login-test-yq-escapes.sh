#!/usr/bin/env bash
#
# dockerhub-login-test-yq-escapes.sh - sourced by test-verify-dockerhub-login.sh (#8397).
#
# yq before 4.49 does not interpret a "\t" escape inside an expression: a
# join("\t") yields a backslash and a t, not a tab. The verifier reads its yq
# output as tab-separated rows, so on such a yq every row is one unsplit field,
# no job or step matches, and the gate passes with "0 Docker Hub-reaching job(s)"
# (fail-open). The verifier must pass the separator through strenv instead.
#
# The shim below makes ANY installed yq behave like the old one for the
# verifier's calls: it doubles the backslash of every \t escape in an argument,
# so the expression holds a literal backslash and a t. The cases then re-run
# one violation per row table (jobs, steps, job env, workflow env) under the
# shim. They go RED on a verifier that joins with a "\t" escape, and stay GREEN
# on one that passes the tab through strenv, whatever yq is installed. Expects
# the helpers of test-verify-dockerhub-login.sh (new_case, add_job, expect_*) and
# of dockerhub-login-test-queue.sh (queue_tree, queue_wf, ghcr_digest, ghcr_buildkit).

old_yq_dir="${tmp_root}/old-yq"
mkdir -p "${old_yq_dir}"
real_yq="$(command -v yq)"
cat >"${old_yq_dir}/yq" <<SHIM
#!/usr/bin/env bash
# Emulates yq < 4.49: the \\t escape in an argument is not interpreted.
args=()
for a in "\$@"; do args+=("\${a//\\\\t/\\\\\\\\t}"); done
exec "${real_yq}" "\${args[@]}"
SHIM
chmod +x "${old_yq_dir}/yq"

run_verifier_old_yq() { PATH="${old_yq_dir}:${PATH}" ESHU_DOCKERHUB_LOGIN_ROOT="$1" bash "${verifier}" 2>&1; }

# Self-check: the shim must yield a literal backslash-t, or the cases below prove nothing.
shim_probe="$(PATH="${old_yq_dir}:${PATH}" yq -n '["a","b"] | join("\t")')"
if [[ "${shim_probe}" == 'a\tb' ]]; then
  echo "ok - old-yq shim leaves the tab escape uninterpreted"
else
  printf 'not ok - old-yq shim leaves the tab escape uninterpreted: got %q\n' "${shim_probe}"; failures=$((failures + 1))
fi

expect_red_old_yq() { # <name> <dir> <expected output fragment>
  local out
  if out="$(run_verifier_old_yq "$2")"; then
    printf 'not ok - %s: verifier passed a planted violation under an old yq\n' "$1"; failures=$((failures + 1))
  elif [[ "${out}" == *"$3"* ]]; then
    printf 'ok - %s\n' "$1"
  else
    printf 'not ok - %s: expected "%s" in:\n%s\n' "$1" "$3" "${out}"; failures=$((failures + 1))
  fi
}

expect_green_old_yq() { # <name> <dir>
  local out
  if out="$(run_verifier_old_yq "$2")"; then
    printf 'ok - %s\n' "$1"
  else
    printf 'not ok - %s: verifier failed a clean tree under an old yq:\n%s\n' "$1" "${out}"; failures=$((failures + 1))
  fi
}

old_no_login='verify-agent-hygiene.yml:fixture-docker: no Docker Hub docker/login-action@v3 step'

# jobs.tsv, service rows: a Hub service without credentials.
d="$(new_case old-yq-service-row)"
yq e -i 'del(.jobs.contention-gate.services.postgres.credentials)' "${d}/.github/workflows/legacy-hub.yml"
expect_red_old_yq old-yq-service-without-credentials "${d}" 'legacy-hub.yml:contention-gate: service postgres'

# jobs.tsv, container rows: a Hub container without credentials.
d="$(new_case old-yq-container-row)"
yq e -i '.jobs.fixture-container = {"runs-on": "ubuntu-latest", "container": {"image": "postgres:18"}, "steps": [{"run": "echo ok"}]}' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red_old_yq old-yq-container-without-credentials "${d}" 'verify-agent-hygiene.yml:fixture-container: container (postgres:18) pulls from Docker Hub at job init without credentials'

# steps.tsv: a docker step with no login.
d="$(new_case old-yq-step-row)"
add_job "${d}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
expect_red_old_yq old-yq-docker-step-without-login "${d}" "${old_no_login}"

# jobs.tsv, job row, and the login step's columns: a compliant job passes.
d="$(new_case old-yq-compliant-job)"
add_job "${d}" verify-agent-hygiene fixture-docker 'docker pull postgres:18'
yq e -i '.jobs.fixture-docker.steps = [{"uses": "docker/login-action@v3", "if": "env.DOCKERHUB_LOGIN_ENABLED == '"'"'true'"'"'", "with": {"username": "${{ secrets.DOCKERHUB_USERNAME }}", "password": "${{ secrets.DOCKERHUB_TOKEN }}"}}, {"run": "docker pull postgres:18"}]' "${d}/.github/workflows/verify-agent-hygiene.yml"
yq e -i ".jobs.fixture-docker.env.DOCKERHUB_LOGIN_ENABLED = \"\${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}\"" "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_green_old_yq old-yq-compliant-job-passes "${d}"

# jobenv.tsv: a job env value that makes the image a Hub image.
d="$(new_case old-yq-job-env-row)"
printf '#!/usr/bin/env bash\nimage="${FIXTURE_IMAGE:-ghcr.io/eshu-hq/eshu:main}"\ndocker run --rm "${image}" true\n' >"${d}/scripts/fixture-run.sh"
add_job "${d}" verify-agent-hygiene fixture-docker 'bash scripts/fixture-run.sh'
yq e -i '.jobs.fixture-docker.env.FIXTURE_IMAGE = "postgres:18"' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red_old_yq old-yq-job-env-value-beats-the-ghcr-default "${d}" "${old_no_login}"

# wfenv.tsv: a workflow env value that makes the image a Hub image.
d="$(new_case old-yq-workflow-env-row)"
printf '#!/usr/bin/env bash\nimage="${FIXTURE_IMAGE:-ghcr.io/eshu-hq/eshu:main}"\ndocker run --rm "${image}" true\n' >"${d}/scripts/fixture-run.sh"
add_job "${d}" verify-agent-hygiene fixture-docker 'bash scripts/fixture-run.sh'
yq e -i '.env.FIXTURE_IMAGE = "postgres:18"' "${d}/.github/workflows/verify-agent-hygiene.yml"
expect_red_old_yq old-yq-workflow-env-value-beats-the-ghcr-default "${d}" "${old_no_login}"

# matrix.tsv: a static matrix cell that names a Hub image fails on the queue path,
# and a matrix whose every cell is ghcr passes. Both depend on the matrix rows
# splitting; the green half alone would also pass a fail-open verifier.
old_yq_matrix_job() { # <dir> <job> <matrix json> <run text>
  yq e -i ".jobs[\"$2\"] = {\"runs-on\": \"ubuntu-latest\", \"strategy\": {\"matrix\": $3}, \"steps\": [{\"run\": \"$4\"}]}" "$1/${queue_wf}"
}
queue_tree old-yq-matrix-red
d="${QUEUE_DIR}"
old_yq_matrix_job "${d}" m-axis-with-a-hub-image '{"image": ["'"${ghcr_digest}"'", "postgres:18"]}' 'docker run --rm ${{ matrix.image }} true'
expect_red_old_yq old-yq-matrix-cell-with-a-hub-image "${d}" "$(queue_job_red m-axis-with-a-hub-image)"
queue_tree old-yq-matrix-green
d="${QUEUE_DIR}"
old_yq_matrix_job "${d}" m-include-ghcr '{"include": [{"cf": "docker-compose.fixture-ghcr.yaml", "svc": "db"}, {"cf": "docker-compose.fixture-ghcr.yaml", "svc": "api"}]}' 'docker compose -f ${{ matrix.cf }} up -d ${{ matrix.svc }}'
old_yq_matrix_job "${d}" m-axis-ghcr '{"image": ["'"${ghcr_digest}"'", "'"${ghcr_buildkit}"'"]}' 'docker run --rm ${{ matrix.image }} true'
expect_green_old_yq old-yq-matrix-cells-that-are-all-ghcr-pass "${d}"
