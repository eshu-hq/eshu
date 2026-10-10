#!/usr/bin/env bash
#
# dockerhub-login-images.sh - image-host derivation for verify-dockerhub-login.sh
# (#7886). Sourced, never run. It decides, from the text of a step and the
# scripts it reaches, whether a docker command can pull from Docker Hub.
#
# The caller defines: root (repo tree), tmp (scratch dir), compose_file_re, mkkey,
# the lexed fact tables (FO_/FU_ per script, RO_/RU_ per run step, see the
# verifier) and the sets other_files and compose_files. A host is Docker Hub when
# the ref has no registry host or the host is docker.io / index.docker.io /
# registry-1.docker.io. Every other host (ghcr.io, gcr.io, ...) is excluded by
# host. A ref that cannot be expanded to a known host counts as Docker Hub.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier
#
# Variable tables: E_<name> holds the step env, J_<name> the job env and
# W_<name> the workflow env (together the process env the script sees, step over
# job over workflow); V_<name> holds script assignments. All are cleared per
# job and the step table per step.

# bash 5.2 treats & in a ${v/pat/rep} replacement as the match; keep it literal.
shopt -u patsub_replacement 2>/dev/null || true

assign_re='^[[:space:]]*(?:export[[:space:]]+|readonly[[:space:]]+|declare[[:space:]]+(?:-[A-Za-z]+[[:space:]]+)?|local[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$'
default_assign_re=':[[:space:]]+"?\$\{([A-Za-z_][A-Za-z0-9_]*):[-=]([^}]*)\}"?'
run_value_flags=" --add-host --attach --blkio-weight --cap-add --cap-drop --cgroup-parent --cgroupns --cidfile --cpu-period --cpu-quota --cpu-shares --cpus --cpuset-cpus --device --dns --dns-option --dns-search --domainname --entrypoint --env --env-file --expose --gpus --group-add --health-cmd --health-interval --health-retries --health-start-period --health-timeout --hostname --ip --ip6 --ipc --isolation --kernel-memory --label --label-file --link --log-driver --log-opt --mac-address --memory --memory-reservation --memory-swap --mount --name --net --network --network-alias --oom-score-adj --pid --pids-limit --platform --publish --pull --restart --runtime --security-opt --shm-size --stop-signal --stop-timeout --storage-opt --sysctl --tmpfs --ulimit --user --userns --uts --volume --volumes-from --workdir -a -c -e -h -l -m -p -u -v -w "
build_value_flags=" --add-host --allow --annotation --attest --build-arg --build-context --builder --cache-from --cache-to --cgroup-parent --iidfile --label --metadata-file --network --no-cache-filter --output --platform --progress --provenance --pull --sbom --secret --shm-size --ssh --tag --target --ulimit -f -o -t --file "

clear_step_tables() {
  local v
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v E_ || true)
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v V_ || true)
}

clear_tables() {
  local v
  clear_step_tables
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v J_ || true)
  while IFS= read -r v; do unset "${v}"; done < <(compgen -v W_ || true)
}

