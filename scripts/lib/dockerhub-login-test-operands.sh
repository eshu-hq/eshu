#!/usr/bin/env bash
#
# dockerhub-login-test-operands.sh - seeded RED/GREEN cases for the -f operands
# of docker compose invocations, for scripts/verify-dockerhub-login.sh (#7886).
# Sourced, never run, from scripts/test-verify-dockerhub-login.sh after the
# trigger batch. It uses queue_tree, qjob, steps_run, pscript and
# expect_queue_jobs_red from the earlier libs and the test's own expect_green.
#
# A compose file the gate cannot read is a file whose images it cannot check, so
# an invocation whose -f operand is not a literal checked file (a variable, an
# env value, an expression, an array, COMPOSE_FILE) is unresolved and fails a
# queue-path job, even when another -f file of the same invocation is an
# override-only file or a ghcr.io file. A variable the scripts and env settle to
# literal compose files is followed file by file.
#
# shellcheck disable=SC2154  # failures, run_verifier, QUEUE_DIR come from the test

# step_env_job <script> <env json>: one step with env; job_env_job puts it on the job.
step_env_job() {
  printf '{"runs-on": "ubuntu-latest", "steps": [{"uses": "actions/checkout@v5"}, {"run": "bash scripts/%s.sh", "env": %s}]}' "$1" "$2"
}
job_env_job() { # <script> <env json>
  printf '{"runs-on": "ubuntu-latest", "env": %s, "steps": [{"uses": "actions/checkout@v5"}, {"run": "bash scripts/%s.sh"}]}' "$2" "$1"
}
override_yaml=$'services:\n  db:\n    environment:\n      A: "1"\n'

