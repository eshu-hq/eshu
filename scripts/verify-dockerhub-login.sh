#!/usr/bin/env bash
#
# verify-dockerhub-login.sh - fails when a workflow job that can pull Docker Hub
# images has no authenticated pull path (#7886), and when any job of a
# merge_group or pull_request workflow reaches Docker Hub at all (#7886).
#
# Hosted runners share one anonymous Docker Hub identity, so an unauthenticated
# pull in a merge_group run can hit "toomanyrequests" and eject a green PR from
# the merge queue. Scope is DERIVED from every .github/workflows/*.yml, never
# listed by hand. A job is Docker Hub-reaching when:
#   - it declares a services: or container: image that comes from Docker Hub;
#   - a step's run: text invokes docker (run, create, pull, build, buildx,
#     image pull|build, container run|create, compose, docker-compose, or a
#     subcommand this gate does not know) naming an image from Docker Hub;
#   - a step's run: text runs a repository shell script (a path ending in .sh or
#     .bash that a command runs or sources, in command position, behind VAR=val
#     prefixes, behind env, sudo, nice, timeout, xargs or another wrapper with its
#     flags, behind a shell with flags or a path-qualified shell such as
#     /bin/bash, or as one of several calls on a line; see
#     lib/dockerhub-login-refs.awk) whose transitive invocations
#     (depth-limited, cycle-safe) do;
#   - a step uses docker/build-push-action or docker/bake-action and its
#     Dockerfile has a FROM that is not provably outside Docker Hub, a
#     docker://image container action, or a local composite action whose run
#     text invokes docker.
#   - a step uses docker/setup-buildx-action with the default docker-container
#     driver (it bootstraps a moby/buildkit builder image from Docker Hub at
#     setup; driver: docker or remote, or driver-opts image= from another
#     registry, pulls none) or docker/setup-qemu-action (its default image is
#     docker.io/tonistiigi/binfmt; an image: input from another registry pulls
#     none).
# docker/login-action, docker/metadata-action, the docker/setup-* installers that
# pull no image (setup-docker-action, setup-compose-action) and actions/* are not
# Docker uses.
#
# Queue path (lib/dockerhub-login-queue.sh): one rule set, derived from each
# workflow's on: and never from a list. A workflow is on the queue path when its
# on: has merge_group, pull_request or pull_request_target; when it runs on
# workflow_run and its workflows: list names (by name:, transitively, cycle-safe)
# a queue-path workflow; or when a queue-path workflow calls it as a reusable
# workflow. Both .yml and .yaml workflow files are read. EVERY job in a queue-path
# workflow must not reach Docker Hub. Such a job fails when it reaches Docker
# Hub by any class above, whether or not it keeps a login: a login does not make
# a queue-path reach acceptable, and a login is not a finding here (a queue-path
# job that reaches nothing and still carries one passes this rule). The login
# rule below applies only to jobs of every other workflow (push, tag, schedule,
# dispatch, or a workflow_run of a non-queue workflow only). There is no mode
# flag.
#
# Finding docker: lib/dockerhub-login-lex.awk lexes the shell text (quotes,
# comments, line continuations, ; & | ( ) and $(...)), so docker is recognised
# behind if/elif/while/until/then/do/else/!/{ and behind sudo, env, command, exec,
# nice, nohup, xargs, timeout and time with their flags and VAR=val prefixes;
# after global flags (--config, --context, -H, ...); inside bash|sh|zsh -c STRING
# and eval STRING; through any assignment whose value holds the word docker
# (NAME=docker, NAME="docker compose", NAME=(docker compose), NAME="${X:-docker}",
# NAME=$(command -v docker), also after then/else/do, and sticky: a later
# NAME=podman does not clear it) invoked later as $NAME or "${NAME[@]}"; with
# docker itself as the command word ("${X:-docker}", $(which docker)); and in
# "$(...)". A command that wraps docker (retry 3 docker pull x, run_it docker
# compose ... up) is read as the docker command it runs, so its subcommand and
# image are classified like a direct call; a bare trailing docker (require_tool
# docker) names the program and runs nothing. A bare docker word the lexer cannot
# place (a variable named like docker or compose invoked as a command without a
# docker assignment in the same file) counts as Docker Hub.
#
# A variable invoked as a command (V) is settled by name, against its assignments
# (lib/dockerhub-login-vconf.awk). A V whose script, or a script it sources (source
# or ., transitively), assigns it is settled by those assignments; a script that
# is only run shares no shell and does not count. A V never assigned there takes
# the assignments of the run text, the reached scripts and the step, job and
# workflow env. It may run docker when one assignment holds the word docker
# (rt=docker, NAME=(docker compose), NAME=$(command -v docker), a for-loop list),
# when one is unresolvable ($(...), a backquote, ${!x}, read) in a script that
# itself names docker, or when an unresolved ${{ }} env value reaches a script
# that names docker. A variable assigned only from its own function's positional
# parameter takes the caller's argument. A V that no script and no env assigns
# takes a value from outside the repository: it passes, a limit of this gate. A
# bare trailing docker passed to a callee ("require_tools fail docker") is settled
# by the callee: a function or script that invokes a variable assigned from that
# parameter, or one no reached script defines, may run docker. Words after docker
# (retry 3 docker pull x) are the docker command the wrapper runs. A variable
# invoked with a non-pulling subcommand (ps, exec, logs) pulls nothing. A for NAME
# in WORDS list is data. A [[ ]] test continued across lines is one command. A
# step whose run reads ${{ matrix.KEY }} is expanded once per static matrix cell
# (lib/dockerhub-login-matrix.awk); a matrix it cannot expand (fromJSON, a missing
# key) stays unresolved. A heredoc body handed to a shell is lexed like a script.
# A docker word in a comment, in a quoted string that no shell evaluates, or in a
# heredoc body that is data does not count. docker ps/exec/logs/rm/... and
# docker compose ps/logs/down/... pull and build nothing and do not count.
#
# A docker command reaches Docker Hub by the HOST of the image it names (derived
# in lib/dockerhub-login-images.sh): a ref with no host, or docker.io (also
# docker.io/library/x) / index.docker.io / registry-1.docker.io /
# registry.hub.docker.com. Any other host (ghcr.io, gcr.io, ...)
# is excluded by host, never by a name list. The host is read from docker
# run/create/pull arguments and docker buildx imagetools refs, from the FROM
# lines of the Dockerfile a build or a compose build: names, and from compose
# image: lines. Variables expand from the script's own assignments, then the
# process env: the step env, the job env and the workflow env (also
# ${{ env.X }}), so a job or workflow env value beats a ${VAR:-ghcr.io/...}
# default. Compose files (lib/dockerhub-login-compose.sh) follow extends: (same
# file or file: + service:) and include:; a service that extends takes the
# base's image or build unless it overrides it. Digest pins do not avoid the
# limit. Anything not derivable counts as Docker Hub: an unresolved variable or
# an expression (inputs, a matrix it cannot expand, secrets, vars) in the host, a
# compose file the script does not name (names in full-line comments do not
# count), an extends/include that cannot be resolved or cycles, an inline
# Dockerfile, a build context that is a URL, an unparsable docker run, a docker
# pull with no readable image, docker buildx create/bootstrap.
#
# Compose -f operands (lib/dockerhub-login-compose-flow.awk): every -f operand and
# COMPOSE_FILE entry of a compose invocation must settle to literal compose files
# of the tree, by the literal or by every assignment (in the script and the files
# it sources, not in a script it only runs), for-in word, env value and default
# of the variable it names, and by the arguments of every call of the function
# whose parameter it is. One that does not (an unassigned, read or
# command-substitution variable, an expression, an array, a script argument, a
# file the tree lacks) fails the job even beside an override-only or ghcr.io file.
#
# A reaching job outside the queue path needs:
#   - services:/container: Hub images with credentials: from the DOCKERHUB_*
#     secrets (pulled at job init, before any step, so a login cannot cover them);
#   - for steps, a docker/login-action@v3 step with no registry: (or docker.io),
#     BEFORE the first reaching step, which includes a setup-buildx or setup-qemu
#     step (a login placed after one does not cover its pull), gated by `env.DOCKERHUB_LOGIN_ENABLED == 'true'`, with no continue-on-error
#     (a failed login would fall back to anonymous pulls and hide invalid
#     secrets), using the DOCKERHUB_USERNAME / DOCKERHUB_TOKEN secrets, with the
#     job (or workflow) env building DOCKERHUB_LOGIN_ENABLED from both secrets.
# A login to ghcr.io does not count.
#
# Behavior with invalid secrets: when both secrets are set but wrong (revoked
# token, wrong user), the flag is true and docker/login-action FAILS in every
# reaching job, and a service container with credentials: fails at job init.
# Those jobs go red rather than falling back to anonymous pulls. Absent secrets
# (forks, Dependabot) leave the flag false: the login is skipped and pulls stay
# anonymous. The gate only checks wiring; it cannot check the secret values.
#
# A job-level uses: (reusable workflow call) has no steps; the callee workflow is
# itself scanned as one of the workflow files.
#
# Remaining limits (not detected): a third-party action that runs docker inside
# itself; a make target or other build tool that runs docker (the Makefile is not
# followed); a script named only inside a quoted string, in a comment or built
# from variables (bash -c 'scripts/x.sh' is not followed for the script, only
# lexed for docker; for s in scripts/a.sh; do bash "$s"; done is not followed; a
# script named in a double-quoted string is followed only when the string starts
# with its path); a script run behind a command this gate does not know as a
# wrapper (retry 3 scripts/x.sh; retry 3 bash scripts/x.sh is followed, the shell
# word is enough); a script named as an argument of a command that does not run
# it is not followed (shellcheck scripts/x.sh, test -f scripts/x.sh);
# docker run through ssh or a quoted string no shell evaluates; a registry pull
# done by another client or runtime (python subprocess or the docker SDK, skopeo,
# crane, kind load, podman, nerdctl); a nested local composite action (a composite
# action's run steps and the scripts they name are followed, an action that uses
# another local action is not); a script that sets its runtime variable only in a
# file no workflow step reaches; compose files found by default name
# (compose.yaml) are counted as Hub when none is named, and not followed
# otherwise; a COMPOSE_FILE set only in a .env file; a function reached only
# through a variable or an eval string (its parameters have no call to settle
# from); a command variable that no script, step or env assigns (a runner or
# non-shell-file value).
#
# ESHU_DOCKERHUB_LOGIN_ROOT points the verifier at a scratch repo tree.
# ESHU_DOCKERHUB_LOGIN_EXPLAIN=1 prints the derived scope table;
# ESHU_DOCKERHUB_LOGIN_TRACE=1 prints each evaluated step.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourced libs
set -euo pipefail