# load_envs <job>: loads the workflow env and the job's env into W_ and J_.
load_envs() {
  local j k val
  while IFS=$'\t' read -r k val; do
    [[ "${k}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    printf -v "W_${k}" '%s' "${val}"
  done <"${tmp}/wfenv.tsv"
  while IFS=$'\t' read -r j k val; do
    [[ "${j}" == "$1" && "${k}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    printf -v "J_${k}" '%s' "${val}"
  done <"${tmp}/jobenv.tsv"
}

# proc_env <name>: sets LOOK to the value the process env gives <name>: the step
# env, then the job env, then the workflow env ("" when undefined).
proc_env() {
  local n="E_$1"
  LOOK=""
  if [[ -n "${!n+x}" ]]; then LOOK="${!n}"; return 0; fi
  n="J_$1"
  if [[ -n "${!n+x}" ]]; then LOOK="${!n}"; return 0; fi
  n="W_$1"
  LOOK="${!n:-}"
}

# expand <value> [<excluded name>]: expands ${{ env.X }}, ${X}, ${X:-d}, ${X:?m}
# and $X from the tables into EXP; an unknown name becomes @UNRESOLVED@. A name
# reads the script assignments, then the process env (step, job, workflow). The
# excluded name skips the script assignments (for NAME="${NAME:-default}").
# An expression this gate cannot evaluate (inputs, matrix, secrets, vars) stays
# in the text; in the host position it makes image_is_hub count the ref as Hub.
expand() {
  local v="$1" ex="${2:-}" n=0 whole name op def val tn
  while [[ "${n}" -lt 8 ]]; do
    n=$((n + 1))
    if [[ "${v}" =~ \$\{\{[[:space:]]*env\.([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\}\} ]]; then
      whole="${BASH_REMATCH[0]}"; proc_env "${BASH_REMATCH[1]}"
      v="${v/"${whole}"/${LOOK:-@UNRESOLVED@}}"
    elif [[ "${v}" =~ \$\{([A-Za-z_][A-Za-z0-9_]*)(:?[-=?+])?([^}]*)\} ]]; then
      whole="${BASH_REMATCH[0]}"; name="${BASH_REMATCH[1]}"; op="${BASH_REMATCH[2]}"; def="${BASH_REMATCH[3]}"
      val=""
      if [[ "${name}" != "${ex}" ]]; then tn="V_${name}"; val="${!tn:-}"; fi
      if [[ -z "${val}" ]]; then proc_env "${name}"; val="${LOOK}"; fi
      if [[ -z "${val}" ]]; then
        case "${op}" in :- | :=) val="${def}" ;; *) val="@UNRESOLVED@" ;; esac
      fi
      v="${v/"${whole}"/${val}}"
    elif [[ "${v}" =~ \$([A-Za-z_][A-Za-z0-9_]*) ]]; then
      whole="${BASH_REMATCH[0]}"; name="${BASH_REMATCH[1]}"
      val=""
      if [[ "${name}" != "${ex}" ]]; then tn="V_${name}"; val="${!tn:-}"; fi
      if [[ -z "${val}" ]]; then proc_env "${name}"; val="${LOOK}"; fi
      v="${v/"${whole}"/${val:-@UNRESOLVED@}}"
    else
      break
    fi
  done
  EXP="${v}"
}

# unquote <value>: strips one layer of matching quotes into UNQ.
unquote() {
  UNQ="$1"
  case "${UNQ}" in
    \"*\") UNQ="${UNQ#\"}"; UNQ="${UNQ%\"}" ;;
    \'*\') UNQ="${UNQ#\'}"; UNQ="${UNQ%\'}" ;;
  esac
}

# image_is_hub <ref>: returns 0 when the ref can pull from Docker Hub or its host
# cannot be derived; 1 when the host is another registry.
image_is_hub() {
  local ref first
  expand "$1" ""
  ref="${EXP}"
  [[ -n "${ref}" ]] || return 0
  [[ "${ref}" == */* ]] || return 0
  first="${ref%%/*}"
  [[ "${first}" != *'$'* && "${first}" != *@UNRESOLVED@* && "${first}" != *'{'* ]] || return 0
  if [[ "${first}" == *.* || "${first}" == *:* || "${first}" == localhost ]]; then
    case "${first}" in docker.io | index.docker.io | registry-1.docker.io | registry.hub.docker.com) return 0 ;; esac
    return 1
  fi
  return 0
}

# dockerfile_is_hub <repo-relative Dockerfile>: 0 when any FROM can pull from
# Docker Hub (scratch and earlier stage aliases excluded); 0 when unreadable.
dockerfile_is_hub() {
  local f="$1" line ref aliases="|" tok prev
  [[ -f "${root}/${f}" ]] || return 0
  while IFS= read -r line; do
    ref=""; prev=""
    for tok in ${line}; do
      case "${tok}" in
        [Ff][Rr][Oo][Mm] | --*) ;;
        *) if [[ -z "${ref}" ]]; then ref="${tok}"; fi ;;
      esac
      case "${prev}" in [Aa][Ss]) aliases="${aliases}${tok}|" ;; esac
      prev="${tok}"
    done
    [[ -n "${ref}" && "${ref}" != scratch && "${aliases}" != *"|${ref}|"* ]] || continue
    if image_is_hub "${ref}"; then return 0; fi
  done < <(rg -N -i -e '^[[:space:]]*FROM[[:space:]]+' "${root}/${f}" || true)
  return 1
}

# text_assigns <file>: prints NAME<TAB>VALUE assignments, cached per file.
text_assigns() {
  local c
  mkkey "$1"; c="AS_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    printf -v "${c}" '%s' "$({
      rg -o -N --no-filename -r $'$1\t$2' -e "${assign_re}" "$1" || true
      rg -o -N --no-filename -r $'$1\t$2' -e "${default_assign_re}" "$1" || true
    })"
  fi
  printf '%s\n' "${!c}"
}

# load_assigns <file>...: applies each file's assignments to the V_ table.
load_assigns() {
  local f name raw
  for f in "$@"; do
    while IFS=$'\t' read -r name raw; do
      [[ -n "${name}" ]] || continue
      raw="${raw%%[[:space:]]#*}"
      unquote "${raw}"
      expand "${UNQ}" "${name}"
      printf -v "V_${name}" '%s' "${EXP}"
    done < <(text_assigns "${f}")
  done
}

# run_image <tokens...>: prints the image token of a docker run/create argument
# list, skipping flags and their values.
run_image() {
  local t skip=0
  for t in "$@"; do
    if [[ "${skip}" -eq 1 ]]; then skip=0; continue; fi
    case "${t}" in
      --*=*) ;;
      --* | -?) if [[ "${run_value_flags}" == *" ${t} "* ]]; then skip=1; fi ;;
      -*) ;;
      *) printf '%s' "${t}"; return 0 ;;
    esac
  done
  return 1
}

# build_context <tokens...>: sets BUILD_FILE (-f/--file value or "") and
# BUILD_CTX (the last positional token).
build_context() {
  local t skip=0 prev="" last=""
  BUILD_FILE=""
  for t in "$@"; do
    if [[ "${skip}" -eq 1 ]]; then
      skip=0
      case "${prev}" in -f | --file) BUILD_FILE="${t}" ;; esac
      continue
    fi
    case "${t}" in
      --file=*) BUILD_FILE="${t#--file=}" ;;
      --*=*) ;;
      --* | -?) if [[ "${build_value_flags}" == *" ${t} "* ]]; then skip=1; prev="${t}"; fi ;;
      -* | \>* | 2\>* | \&* | \|*) ;;
      *) last="${t}" ;;
    esac
  done
  BUILD_CTX="${last}"
}

# ref_is_hub <verb> <ref token>: 0 (and sets FACT_WHY) when the ref can pull from
# Docker Hub.
ref_is_hub() {
  unquote "$2"
  if image_is_hub "${UNQ}"; then FACT_WHY="docker $1 ${EXP}"; return 0; fi
  return 1
}

# fact_is_hub <fact>: <fact> is one lexed docker command, words separated by
# octal 037 and normalised to "docker <sub> ..." (global flags removed). Returns
# 0 (and sets FACT_WHY) when the command can pull from Docker Hub; 1 when it is
# non-Hub or pulls nothing. A subcommand or argument it cannot read counts as Hub.
fact_is_hub() {
  local -a T=()
  local sub rest ref t skip=0 df ctx prevflag="" found=0 drv=""
  IFS=$'\037' read -ra T <<<"$1"
  sub="${T[1]:-}"
  case "${sub}" in
    run | create) rest=2 ;;
    container) case "${T[2]:-}" in run | create) rest=3 ;; *) return 1 ;; esac; sub=run ;;
    pull) rest=2 ;;
    image) case "${T[2]:-}" in pull) rest=3; sub=pull ;; build) rest=3; sub=build ;; *) return 1 ;; esac ;;
    build) rest=2 ;;
    buildx)
      case "${T[2]:-}" in
        build) rest=3; sub=build ;;
        bake) FACT_WHY="docker buildx bake"; return 0 ;;
        imagetools) case "${T[3]:-}" in create | inspect) rest=4; sub=imagetools ;; *) FACT_WHY="docker buildx imagetools ${T[3]:-} (not recognised)"; return 0 ;; esac ;;
        create | bootstrap)
          for t in "${T[@]:3}"; do
            case "${prevflag}" in --driver) drv="${t}" ;; esac
            case "${t}" in --driver=*) drv="${t#--driver=}" ;; esac
            prevflag="${t}"
          done
          case "${drv//[\"\']/}" in docker | remote) return 1 ;; esac
          FACT_WHY="docker buildx ${T[2]} bootstraps a moby/buildkit builder"; return 0 ;;
        inspect)
          for t in "${T[@]:3}"; do
            if [[ "${t}" == --bootstrap ]]; then FACT_WHY="docker buildx inspect --bootstrap pulls moby/buildkit"; return 0; fi
          done
          return 1 ;;
        ls | rm | prune | du | stop | use | version | history | debug | dial-stdio) return 1 ;;
        *) FACT_WHY="docker buildx ${T[2]:-} (subcommand not recognised)"; return 0 ;;
      esac ;;
    *) FACT_WHY="docker ${sub} (subcommand not recognised)"; return 0 ;;
  esac
  case "${sub}" in
    run | create)
      ref="$(run_image "${T[@]:${rest}}")" || { FACT_WHY="docker ${sub} with no parsable image"; return 0; }
      unquote "${ref}"
      if image_is_hub "${UNQ}"; then FACT_WHY="docker ${sub} ${EXP}"; return 0; fi
      return 1 ;;
    pull | imagetools)
      for t in "${T[@]:${rest}}"; do
        if [[ "${skip}" -eq 1 ]]; then
          skip=0
          case "${prevflag}" in -t | --tag) found=1; if ref_is_hub "${sub}" "${t}"; then return 0; fi ;; esac
          continue
        fi
        case "${t}" in
          -t | --tag | --platform | -f | --file | --builder | --progress | --annotation) skip=1; prevflag="${t}" ;;
          \>* | 2\>* | \&* | \|*) break ;;
          -*) ;;
          *) found=1; if ref_is_hub "${sub}" "${t}"; then return 0; fi ;;
        esac
      done
      [[ "${found}" -eq 1 ]] || { FACT_WHY="docker ${sub} with no image argument it can read"; return 0; }
      return 1 ;;
    build)
      build_context "${T[@]:${rest}}"
      unquote "${BUILD_FILE}"; df="${UNQ}"
      unquote "${BUILD_CTX}"; ctx="${UNQ}"
      expand "${df}" ""; df="${EXP}"
      expand "${ctx}" ""; ctx="${EXP}"
      if [[ -z "${df}" ]]; then
        [[ -n "${ctx}" && "${ctx}" != *@UNRESOLVED@* && "${ctx}" != - && "${ctx}" != *://* ]] || { FACT_WHY="docker build with an underivable context"; return 0; }
        df="${ctx}/Dockerfile"
      fi
      [[ "${df}" != *@UNRESOLVED@* ]] || { FACT_WHY="docker build with an underivable Dockerfile"; return 0; }
      df="${df#./}"
      if dockerfile_is_hub "${df}"; then FACT_WHY="docker build of ${df}, which has a Docker Hub FROM"; return 0; fi
      return 1 ;;
  esac
  return 1
}

# var_example <seq> <files...>: prints the first variable-as-command record (the
# run text first), for the failure message.
var_example() {
  local seq="$1" f cur line
  shift
  cur="RV_${seq}"
  line="$(printf '%s' "${!cur:-}" | head -n 1 | tr '\037' ' ')"
  if [[ -n "${line}" ]]; then printf 'run: %s' "${line}"; return 0; fi
  for f in "$@"; do
    mkkey "${f}"; cur="FV_${KEY}"
    line="$(printf '%s' "${!cur:-}" | head -n 1 | tr '\037' ' ')"
    if [[ -n "${line}" ]]; then printf '%s: %s' "${f}" "${line}"; return 0; fi
  done
  printf '(not recorded)'
}

# step_hub_why <run file> <run has other> <run has compose> <closure files>
# <env64> <run has var> <run has docker word>: sets HUBWHY to a reason when the
# step can pull from Docker Hub, "" otherwise. The has-flags are 1 when the run
# text itself matches. Variable
# tables load only when a docker command carries a $ reference. A docker word
# the lexer could not classify (RU_/FU_ records) counts as Docker Hub last.
step_hub_why() {
  local runf="$1" run_other="$2" cfiles="$4" env64="$5" run_var="${6:-0}" run_dock="${7:-0}" f line name need_compose="$3" need_vars=0 i cur seq="${1##*/}"
  local -a files=() fact_text=() fact_src=()
  HUBWHY=""
  while IFS= read -r f; do [[ -z "${f}" ]] || files+=("${f}"); done <<<"${cfiles}"
  if [[ "${run_other}" -eq 1 ]]; then
    cur="RO_${seq}"
    while IFS= read -r line; do
      [[ -n "${line}" ]] || continue
      fact_text+=("${line}"); fact_src+=("run")
      [[ "${line}" != *'$'* ]] || need_vars=1
    done <<<"${!cur:-}"
  fi
  for f in ${files[@]+"${files[@]}"}; do
    [[ "${compose_files}" != *$'\n'"${f}"$'\n'* ]] || need_compose=1
    [[ "${other_files}" == *$'\n'"${f}"$'\n'* ]] || continue
    mkkey "${f}"; cur="FO_${KEY}"
    while IFS= read -r line; do
      [[ -n "${line}" ]] || continue
      fact_text+=("${line}"); fact_src+=("${f}")
      [[ "${line}" != *'$'* ]] || need_vars=1
    done <<<"${!cur:-}"
  done
  clear_step_tables
  if [[ "${need_vars}" -eq 1 ]]; then
    if [[ -n "${env64}" ]]; then
      # The decoded text has no trailing newline: keep a last unterminated line.
      while IFS= read -r line || [[ -n "${line}" ]]; do
        [[ "${line}" == *=* ]] || continue
        printf -v "E_${line%%=*}" '%s' "${line#*=}"
      done < <(printf '%s' "${env64}" | b64d)
      while IFS= read -r line || [[ -n "${line}" ]]; do
        [[ "${line}" == *=* ]] || continue
        name="${line%%=*}"
        expand "${line#*=}" ""
        printf -v "E_${name}" '%s' "${EXP}"
      done < <(printf '%s' "${env64}" | b64d)
    fi
    # Assignments: reached scripts first, then the run text itself.
    for f in ${files[@]+"${files[@]}"}; do load_assigns "${root}/${f}"; done
    load_assigns "${runf}"
  fi
  for ((i = 0; i < ${#fact_text[@]}; i++)); do
    if fact_is_hub "${fact_text[$i]}"; then
      if [[ "${fact_src[$i]}" == run ]]; then HUBWHY="run: ${FACT_WHY}"; else HUBWHY="${fact_src[$i]}: ${FACT_WHY}"; fi
      return 0
    fi
  done
  # Compose: every compose file the reached text names must derive non-Hub.
  if [[ "${need_compose}" -eq 1 ]] && compose_step_is_hub "${runf}" "${cfiles}"; then
    HUBWHY="docker compose with compose files not provably non-Hub"
    return 0
  fi
  # A variable invoked as a command (V) next to a docker word (D) in the run text
  # or the scripts it reaches may hold docker (fail closed).
  has_vd "${cfiles}" "${run_dock}"
  if [[ ( "${run_var}" -eq 1 || "${HAS_V}" -eq 1 ) && "${HAS_D}" -eq 1 ]]; then
    HUBWHY="a variable is invoked as a command where docker is also named, so the gate cannot tell whether it runs docker: $(var_example "${seq}" "${files[@]+"${files[@]}"}")"
    return 0
  fi
  # A docker word the lexer could not classify counts as Docker Hub (fail closed).
  cur="RU_${seq}"
  if [[ -n "${!cur:-}" ]]; then
    HUBWHY="run: docker is mentioned where the gate cannot classify it: $(printf '%s' "${!cur}" | head -n 1 | tr '\037' ' ')"
    return 0
  fi
  for f in ${files[@]+"${files[@]}"}; do
    mkkey "${f}"; cur="FU_${KEY}"
    if [[ -n "${!cur:-}" ]]; then
      HUBWHY="${f}: docker is mentioned where the gate cannot classify it: $(printf '%s' "${!cur}" | head -n 1 | tr '\037' ' ')"
      return 0
    fi
  done
  return 0
}