# --- RED: an operand the gate cannot settle is unresolved ----------------------------
queue_tree operands-red
d="${QUEUE_DIR}"
printf '%s' "${override_yaml}" >"${d}/docker-compose.fixture-override.yaml"
pscript "${d}" fixture-o-env-base '#!/usr/bin/env bash\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-override.yaml up -d\n'
qjob "${d}" o-step-env-names-a-hub-base "$(step_env_job fixture-o-env-base '{"BASE_FILE": "docker-compose.fixture-hub.yaml"}')"
pscript "${d}" fixture-o-job-env '#!/usr/bin/env bash\nbase="${BASE_FILE}"\ndocker compose -f "${base}" -f docker-compose.fixture-override.yaml up -d\n'
qjob "${d}" o-job-env-through-a-script-variable-names-a-hub-base "$(job_env_job fixture-o-job-env '{"BASE_FILE": "docker-compose.fixture-hub.yaml"}')"
pscript "${d}" fixture-o-var-beside-ghcr '#!/usr/bin/env bash\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-unsettled-variable-beside-a-ghcr-file "$(steps_run 'bash scripts/fixture-o-var-beside-ghcr.sh')"
pscript "${d}" fixture-o-var-alone '#!/usr/bin/env bash\ndocker compose -f "$BASE_FILE" up -d\n'
qjob "${d}" o-unsettled-variable-alone "$(steps_run 'bash scripts/fixture-o-var-alone.sh')"
pscript "${d}" fixture-o-expression '#!/usr/bin/env bash\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-step-env-expression-beside-a-ghcr-file "$(step_env_job fixture-o-expression '{"BASE_FILE": "${{ inputs.base }}"}')"
pscript "${d}" fixture-o-compose-file-env '#!/usr/bin/env bash\nexport COMPOSE_FILE="docker-compose.fixture-ghcr.yaml:${EXTRA_FILE}"\ndocker compose up -d\n'
qjob "${d}" o-compose-file-env-with-an-unsettled-entry "$(steps_run 'bash scripts/fixture-o-compose-file-env.sh')"
pscript "${d}" fixture-o-array-operand '#!/usr/bin/env bash\nfiles=(docker-compose.fixture-ghcr.yaml)\ndocker compose -f "${files[@]}" up -d\n'
qjob "${d}" o-array-expansion-as-the-operand "$(steps_run 'bash scripts/fixture-o-array-operand.sh')"
pscript "${d}" fixture-o-two-assignments '#!/usr/bin/env bash\nf=docker-compose.fixture-ghcr.yaml\nif [ -n "${X:-}" ]; then f=docker-compose.fixture-hub.yaml; fi\ndocker compose -f "$f" up -d\n'
qjob "${d}" o-one-of-two-assignments-names-a-hub-file "$(steps_run 'bash scripts/fixture-o-two-assignments.sh')"
pscript "${d}" fixture-o-missing-file '#!/usr/bin/env bash\nf=docker-compose.fixture-missing.yaml\ndocker compose -f "$f" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-operand-names-a-file-the-tree-lacks "$(steps_run 'bash scripts/fixture-o-missing-file.sh')"
pscript "${d}" fixture-o-array-var '#!/usr/bin/env bash\nC=(docker compose)\n"${C[@]}" -f "$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-variable-invocation-with-an-unsettled-base "$(steps_run 'bash scripts/fixture-o-array-var.sh')"
pscript "${d}" fixture-o-helper-unset '#!/usr/bin/env bash\nhelper() {\n\tlocal cf="$1"\n\tdocker compose -f "$cf" -f docker-compose.fixture-ghcr.yaml up -d\n}\nhelper "$UNSET_BASE"\n'
qjob "${d}" o-helper-called-with-an-unsettled-variable "$(steps_run 'bash scripts/fixture-o-helper-unset.sh')"
pscript "${d}" fixture-o-script-arg '#!/usr/bin/env bash\ndocker compose -f "$1" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-script-argument-as-the-operand "$(steps_run 'bash scripts/fixture-o-script-arg.sh docker-compose.fixture-ghcr.yaml')"
pscript "${d}" fixture-o-helper-env '#!/usr/bin/env bash\nhelper() {\n\tdocker compose -f "$1" -f docker-compose.fixture-override.yaml up -d\n}\nhelper "$BASE_FILE"\n'
qjob "${d}" o-helper-argument-from-env-names-a-hub-base "$(step_env_job fixture-o-helper-env '{"BASE_FILE": "docker-compose.fixture-hub.yaml"}')"
pscript "${d}" fixture-o-read '#!/usr/bin/env bash\nread -r BASE_FILE\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-read-fills-the-operand-variable "$(steps_run 'bash scripts/fixture-o-read.sh')"
pscript "${d}" fixture-o-subst '#!/usr/bin/env bash\nBASE_FILE="$(cat base-file.txt)"\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-command-substitution-fills-the-operand-variable "$(steps_run 'bash scripts/fixture-o-subst.sh')"
pscript "${d}" fixture-o-attached '#!/usr/bin/env bash\ndocker compose -f"$BASE_FILE" -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-attached-operand-form-with-an-unsettled-variable "$(steps_run 'bash scripts/fixture-o-attached.sh')"
pscript "${d}" fixture-o-array-append '#!/usr/bin/env bash\nargs=(-p proj)\nargs+=(-f "$BASE_FILE")\nargs+=(-f docker-compose.fixture-ghcr.yaml)\ndocker compose "${args[@]}" up -d\n'
qjob "${d}" o-option-array-appended-with-an-unsettled-variable "$(steps_run 'bash scripts/fixture-o-array-append.sh')"
# The shape run-live-backend-tests.sh had: the compose file is cut out of a row
# table built with mapfile and printf. The literal file name sits in the text,
# so the name scan alone passes it; the operand has no static source and fails.
pscript "${d}" fixture-o-row-table '#!/usr/bin/env bash\nmapfile -t runs < <(printf "nornicdb|%%s\\n" "docker-compose.fixture-ghcr.yaml")\nfor (( i = 0; i < ${#runs[@]}; i++ )); do\n\trest="${runs[$i]#*|}"\n\tcompose_file="${rest%%%%|*}"\n\tdocker compose -f "${compose_file}" up -d\ndone\n'
qjob "${d}" o-compose-file-cut-from-a-mapfile-row-table "$(steps_run 'bash scripts/fixture-o-row-table.sh')"
# A name that only passes itself through a function parameter has no source.
pscript "${d}" fixture-o-self-cycle '#!/usr/bin/env bash\nhelper() {\n\tlocal compose_file="$1"\n\tdocker compose -f "$compose_file" -f docker-compose.fixture-ghcr.yaml up -d\n}\nhelper "$compose_file"\n'
qjob "${d}" o-name-passed-through-a-parameter-with-no-source "$(steps_run 'bash scripts/fixture-o-self-cycle.sh')"
# A sourced file shares the shell: its unsettled assignment of the same name counts.
pscript "${d}" fixture-o-scope-sourced '#!/usr/bin/env bash\nsource "$(dirname "$0")/lib/fixture-o-scope-lib.sh"\ncompose_base="docker-compose.fixture-ghcr.yaml"\ndocker compose -f "${compose_base}" up -d\n'
printf '#!/usr/bin/env bash\ncompose_base="$(cat /tmp/base-file)"\n' >"${d}/scripts/lib/fixture-o-scope-lib.sh"
qjob "${d}" o-sourced-file-assigns-the-name-from-a-command "$(steps_run 'bash scripts/fixture-o-scope-sourced.sh')"
expect_queue_jobs_red operand-holes-stay-unresolved "${d}" o-step-env-names-a-hub-base \
  o-sourced-file-assigns-the-name-from-a-command \
  o-name-passed-through-a-parameter-with-no-source \
  o-compose-file-cut-from-a-mapfile-row-table \
  o-attached-operand-form-with-an-unsettled-variable o-option-array-appended-with-an-unsettled-variable \
  o-helper-called-with-an-unsettled-variable o-script-argument-as-the-operand \
  o-helper-argument-from-env-names-a-hub-base o-read-fills-the-operand-variable \
  o-command-substitution-fills-the-operand-variable \
  o-job-env-through-a-script-variable-names-a-hub-base o-unsettled-variable-beside-a-ghcr-file \
  o-unsettled-variable-alone o-step-env-expression-beside-a-ghcr-file o-compose-file-env-with-an-unsettled-entry \
  o-array-expansion-as-the-operand o-one-of-two-assignments-names-a-hub-file o-operand-names-a-file-the-tree-lacks \
  o-variable-invocation-with-an-unsettled-base