unresolved_rule="An image host that cannot be resolved statically, for example an environment-variable reference without a default, counts as Docker Hub, because that is the only fail-closed rule."
case "${1:-}" in
  -h | --help)
    printf 'usage: verify-dockerhub-login.sh [-h|--help]\n'
    printf 'Fails when a workflow job that can pull Docker Hub images has no gated Docker Hub login,\n'
    printf 'and when any job of a merge_group or pull_request workflow reaches Docker Hub at all.\n'
    printf 'ESHU_DOCKERHUB_LOGIN_EXPLAIN=1 prints the derived scope table; ESHU_DOCKERHUB_LOGIN_ROOT selects a tree.\n'
    printf '%s\n' "${unresolved_rule}"
    exit 0 ;;
esac

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
root="${ESHU_DOCKERHUB_LOGIN_ROOT:-${repo_root}}"
explain="${ESHU_DOCKERHUB_LOGIN_EXPLAIN:-}"
trace="${ESHU_DOCKERHUB_LOGIN_TRACE:-}"
max_depth=8
user_expr='${{ secrets.DOCKERHUB_USERNAME }}'
token_expr='${{ secrets.DOCKERHUB_TOKEN }}'
enabled_expr="\${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}"
gate_expr="env.DOCKERHUB_LOGIN_ENABLED == 'true'"

