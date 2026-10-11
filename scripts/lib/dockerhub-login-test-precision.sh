#!/usr/bin/env bash
#
# dockerhub-login-test-precision.sh - seeded RED/GREEN cases for the lexer and
# resolution rules the queue-path rule depends on (#7886). Sourced, never run,
# from scripts/test-verify-dockerhub-login.sh after the queue batch. It uses
# queue_tree, qjob, steps_run and expect_queue_jobs_red from
# lib/dockerhub-login-test-queue.sh and the test's own expect_green.
#
# A queue-path job cannot be waived by a login, so every fail-closed guess of the
# gate is a hard failure there. Each GREEN shape below used to be a false
# positive of such a guess; each RED shape is its nearest real violation, so the
# refinement is shown not to hide the true reach.
#
# shellcheck disable=SC2154  # failures, ghcr_digest come from the test

pscript() { # <dir> <name> <printf format> [args...]: writes scripts/<name>.sh
  local dir="$1" name="$2" fmt="$3"
  shift 3
  # shellcheck disable=SC2059
  printf "${fmt}" "$@" >"${dir}/scripts/${name}.sh"
}

# --- GREEN: refinements that must not over-reach ----------------------------------
queue_tree precision-green
d="${QUEUE_DIR}"
# A full-line comment that names a Docker Hub compose file is not a command.
pscript "${d}" fixture-p-comment-compose '#!/usr/bin/env bash\n# docker-compose.fixture-hub.yaml is the Hub variant; this script never uses it.\ndocker compose -f docker-compose.fixture-ghcr.yaml up -d\n'
qjob "${d}" p-comment-compose-name "$(steps_run 'bash scripts/fixture-p-comment-compose.sh')"
# A compose file named only as the default of a variable is still named.
pscript "${d}" fixture-p-default-compose '#!/usr/bin/env bash\ncompose_file="${FIXTURE_COMPOSE:-docker-compose.fixture-ghcr.yaml}"\ndocker compose -f "${compose_file}" up -d\n'
qjob "${d}" p-default-compose-name "$(steps_run 'bash scripts/fixture-p-default-compose.sh')"
# docker in a for-in word list is data.
pscript "${d}" fixture-p-for-list '#!/usr/bin/env bash\nfor tool in docker node go; do\n\tcommand -v "$tool" >/dev/null 2>&1 || exit 1\ndone\n'
qjob "${d}" p-for-in-docker-word "$(steps_run 'bash scripts/fixture-p-for-list.sh')"
# A [[ ]] test continued across lines is not a command in its second line.
pscript "${d}" fixture-p-multiline-test '#!/usr/bin/env bash\ndocker ps -q\na=1; b=2\n[[ "${a}" -eq 1 &&\n\t"${b}" -eq 2 ]] || exit 1\n'
qjob "${d}" p-multiline-test-after-docker "$(steps_run 'bash scripts/fixture-p-multiline-test.sh')"
# A function-name variable beside docker is not docker.
pscript "${d}" fixture-p-fn-var '#!/usr/bin/env bash\ndocker ps -q\nfail_fn="${1:?}"\n"${fail_fn}" "message"\n'
qjob "${d}" p-function-variable-beside-docker "$(steps_run 'bash scripts/fixture-p-fn-var.sh fail_now')"
# A variable invoked with a non-pulling subcommand pulls nothing.
pscript "${d}" fixture-p-nopull-var '#!/usr/bin/env bash\ndocker ps -q\n"${COMPOSE_CMD[@]}" ps -a -q service\n"${COMPOSE_CMD[@]}" exec -T service true\n'
qjob "${d}" p-variable-with-a-non-pulling-subcommand "$(steps_run 'bash scripts/fixture-p-nopull-var.sh')"
# A wrapper around docker is read as the docker command it runs; a bare docker
# word names the program only.
pscript "${d}" fixture-p-wrapper '#!/usr/bin/env bash\nrequire_tool() { command -v "$1" >/dev/null 2>&1; }\nretry 3 docker pull %s\nrequire_tool docker\nrun_it docker compose -f docker-compose.fixture-ghcr.yaml exec -T db true\n' "${ghcr_digest}"
qjob "${d}" p-wrapper-of-a-ghcr-docker-command "$(steps_run 'bash scripts/fixture-p-wrapper.sh')"
# A path inside a backslash-continued for-list is data, not a command.
pscript "${d}" fixture-p-for-continuation '#!/usr/bin/env bash\nfor entry in \\\n\t"${repo_root}/scripts/fixture-q-run-hub.sh:1" \\\n\t"${repo_root}/scripts/fixture-q-compose-hub.sh:2"; do\n\techo "${entry}"\ndone\n'
qjob "${d}" p-script-path-in-a-continued-for-list "$(steps_run 'bash scripts/fixture-p-for-continuation.sh')"
# A function-name variable beside real docker words (the golden-corpus gate shape):
# the parameter is assigned from $1, and the bare docker is argument 2 of a callee
# that invokes argument 1 only.
pscript "${d}" fixture-p-golden-shape '#!/usr/bin/env bash\nrequire_tools() {\n\tlocal fail_fn="$1"\n\tshift\n\tlocal tool\n\tfor tool in "$@"; do\n\t\tcommand -v "${tool}" >/dev/null 2>&1 || "${fail_fn}" "missing ${tool}"\n\tdone\n}\ndocker ps -q\nrequire_tools fail_now rg jq\nrequire_tools fail_now docker\n'
qjob "${d}" p-function-name-variable-beside-docker-words "$(steps_run 'bash scripts/fixture-p-golden-shape.sh')"
# A variable in a file with no docker word, while docker is named elsewhere in the
# closure (the install-apt-packages shape).
pscript "${d}" fixture-p-docker-free '#!/usr/bin/env bash\nsha_cmd="$(command -v sha256sum || command -v shasum)"\n"${sha_cmd}" archive.tgz\n'
pscript "${d}" fixture-p-docker-elsewhere '#!/usr/bin/env bash\ndocker ps -q\n'
qjob "${d}" p-variable-in-a-docker-free-file "$(steps_run 'bash scripts/fixture-p-docker-free.sh && bash scripts/fixture-p-docker-elsewhere.sh')"
# An unresolvable assignment in a file with no docker word.
pscript "${d}" fixture-p-unresolvable-free '#!/usr/bin/env bash\nrt="$(pick_runtime)"\n"$rt" frobnicate target\n'
qjob "${d}" p-unresolvable-assignment-in-a-docker-free-file "$(steps_run 'bash scripts/fixture-p-unresolvable-free.sh && bash scripts/fixture-p-docker-elsewhere.sh')"
# A variable no script and no env assigns takes its value from outside the
# repository: the documented limit of the rule.
pscript "${d}" fixture-p-outside '#!/usr/bin/env bash\ndocker ps -q\n"$TOOL" pull alpine:3.21\n'
qjob "${d}" p-variable-assigned-nowhere-beside-docker "$(steps_run 'bash scripts/fixture-p-outside.sh')"
expect_green queue-path-precision-refinements-do-not-over-reach "${d}"