# --- GREEN: literal and settled operands pass --------------------------------------
queue_tree operands-green
d="${QUEUE_DIR}"
printf '%s' "${override_yaml}" >"${d}/docker-compose.fixture-override.yaml"
pscript "${d}" fixture-o-assigned '#!/usr/bin/env bash\nf=docker-compose.fixture-ghcr.yaml\ndocker compose -f "$f" up -d\n'
qjob "${d}" o-variable-assigned-a-ghcr-file "$(steps_run 'bash scripts/fixture-o-assigned.sh')"
pscript "${d}" fixture-o-default '#!/usr/bin/env bash\ndocker compose -f "${FIXTURE_COMPOSE:-docker-compose.fixture-ghcr.yaml}" up -d\n'
qjob "${d}" o-default-value-names-a-ghcr-file "$(steps_run 'bash scripts/fixture-o-default.sh')"
pscript "${d}" fixture-o-env-ghcr '#!/usr/bin/env bash\ndocker compose -f "$BASE_FILE" -f docker-compose.fixture-override.yaml up -d\n'
qjob "${d}" o-step-env-names-a-ghcr-base "$(step_env_job fixture-o-env-ghcr '{"BASE_FILE": "docker-compose.fixture-ghcr.yaml"}')"
pscript "${d}" fixture-o-dir-prefix '#!/usr/bin/env bash\nrepo_root="$(pwd)"\ndocker compose -f "${repo_root}/docker-compose.fixture-ghcr.yaml" -f "${repo_root}/docker-compose.fixture-override.yaml" up -d\n'
qjob "${d}" o-variable-directory-prefix-with-literal-files "$(steps_run 'bash scripts/fixture-o-dir-prefix.sh')"
pscript "${d}" fixture-o-follow '#!/usr/bin/env bash\ndocker compose -f docker-compose.fixture-ghcr.yaml up -d\ndocker compose -f docker-compose.fixture-ghcr.yaml logs -f db\nrm -f "$TMP"\ntail -f "$LOG" &\n'
qjob "${d}" o-logs-follow-and-rm-force-are-not-compose-files "$(steps_run 'bash scripts/fixture-o-follow.sh')"
pscript "${d}" fixture-o-array-args '#!/usr/bin/env bash\nargs=(-p proj -f docker-compose.fixture-ghcr.yaml -f docker-compose.fixture-override.yaml)\ndocker compose "${args[@]}" up -d\n'
qjob "${d}" o-literal-files-inside-an-array "$(steps_run 'bash scripts/fixture-o-array-args.sh')"
pscript "${d}" fixture-o-compose-file-ok '#!/usr/bin/env bash\nexport COMPOSE_FILE="docker-compose.fixture-ghcr.yaml:docker-compose.fixture-override.yaml"\ndocker compose up -d\n'
qjob "${d}" o-compose-file-env-of-literal-files "$(steps_run 'bash scripts/fixture-o-compose-file-ok.sh')"
pscript "${d}" fixture-o-continued-var '#!/usr/bin/env bash\nf=docker-compose.fixture-ghcr.yaml\ndocker compose \\\n\t-f "$f" \\\n\t-f docker-compose.fixture-override.yaml \\\n\tup -d\n'
qjob "${d}" o-continued-line-with-a-settled-variable "$(steps_run 'bash scripts/fixture-o-continued-var.sh')"
pscript "${d}" fixture-o-helper-ok '#!/usr/bin/env bash\ncompose_file="docker-compose.fixture-ghcr.yaml"\nhelper() {\n\tlocal project="$1" cf="${2:-docker-compose.fixture-ghcr.yaml}"\n\tdocker compose -p "${project}" -f "${cf}" -f docker-compose.fixture-override.yaml up -d\n}\nunused() {\n\tdocker compose -f "$1" up -d\n}\nhelper one "${compose_file}"\nif ! helper two; then echo failed; fi\nout="$(helper three "${compose_file}")"\n'
qjob "${d}" o-helper-arguments-and-defaults-settle-to-ghcr-files "$(steps_run 'bash scripts/fixture-o-helper-ok.sh')"
pscript "${d}" fixture-o-quoted-data '#!/usr/bin/env bash\nprintf "docker compose -f \\"$X\\" up\\n" >note\ncat <<'"'"'EOF'"'"' >note2\ndocker compose -f "$IN_HEREDOC" up\nEOF\ndocker compose -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-quoted-and-heredoc-text-is-data "$(steps_run 'bash scripts/fixture-o-quoted-data.sh')"
pscript "${d}" fixture-o-message-text '#!/usr/bin/env bash\ncompose_note() { printf "%%s\\n" "$1"; }\ncompose_note "-f operand in ${0##*/}"\ncompose_note "-f $BASE_FILE is not a compose file"\ndocker compose -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" o-quoted-message-text-that-starts-with-dash-f "$(steps_run 'bash scripts/fixture-o-message-text.sh')"
# The same runner after the case-select: the file is chosen onto literal variables.
pscript "${d}" fixture-o-case-select '#!/usr/bin/env bash\nnornicdb_compose="docker-compose.fixture-ghcr.yaml"\nneo4j_compose="docker-compose.fixture-ghcr.yaml"\nname="${1:-nornicdb}"\ncase "${name}" in\n\tnornicdb) compose_file="${nornicdb_compose}" ;;\n\tneo4j) compose_file="${neo4j_compose}" ;;\n\t*) exit 2 ;;\nesac\ndocker compose -f "${compose_file}" up -d\n'
qjob "${d}" o-compose-file-selected-by-case-onto-literal-variables "$(steps_run 'bash scripts/fixture-o-case-select.sh')"
# A script that only runs another script shares no shell with it: the caller's
# unsettled assignment of the same name does not reach the callee's operand.
pscript "${d}" fixture-o-scope-callee '#!/usr/bin/env bash\ncompose_base="docker-compose.fixture-ghcr.yaml"\ndocker compose -f "${compose_base}" up -d\n'
pscript "${d}" fixture-o-scope-caller '#!/usr/bin/env bash\ncompose_base="$(cat /tmp/base-file)"\nbash scripts/fixture-o-scope-callee.sh\n'
qjob "${d}" o-separately-run-script-does-not-share-assignments "$(steps_run 'bash scripts/fixture-o-scope-caller.sh')"
expect_green literal-and-settled-compose-operands-pass "${d}"