for tool in yq rg base64 awk; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "verify-dockerhub-login: ${tool} is required" >&2; exit 1; }
done
[[ -d "${root}/.github/workflows" ]] || { echo "verify-dockerhub-login: ${root}/.github/workflows is missing" >&2; exit 1; }
root="$(cd "${root}" && pwd -P)"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
failures=0
fail() { printf 'verify-dockerhub-login: %s\n' "$1" >&2; failures=$((failures + 1)); }
b64d() { base64 -d 2>/dev/null || base64 -D; }

# queue_job_check <where> <init reasons> <first reach idx> <why>: one failure
# naming every reach of a queue-path job (a job of a workflow that runs on
# merge_group or pull_request).
queue_job_check() {
  local problems=""
  [[ -z "$2" ]] || problems="${problems}${problems:+; }job init pulls Docker Hub image(s): $2"
  [[ -z "$3" ]] || problems="${problems}${problems:+; }steps[$3]: $4"
  [[ -z "${problems}" ]] || fail "$1: queue-path job (its workflow runs on merge_group or pull_request) must not reach Docker Hub: ${problems}"
}

# Regexes (Rust syntax; a # starts a comment only at line start or after
# whitespace, so $# and ${x#y} are not comments). bare_re only selects which
# scripts to lex: any bare docker word, or a variable named like docker or
# compose. Docker uses are found by lib/dockerhub-login-lex.awk (it lexes the
# shell text) and script references by lib/dockerhub-login-refs.awk.
bare_re='(?:^|[^A-Za-z0-9_./$-])docker(?:-compose)?(?:[^A-Za-z0-9_./-]|$)|\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=+]docker(?:-compose)?(?:[^A-Za-z0-9_./-]|$)|\$\{?[A-Za-z0-9_]*(?i:docker|compose)'
lex_awk="${repo_root}/scripts/lib/dockerhub-login-lex.awk"
lex_params_awk="${repo_root}/scripts/lib/dockerhub-login-lex-params.awk"
refs_awk="${repo_root}/scripts/lib/dockerhub-login-refs.awk"
flow_awk="${repo_root}/scripts/lib/dockerhub-login-compose-flow.awk"
compose_file_re='[A-Za-z0-9_./-]*docker-compose[A-Za-z0-9_.-]*\.ya?ml'
rg_scripts=(--no-ignore --hidden -g '*.sh' -g '*.bash' -g '!.git' -g '!node_modules')