# --- static matrix expansion of the run text --------------------------------------
matrix_job() { # <dir> <job> <matrix json> <run text>
  yq e -i ".jobs[\"$2\"] = {\"runs-on\": \"ubuntu-latest\", \"strategy\": {\"matrix\": $3}, \"steps\": [{\"run\": \"$4\"}]}" "$1/${queue_wf}"
}
queue_tree matrix-red
d="${QUEUE_DIR}"
matrix_job "${d}" m-include-with-a-hub-compose '{"include": [{"cf": "docker-compose.fixture-ghcr.yaml"}, {"cf": "docker-compose.fixture-hub.yaml"}]}' 'docker compose -f ${{ matrix.cf }} up -d'
matrix_job "${d}" m-axis-with-a-hub-image '{"image": ["'"${ghcr_digest}"'", "postgres:18"]}' 'docker run --rm ${{ matrix.image }} true'
matrix_job "${d}" m-expression-matrix '"${{ fromJSON(needs.changes.outputs.matrix) }}"' 'docker compose -f ${{ matrix.cf }} up -d'
matrix_job "${d}" m-key-the-cell-lacks '{"include": [{"cf": "docker-compose.fixture-ghcr.yaml"}]}' 'docker compose -f ${{ matrix.other }} up -d'
expect_queue_jobs_red matrix-cells-with-a-hub-reach-or-no-static-value-fail "${d}" m-include-with-a-hub-compose \
  m-axis-with-a-hub-image m-expression-matrix m-key-the-cell-lacks
queue_tree matrix-green
d="${QUEUE_DIR}"
matrix_job "${d}" m-include-ghcr '{"include": [{"cf": "docker-compose.fixture-ghcr.yaml", "svc": "db"}, {"cf": "docker-compose.fixture-ghcr.yaml", "svc": "api"}]}' 'docker compose -f ${{ matrix.cf }} up -d ${{ matrix.svc }}'
matrix_job "${d}" m-axis-ghcr '{"image": ["'"${ghcr_digest}"'", "'"${ghcr_buildkit}"'"]}' 'docker run --rm ${{ matrix.image }} true'
expect_green matrix-cells-that-are-all-ghcr-pass "${d}"

# --- RED: the nearest real violations still fail ----------------------------------
queue_tree precision-red
d="${QUEUE_DIR}"
# The compose file is named in code too; the comment does not hide it.
pscript "${d}" fixture-p-comment-and-code '#!/usr/bin/env bash\n# the Hub variant: docker-compose.fixture-hub.yaml\ndocker compose -f docker-compose.fixture-hub.yaml up -d\n'
qjob "${d}" p-compose-named-in-code-and-comment "$(steps_run 'bash scripts/fixture-p-comment-and-code.sh')"
# A [[ ]] test continued across lines, then a real pull.
pscript "${d}" fixture-p-multiline-then-pull '#!/usr/bin/env bash\na=1; b=2\n[[ "${a}" -eq 1 &&\n\t"${b}" -eq 2 ]] || exit 1\ndocker pull postgres:18\n'
qjob "${d}" p-multiline-test-then-a-hub-pull "$(steps_run 'bash scripts/fixture-p-multiline-then-pull.sh')"
# A wrapper around a Hub pull, and around a Hub compose file.
pscript "${d}" fixture-p-wrapper-hub '#!/usr/bin/env bash\nretry 3 docker pull postgres:18\n'
qjob "${d}" p-wrapper-of-a-hub-pull "$(steps_run 'bash scripts/fixture-p-wrapper-hub.sh')"
pscript "${d}" fixture-p-wrapper-compose '#!/usr/bin/env bash\nrun_it docker compose -f docker-compose.fixture-hub.yaml up -d\n'
qjob "${d}" p-wrapper-of-a-hub-compose "$(steps_run 'bash scripts/fixture-p-wrapper-compose.sh')"
# The variable is assigned docker in the same file, and invoked through it.
pscript "${d}" fixture-p-same-file '#!/usr/bin/env bash\nrt=docker\n"$rt" frobnicate target\n'
qjob "${d}" p-same-file-docker-assignment "$(steps_run 'bash scripts/fixture-p-same-file.sh')"
# An unresolvable assignment in a file that itself names docker.
pscript "${d}" fixture-p-unresolvable '#!/usr/bin/env bash\ndocker ps -q\nrt="$(pick_runtime)"\n"$rt" frobnicate target\n'
qjob "${d}" p-unresolvable-assignment-beside-docker "$(steps_run 'bash scripts/fixture-p-unresolvable.sh')"
# A bare docker passed to a script whose $1 is invoked.
pscript "${d}" fixture-p-passthrough '#!/usr/bin/env bash\nrt="$1"\n"$rt" frobnicate target\n'
qjob "${d}" p-bare-docker-to-a-script-that-invokes-its-first-argument "$(steps_run 'bash scripts/fixture-p-passthrough.sh docker')"
# The same through a function that a sourced lib defines.
pscript "${d}" fixture-p-fn-lib '#!/usr/bin/env bash\nrun_tool() {\n\tlocal t="$1"\n\t"$t" frobnicate target\n}\n'
pscript "${d}" fixture-p-fn-caller '#!/usr/bin/env bash\nsource "$(dirname "$0")/fixture-p-fn-lib.sh"\nrun_tool docker\n'
qjob "${d}" p-bare-docker-to-a-function-that-invokes-its-argument "$(steps_run 'bash scripts/fixture-p-fn-caller.sh')"
# A bare docker passed to a command no reached script defines.
pscript "${d}" fixture-p-unknown-callee '#!/usr/bin/env bash\nmystery_tool docker\n'
qjob "${d}" p-bare-docker-to-an-undefined-command "$(steps_run 'bash scripts/fixture-p-unknown-callee.sh')"
# A variable that a sourced lib assigns a docker value, invoked with an ordinary
# argument: the value reaches it from another file.
pscript "${d}" fixture-p-runtime-lib '#!/usr/bin/env bash\nRUNTIME=docker\n'
pscript "${d}" fixture-p-runtime-user '#!/usr/bin/env bash\nsource "$(dirname "$0")/fixture-p-runtime-lib.sh"\n"${RUNTIME}" frobnicate target\n'
qjob "${d}" p-docker-valued-variable-from-a-sourced-lib "$(steps_run 'bash scripts/fixture-p-runtime-user.sh')"
# The same through the step env and the job env.
pscript "${d}" fixture-p-env-user '#!/usr/bin/env bash\n"${RUNTIME}" frobnicate target\n'
qjob "${d}" p-docker-valued-variable-from-the-job-env "$(steps_run 'bash scripts/fixture-p-env-user.sh')"
yq e -i '.jobs.p-docker-valued-variable-from-the-job-env.env.RUNTIME = "docker"' "${d}/${queue_wf}"
qjob "${d}" p-docker-valued-variable-from-the-step-env "$(steps_run 'bash scripts/fixture-p-env-user.sh')"
yq e -i '.jobs.p-docker-valued-variable-from-the-step-env.steps[1].env.RUNTIME = "docker"' "${d}/${queue_wf}"
# An unresolved expression in the env, read by a script that names docker.
pscript "${d}" fixture-p-expr-env-user '#!/usr/bin/env bash\ndocker ps -q\n"${RUNTIME}" frobnicate target\n'
qjob "${d}" p-unresolved-env-expression-beside-docker "$(steps_run 'bash scripts/fixture-p-expr-env-user.sh')"
yq e -i '.jobs.p-unresolved-env-expression-beside-docker.env.RUNTIME = "${{ inputs.runtime }}"' "${d}/${queue_wf}"
expect_queue_jobs_red queue-path-nearest-violations-still-fail "${d}" p-compose-named-in-code-and-comment \
  p-multiline-test-then-a-hub-pull p-wrapper-of-a-hub-pull p-wrapper-of-a-hub-compose p-same-file-docker-assignment \
  p-unresolvable-assignment-beside-docker p-bare-docker-to-a-script-that-invokes-its-first-argument \
  p-bare-docker-to-a-function-that-invokes-its-argument p-bare-docker-to-an-undefined-command \
  p-docker-valued-variable-from-a-sourced-lib p-docker-valued-variable-from-the-job-env \
  p-docker-valued-variable-from-the-step-env p-unresolved-env-expression-beside-docker