# shellcheck source=lib/dockerhub-login-scripts.sh disable=SC2034
source "${repo_root}/scripts/lib/dockerhub-login-scripts.sh"

# --- batch scan of every repo shell script: docker facts and invoked scripts -----
other_files=$'\n'; compose_files=$'\n'; unclass_files=$'\n'; var_files=$'\n'; dock_files=$'\n'; lexed_files=$'\n'
lex_inputs=()
while IFS= read -r path; do
  [[ -z "${path}" ]] || { lex_inputs+=("${path}"); lexed_files="${lexed_files}${path#"${root}"/}"$'\n'; }
done < <({ rg -l -e "${bare_re}" "${rg_scripts[@]}" "${root}" || true; } | sort)
lex_load repo ${lex_inputs[@]+"${lex_inputs[@]}"}
# Every script the repo names another script from: refs.awk finds the references
# (any path ending in .sh or .bash outside a comment and a quoted string).
ref_inputs=()
while IFS= read -r path; do
  [[ -z "${path}" ]] || ref_inputs+=("${path}")
done < <({ rg --files "${rg_scripts[@]}" "${root}" || true; } | sort)
while IFS=: read -r path tok; do
  [[ -n "${path}" ]] || continue
  path="${path#"${root}"/}"
  if [[ "${path}" == */* ]]; then pdir="${path%/*}"; else pdir="."; fi
  resolve_token "${tok}" "${pdir}"
  [[ -n "${RESOLVED}" && "${RESOLVED}" != "${path}" ]] || continue
  mkkey "${path}"; cur="FR_${KEY}"
  printf -v "${cur}" '%s%s\n' "${!cur:-}" "${RESOLVED}"
done < <([[ "${#ref_inputs[@]}" -eq 0 ]] || LC_ALL=C awk -f "${refs_awk}" "${ref_inputs[@]}")

# shellcheck source=lib/dockerhub-login-images.sh disable=SC2034
source "${repo_root}/scripts/lib/dockerhub-login-images.sh"
# shellcheck source=lib/dockerhub-login-compose.sh disable=SC2034
source "${repo_root}/scripts/lib/dockerhub-login-compose.sh"
# shellcheck source=lib/dockerhub-login-queue.sh disable=SC2034
source "${repo_root}/scripts/lib/dockerhub-login-queue.sh"
# shellcheck source=lib/dockerhub-login-matrix.sh disable=SC2034
source "${repo_root}/scripts/lib/dockerhub-login-matrix.sh"

# check_job <workflow> <job> <workflow env> <queue 0|1>: evaluates one job. A job
# of a queue-path workflow must not reach Docker Hub or keep any Docker Hub login
# wiring; every other job needs the authenticated pull path.
check_job() {
  local name="$1" job="$2" wf_env="$3" queue="$4" where="$1:$2" job_env kind sname image cuser cpass reasons="" qtag=""
  local seq idx uses cond reg user pass dfile dctx why first_reach="" first_why=""
  clear_tables
  load_envs "${job}"
  local login_idx="" login_if="" login_user="" login_pass="" login_uses="" login_coe=""
  local df dc act af ref cur runf dynamic cfiles d_other d_comp d_unclass d_var d_dock possible env64 coe drv dopts qimg bimg opt aref
  job_env="$(awk -F'\t' -v j="${job}" '$1=="J" && $2==j {print substr($3,2)}' "${tmp}/jobs.tsv" | head -n 1)"

  # Image pulls at job init: every Hub services:/container: image needs credentials.
  while IFS=$'\t' read -r kind _ sname image cuser cpass; do
    [[ -n "${kind}" && -n "${image#x}" ]] || continue
    if [[ "${kind}" == S ]]; then sname="service ${sname}"; else sname="container"; fi
    image_is_hub "${image#x}" || continue
    reasons="${reasons}${reasons:+; }${sname} ${image#x}"
    if [[ "${queue}" -eq 0 && ( "${cuser#x}" != "${user_expr}" || "${cpass#x}" != "${token_expr}" ) ]]; then
      fail "${where}: ${sname} (${image#x}) pulls from Docker Hub at job init without credentials: from secrets.DOCKERHUB_USERNAME and secrets.DOCKERHUB_TOKEN"
    fi
  done < <(awk -F'\t' -v j="${job}" '($1=="S" || $1=="C") && $2==j' "${tmp}/jobs.tsv")

  while IFS=$'\t' read -r seq _ idx uses cond reg user pass dfile dctx env64 coe drv dopts qimg; do
    uses="${uses#x}"; reg="${reg#x}"; env64="${env64#x}"; coe="${coe#x}"; why=""
    case "${uses}" in
      docker/login-action@*)
        case "${reg}" in
          ""|docker.io|index.docker.io|registry-1.docker.io)
            if [[ -z "${login_idx}" ]]; then
              login_idx="${idx}"; login_uses="${uses}"
              login_if="$(printf '%s' "${cond#x}" | b64d | tr -d '\n')"
              login_user="${user#x}"; login_pass="${pass#x}"; login_coe="${coe}"
            fi ;;
        esac
        continue ;;
      docker/build-push-action@*|docker/bake-action@*)
        df="${dfile#x}"; df="${df#./}"
        if [[ -z "${df}" ]]; then dc="${dctx#x}"; dc="${dc#./}"; [[ "${dc}" == . ]] && dc=""; df="${dc:+${dc}/}Dockerfile"; fi
        if dockerfile_is_hub "${df}"; then why="${uses%@*} builds ${df}, which has a Docker Hub FROM"; fi ;;
      docker/setup-buildx-action@*)
        # The default docker-container driver bootstraps a moby/buildkit builder
        # image from Docker Hub at setup; docker and remote pull none.
        case "${drv#x}" in
          docker | remote) ;;
          *)
            bimg="moby/buildkit"
            while IFS= read -r opt || [[ -n "${opt}" ]]; do
              case "${opt}" in image=*) bimg="${opt#image=}" ;; esac
            done < <(printf '%s' "${dopts#x}" | b64d | tr ',' '\n')
            if image_is_hub "${bimg}"; then why="${uses%@*} bootstraps the ${bimg} builder image from Docker Hub"; fi ;;
        esac ;;
      docker/setup-qemu-action@*)
        qimg="${qimg#x}"; qimg="${qimg:-docker.io/tonistiigi/binfmt:latest}"
        if image_is_hub "${qimg}"; then why="${uses%@*} pulls ${qimg} from Docker Hub"; fi ;;
      docker://*)
        if image_is_hub "${uses#docker://}"; then why="${uses} is a container action pulled from Docker Hub"; fi ;;
      ./*)
        act="${uses#./}"; act="${act%/}"
        for af in "${act}/action.yml" "${act}/action.yaml"; do
          [[ -f "${root}/${af}" ]] || continue
          file_kind "${af}"
          [[ "${KIND}" == none ]] || why="local action ${uses} invokes docker"
          [[ -z "${why}" ]] || continue
          # A composite action's run steps may call a repository script (the run
          # text is in ${tmp}/action-run.txt, written by file_kind).
          while IFS= read -r aref; do
            [[ -n "${aref}" ]] || continue
            resolve_token "${aref}" "${act}"
            [[ -n "${RESOLVED}" ]] || continue
            closure "${RESOLVED}"
            [[ "${closure_kind}" == none ]] || why="local action ${uses} runs ${RESOLVED}, which reaches docker (${closure_chain})"
          done < <(LC_ALL=C awk -v bare=1 -f "${refs_awk}" "${tmp}/action-run.txt")
        done ;;
      "")
        runf="${tmp}/runs/${seq}"
        [[ -f "${runf}" ]] || continue
        d_other=0; d_comp=0; d_unclass=0; d_var=0; d_dock=0
        [[ "${runs_other}" != *$'\n'"${seq}"$'\n'* ]] || d_other=1
        [[ "${runs_var}" != *$'\n'"${seq}"$'\n'* ]] || d_var=1
        [[ "${runs_dock}" != *$'\n'"${seq}"$'\n'* ]] || d_dock=1
        [[ "${runs_compose}" != *$'\n'"${seq}"$'\n'* ]] || d_comp=1
        [[ "${runs_unclass}" != *$'\n'"${seq}"$'\n'* ]] || d_unclass=1
        cfiles=""; cur="SR_${seq}"
        # A step whose whole run is an expression (a matrix command) is dynamic:
        # any script the workflow names may run, so use the workflow's script set.
        if [[ "$(<"${runf}")" =~ ^[[:space:]]*\$\{\{[^}]*\}\}[[:space:]]*$ ]]; then
          cur="loose_refs"; dynamic=1
        else dynamic=0; fi
        possible=$((d_other + d_comp + d_unclass))
        while IFS= read -r ref; do
          [[ -n "${ref}" ]] || continue
          closure "${ref}"
          cfiles="${cfiles}${closure_files}"$'\n'
          [[ "${closure_kind}" == none ]] || possible=1
        done <<<"${!cur:-}"
        # A variable invoked as a command next to a docker word anywhere in the
        # run text or the scripts it reaches may hold docker: fail closed.
        var_conflict "${seq}" "${cfiles}" "${env64}" "${job}"
        [[ -z "${VCONF}" ]] || possible=1
        [[ "${possible}" -gt 0 ]] || continue
        step_hub_why "${runf}" "${d_other}" "${d_comp}" "${cfiles}" "${env64}" "${job}"
        [[ -z "${trace}" ]] || printf 'trace %s steps[%s] other=%s compose=%s files=%s -> %s\n' "${where}" "${idx}" "${d_other}" "${d_comp}" "$(printf '%s' "${cfiles}" | rg -c .)" "${HUBWHY:-none}" >&2
        if [[ -n "${HUBWHY}" ]]; then
          case "${HUBWHY}" in
            run:*) why="run invokes${HUBWHY#run:}" ;;
            *) why="run reaches ${HUBWHY}" ;;
          esac
          if [[ "${dynamic}" -eq 1 ]]; then why="run is an expression; the workflow names a script that ${why#run }"; fi
        fi ;;
    esac
    if [[ -n "${why}" && -z "${first_reach}" ]]; then first_reach="${idx}"; first_why="${why}"; fi
  done < <(awk -F'\t' -v j="${job}" '$2==j' "${tmp}/steps.tsv")

  qtag=""; [[ "${queue}" -eq 0 ]] || qtag="[queue-path] "
  [[ -z "${reasons}" ]] || printf '%s\t%s\n' "${where}" "${qtag}job init pulls Docker Hub image(s): ${reasons}" >>"${tmp}/scope.tsv"
  [[ -z "${first_reach}" ]] || printf '%s\t%s\n' "${where}" "${qtag}steps[${first_reach}]: ${first_why}" >>"${tmp}/scope.tsv"
  [[ -z "${reasons}" && -z "${first_reach}" ]] || reaching_jobs=$((reaching_jobs + 1))
  if [[ "${queue}" -eq 1 ]]; then
    queue_job_check "${where}" "${reasons}" "${first_reach}" "${first_why}"
    return 0
  fi
  [[ -n "${first_reach}" ]] || return 0

  if [[ -z "${login_idx}" ]]; then
    fail "${where}: no Docker Hub docker/login-action@v3 step before steps[${first_reach}] (${first_why}); a login to another registry does not count"
    return 0
  fi
  if [[ "${login_idx}" -ge "${first_reach}" ]]; then
    fail "${where}: login steps[${login_idx}] comes after the first Docker-reaching steps[${first_reach}] (${first_why})"
    return 0
  fi
  case "${login_coe}" in
    "" | false) ;;
    *) fail "${where}: login steps[${login_idx}] sets continue-on-error (${login_coe}); a failed login would fall back to anonymous pulls silently" ;;
  esac
  [[ "${login_uses}" == docker/login-action@v3 || "${login_uses}" == docker/login-action@v3.* ]] ||
    fail "${where}: steps[${login_idx}] must use docker/login-action@v3, not ${login_uses}"
  [[ "${login_if}" == "${gate_expr}" ]] ||
    fail "${where}: login steps[${login_idx}] must run only when DOCKERHUB_LOGIN_ENABLED is true (if: ${gate_expr})"
  [[ "${login_user}" == "${user_expr}" && "${login_pass}" == "${token_expr}" ]] ||
    fail "${where}: login steps[${login_idx}] must use the DOCKERHUB_USERNAME and DOCKERHUB_TOKEN secrets"
  [[ "${job_env:-${wf_env}}" == "${enabled_expr}" ]] ||
    fail "${where}: job env must define DOCKERHUB_LOGIN_ENABLED from both secrets (${enabled_expr})"
}

: >"${tmp}/scope.tsv"
workflows_seen=0
reaching_jobs=0
queue_path_workflows

for wf in ${wf_files[@]+"${wf_files[@]}"}; do
  name="$(basename "${wf}")"
  workflows_seen=$((workflows_seen + 1))
  if ! yq e '.jobs | keys | .[]' "${wf}" >"${tmp}/jobs.list" 2>"${tmp}/yq.err"; then
    fail "${name}: cannot parse jobs ($(head -n 1 "${tmp}/yq.err"))"; continue
  fi
  queue=0
  [[ "${queue_wfs}" != *$'\n'"${name}"$'\n'* ]] || queue=1
  wf_env="$(yq e '(.env | select(type == "!!map") | .DOCKERHUB_LOGIN_ENABLED) // ""' "${wf}")"
  yq e '.jobs | to_entries | .[] | .key as $j | (
      (["J", $j, "x" + (((.value.env | select(type == "!!map") | .DOCKERHUB_LOGIN_ENABLED) // "") | tostring), "x" + (.value.uses // "")] | join("\t")),
      ((.value.services | select(type == "!!map") | to_entries | .[] | ["S", $j, .key, "x" + (.value.image // ""), "x" + (.value.credentials.username // ""), "x" + (.value.credentials.password // "")] | join("\t")) // ""),
      ((.value.container | select(type == "!!str") | ["C", $j, "container", "x" + ., "x", "x"] | join("\t")) // ""),
      ((.value.container | select(type == "!!map") | ["C", $j, "container", "x" + (.image // ""), "x" + (.credentials.username // ""), "x" + (.credentials.password // "")] | join("\t")) // "")
    )' "${wf}" | sed '/^$/d;/^null$/d' >"${tmp}/jobs.tsv"
  # steps.tsv: seq, job, idx, uses, if (base64), with.registry/username/password/file/context, env (base64 NAME=VALUE lines), continue-on-error, with.driver, with.driver-opts (base64), with.image.
  yq e '.jobs | to_entries | .[] | select(.value.steps | type == "!!seq") | select(.value.steps | length > 0) | .key as $j | ((.value.steps // []) | to_entries | .[] |
      [$j, (.key | tostring), "x" + (.value.uses // ""), "x" + ((.value.if // "") | tostring | @base64),
       "x" + ((.value.with.registry // "") | tostring), "x" + ((.value.with.username // "") | tostring),
       "x" + ((.value.with.password // "") | tostring), "x" + ((.value.with.file // "") | tostring),
       "x" + ((.value.with.context // "") | tostring),
       "x" + (((.value.env | select(type == "!!map") | to_entries | map(.key + "=" + (.value | tostring)) | join("\n")) // "") | @base64),
       "x" + ((.value["continue-on-error"] // "") | tostring),
       "x" + ((.value.with.driver // "") | tostring), "x" + (((.value.with["driver-opts"] // "") | tostring) | @base64),
       "x" + ((.value.with.image // "") | tostring)] | join("\t"))' "${wf}" | awk '{print NR "\t" $0}' >"${tmp}/steps.tsv"
  yq e '(.env | select(type == "!!map") | to_entries | .[] | [.key, (.value | tostring)] | join("\t"))' "${wf}" >"${tmp}/wfenv.tsv"
  yq e '.jobs | to_entries | .[] | .key as $j | (.value.env | select(type == "!!map") | to_entries | .[] | [$j, .key, (.value | tostring)] | join("\t"))' "${wf}" >"${tmp}/jobenv.tsv"
  # One file per step run text, named by the step's sequence number.
  rm -rf "${tmp}/runs"; mkdir -p "${tmp}/runs"
  yq e '.jobs | to_entries | .[] | select(.value.steps | type == "!!seq") | select(.value.steps | length > 0) | ((.value.steps // []) | to_entries | .[] | ("@@STEP\n" + (.value.run // "")))' "${wf}" |
    awk -v dir="${tmp}/runs" '/^@@STEP$/ { if (out != "") close(out); n++; out = dir "/" n; next } { print > out }'
  matrix_expand_runs "${wf}"
  runs_other=$'\n'; runs_compose=$'\n'; runs_unclass=$'\n'; runs_var=$'\n'; runs_dock=$'\n'
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v RO_ || true; compgen -v RU_ || true; compgen -v RV_ || true)
  run_inputs=()
  for rf in "${tmp}"/runs/*; do
    [[ ! -f "${rf}" ]] || run_inputs+=("${rf}")
  done
  lex_load run ${run_inputs[@]+"${run_inputs[@]}"}
  while IFS=: read -r path tok; do
    [[ -n "${path}" ]] || continue
    resolve_token "${tok}" "."
    [[ -n "${RESOLVED}" ]] || continue
    cur="SR_${path##*/}"
    printf -v "${cur}" '%s%s\n' "${!cur:-}" "${RESOLVED}"
  done < <([[ "${#run_inputs[@]}" -eq 0 ]] || LC_ALL=C awk -f "${refs_awk}" "${run_inputs[@]}")
  # Every script the workflow's run texts name (for dynamic expression steps).
  loose_refs=""
  while IFS= read -r tok; do
    [[ -n "${tok}" ]] || continue
    resolve_token "${tok}" "."
    [[ -z "${RESOLVED}" ]] || loose_refs="${loose_refs}${RESOLVED}"$'\n'
  done < <({ rg -o -N --no-filename -e '[A-Za-z0-9_./-]+\.(sh|bash)\b' "${tmp}/runs" || true; } | sort -u)

  while IFS= read -r job; do
    check_job "${name}" "${job}" "${wf_env}" "${queue}"
  done <"${tmp}/jobs.list"
  # Clear the per-workflow step ref variables.
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v SR_ || true)
done

if [[ -n "${explain}" ]]; then
  printf 'scope: %s Docker Hub-reaching job(s) derived from %s workflow file(s)\n' "${reaching_jobs}" "${workflows_seen}"
  printf 'queue-path: %s workflow file(s) are on the queue path (merge_group, pull_request, pull_request_target, or following one); every job in them must pull from ghcr.io only\n' "$(printf '%s' "${queue_wfs}" | rg -c .)"
  sort "${tmp}/scope.tsv" | awk -F'\t' '{printf "  %s\t%s\n", $1, $2}'
  printf 'rule: %s\n' "${unresolved_rule}"
fi
if [[ "${failures}" -gt 0 ]]; then
  echo "verify-dockerhub-login: ${failures} problem(s)" >&2
  exit 1
fi
echo "verify-dockerhub-login: ok (${reaching_jobs} Docker Hub-reaching job(s) across ${workflows_seen} workflow file(s))"