# The workflow env.
queue_tree precision-red-workflow-env
d="${QUEUE_DIR}"
pscript "${d}" fixture-p-env-user '#!/usr/bin/env bash\n"${RUNTIME}" frobnicate target\n'
qjob "${d}" p-docker-valued-variable-from-the-workflow-env "$(steps_run 'bash scripts/fixture-p-env-user.sh')"
yq e -i '.env.RUNTIME = "docker"' "${d}/${queue_wf}"
expect_queue_jobs_red queue-path-workflow-env-docker-value-fails "${d}" p-docker-valued-variable-from-the-workflow-env

# --- a sourced file shares the script's shell, so its assignments are the script's ---
queue_tree sourced-red
d="${QUEUE_DIR}"
pscript "${d}" fixture-s-lib '#!/usr/bin/env bash\nRT=docker\n'
# The script assigns a non-docker value, then sources a lib that sets docker.
pscript "${d}" fixture-s-after '#!/usr/bin/env bash\nRT=podman\nsource "$(dirname "$0")/fixture-s-lib.sh"\n"$RT" pull alpine:3.21\n'
qjob "${d}" s-own-value-then-a-sourced-docker-value "$(steps_run 'bash scripts/fixture-s-after.sh')"
# The lib is sourced first and the script assigns afterwards.
pscript "${d}" fixture-s-before '#!/usr/bin/env bash\nsource "$(dirname "$0")/fixture-s-lib.sh"\nRT=podman\n"$RT" frobnicate x\n'
qjob "${d}" s-sourced-docker-value-then-an-own-value "$(steps_run 'bash scripts/fixture-s-before.sh')"
# The dot form, and a chain: main sources a, a sources b, b sets docker.
pscript "${d}" fixture-s-dot '#!/usr/bin/env bash\nRT=podman\n. "$(dirname "$0")/fixture-s-lib.sh"\n"$RT" frobnicate x\n'
qjob "${d}" s-dot-form-sources-a-docker-value "$(steps_run 'bash scripts/fixture-s-dot.sh')"
pscript "${d}" fixture-s-chain-a '#!/usr/bin/env bash\nsource "$(dirname "$0")/fixture-s-lib.sh"\n'
pscript "${d}" fixture-s-chain-main '#!/usr/bin/env bash\nRT=podman\nsource "$(dirname "$0")/fixture-s-chain-a.sh"\n"$RT" frobnicate x\n'
qjob "${d}" s-doubly-sourced-docker-value "$(steps_run 'bash scripts/fixture-s-chain-main.sh')"
expect_queue_jobs_red sourced-assignments-count-as-the-scripts-own "${d}" s-own-value-then-a-sourced-docker-value \
  s-sourced-docker-value-then-an-own-value s-dot-form-sources-a-docker-value s-doubly-sourced-docker-value
queue_tree sourced-green
d="${QUEUE_DIR}"
# A script that is run, not sourced, has its own process: its assignment does not
# reach the caller.
pscript "${d}" fixture-s-other '#!/usr/bin/env bash\nRT=docker\n'
pscript "${d}" fixture-s-separate '#!/usr/bin/env bash\nRT=podman\nbash scripts/fixture-s-other.sh\n"$RT" frobnicate x\n'
qjob "${d}" s-separately-run-script-does-not-shadow-in "$(steps_run 'bash scripts/fixture-s-separate.sh')"
# A sourced lib that assigns the same name a non-docker value.
pscript "${d}" fixture-s-nerdctl '#!/usr/bin/env bash\nRT=nerdctl\n'
pscript "${d}" fixture-s-benign '#!/usr/bin/env bash\nRT=podman\nsource "$(dirname "$0")/fixture-s-nerdctl.sh"\n"$RT" frobnicate x\n'
qjob "${d}" s-sourced-lib-with-a-non-docker-value "$(steps_run 'bash scripts/fixture-s-benign.sh')"
expect_green sourced-and-separately-run-scripts-with-no-docker-value-pass "${d}"
