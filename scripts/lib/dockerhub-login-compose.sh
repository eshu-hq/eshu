#!/usr/bin/env bash
#
# dockerhub-login-compose.sh - docker compose file resolution for
# verify-dockerhub-login.sh (#7886). Sourced, never run, after
# dockerhub-login-images.sh.
#
# A compose file reaches Docker Hub when any service in it, or in a file it
# pulls in, can: by an image: ref whose host is Docker Hub, or by a build: whose
# Dockerfile has a Docker Hub FROM. A service that extends another service
# (extends: {file, service} or extends: <service>) takes the image or build the
# base defines unless it overrides it, so the base is followed, file and
# service. A top-level include: (a path string, a list, or a map with path:) is
# followed file by file. Anything that cannot be resolved counts as Docker Hub:
# a missing file or service, a parse error, a build context that is a URL or
# leaves the repo, an inline Dockerfile, a cycle or a chain deeper than
# compose_max_depth.
#
# A compose invocation is read by its -f operands too (lib/dockerhub-login-
# compose-flow.awk): every operand, and every entry of COMPOSE_FILE, must settle to
# literal compose files, by the literal itself or by every script assignment,
# loop list, env value and default of the variable it names. An operand that
# does not (an unassigned or expression-valued variable, an array expansion, a
# file the tree lacks) leaves the invocation unresolved, whatever its other -f
# files hold: an override-only file is not a waiver for a base file the gate
# cannot read.
#
# The caller defines root, tmp, mkkey, expand, image_is_hub, dockerfile_is_hub,
# compose_file_re, flow_awk and the E_/J_/W_/V_ tables.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

compose_max_depth=6
# One row per include (I|path) and per service
# (S|name|image|context|dockerfile|extends file|extends service|inline|has build).
# A build map without a context leaves the context empty, so an extending
# service inherits the base's. A non-empty service map with no image, build or
# extends is an override of a service another -f file defines (environment,
# command, ports): it names no image of its own. A non-map service and an empty
# map stay unresolved.
compose_rows_expr='explode(.) as $r
| ( (("S|@UNRESOLVED@|@UNRESOLVED@||||||" | select(($r | tag) != "!!map" or (($r | has("services")) and ($r.services | tag) != "!!map")))), ($r.include
    | ((select(tag == "!!null") | []),
       (select(tag == "!!str" or tag == "!!map") | [.]),
       select(tag == "!!seq"),
       (select(tag != "!!null" and tag != "!!str" and tag != "!!map" and tag != "!!seq") | ["@UNRESOLVED@"]))
    | .[]
    | ((select(tag == "!!str") | [.]),
       (select(tag == "!!map") | .path
         | ((select(tag == "!!str") | [.]), select(tag == "!!seq"),
            (select(tag != "!!str" and tag != "!!seq") | ["@UNRESOLVED@"]))),
       (select(tag != "!!str" and tag != "!!map") | ["@UNRESOLVED@"]))
    | .[] | "I|" + .),
    (($r.services // {}) | to_entries | .[] | .key as $n | .value as $v
      | "S|" + $n + "|" + (("@UNRESOLVED@" | select(($v | tag) != "!!map" or ($v | length) == 0)) // ($v.image | select(tag == "!!str" and . != "")) // ("@UNRESOLVED@" | select($v | has("image"))) // "") + "|"
        + (($v.build | select(tag == "!!str")) // ($v.build | select(tag == "!!map") | .context) // "") + "|"
        + (($v.build | select(tag == "!!map") | .dockerfile) // "") + "|"
        + (($v.extends | select(tag == "!!map") | .file) // "") + "|"
        + (($v.extends | select(tag == "!!str" and . != "")) // ($v.extends | select(tag == "!!map") | .service | select(. != null and . != "")) // ("@UNRESOLVED@" | select($v | has("extends"))) // "") + "|"
        + (("inline" | select((($v.build | tag) == "!!map" and $v.build.dockerfile_inline != null) or (($v | has("build")) and ($v.build | tag) != "!!str" and ($v.build | tag) != "!!map"))) // "") + "|"
        + (("build" | select($v | has("build"))) // "")) )'

# dir_join <dir> <relative path>: sets JOIN to the normalised repo-relative path
# ("." for the root) or "" when the path is absolute or leaves the repo.
dir_join() {
  local seg IFS=/
  local -a parts=()
  JOIN=""
  case "$2" in /*) return 0 ;; esac
  for seg in $1/$2; do
    case "${seg}" in
      "" | .) ;;
      ..) [[ "${#parts[@]}" -gt 0 ]] || return 0; unset "parts[$((${#parts[@]} - 1))]"; parts=("${parts[@]+"${parts[@]}"}") ;;
      *) parts+=("${seg}") ;;
    esac
  done
  if [[ "${#parts[@]}" -eq 0 ]]; then JOIN="."; else JOIN="${parts[*]}"; fi
}

# compose_rows <repo-relative file>: sets ROWS (cached per file); the text
# @PARSE_ERROR@ when yq cannot read it.
compose_rows() {
  local c out
  mkkey "$1"; c="CR_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    out="$(yq e "${compose_rows_expr}" "${root}/$1" 2>/dev/null)" || out="@PARSE_ERROR@"
    printf -v "${c}" '%s' "${out}"
  fi
  ROWS="${!c}"
}

# svc_effective <file> <service> <depth>: resolves a service through extends.
# Sets EFF_FAIL (1 when it cannot be resolved), EFF_IMG, EFF_BUILD (1 when the
# service builds), EFF_CTX (repo-relative build context), EFF_DF (Dockerfile
# name inside the context) and EFF_INLINE (1 for an inline Dockerfile).
svc_effective() {
  local f="$1" s="$2" d="$3" dir found=0 bf
  local r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build
  local bimg="" bbuild=0 bctx="" bdf="" binl=0
  EFF_FAIL=1; EFF_IMG=""; EFF_BUILD=0; EFF_CTX=""; EFF_DF=""; EFF_INLINE=0
  [[ "${d}" -le "${compose_max_depth}" ]] || return 0
  compose_rows "${f}"
  [[ "${ROWS}" != "@PARSE_ERROR@" ]] || return 0
  if [[ "${f}" == */* ]]; then dir="${f%/*}"; else dir="."; fi
  while IFS='|' read -r r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build; do
    [[ "${r_kind}" == S && "${r_name}" == "${s}" ]] || continue
    found=1; break
  done <<<"${ROWS}"
  [[ "${found}" -eq 1 ]] || return 0
  if [[ -n "${r_es}" ]]; then
    if [[ -n "${r_ef}" ]]; then
      expand "${r_ef}" ""
      [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
      dir_join "${dir}" "${EXP}"; bf="${JOIN}"
    else
      bf="${f}"
    fi
    [[ -n "${bf}" && -f "${root}/${bf}" ]] || return 0
    svc_effective "${bf}" "${r_es}" $((d + 1))
    [[ "${EFF_FAIL}" -eq 0 ]] || return 0
    bimg="${EFF_IMG}"; bbuild="${EFF_BUILD}"; bctx="${EFF_CTX}"; bdf="${EFF_DF}"; binl="${EFF_INLINE}"
  fi
  EFF_FAIL=1
  EFF_IMG="${r_img:-${bimg}}"
  EFF_INLINE="${binl}"
  if [[ -n "${r_build}" ]]; then
    EFF_BUILD=1
    if [[ -n "${r_ctx}" ]]; then
      expand "${r_ctx}" ""
      case "${EXP}" in *@UNRESOLVED@* | *'$'* | *://* | git@* | github.com/*) return 0 ;; esac
      dir_join "${dir}" "${EXP}"; EFF_CTX="${JOIN}"
    elif [[ "${bbuild}" -eq 1 ]]; then
      EFF_CTX="${bctx}"
    else
      dir_join "${dir}" "."; EFF_CTX="${JOIN}"
    fi
    [[ -n "${EFF_CTX}" ]] || return 0
    EFF_DF="${r_df:-${bdf:-Dockerfile}}"
    [[ -z "${r_inl}" ]] || EFF_INLINE=1
  else
    EFF_BUILD="${bbuild}"; EFF_CTX="${bctx}"; EFF_DF="${bdf}"
  fi
  EFF_FAIL=0
}

# compose_is_hub <repo-relative compose file> [<depth>]: returns 0 when any
# service, here or in an included file, can pull from Docker Hub or cannot be
# resolved; 1 when every service is provably non-Hub.
compose_is_hub() {
  local f="$1" d="${2:-0}" dir rows
  local r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build
  [[ "${d}" -le "${compose_max_depth}" && -f "${root}/${f}" ]] || return 0
  compose_rows "${f}"
  rows="${ROWS}"
  [[ "${rows}" != "@PARSE_ERROR@" ]] || return 0
  if [[ "${f}" == */* ]]; then dir="${f%/*}"; else dir="."; fi
  while IFS='|' read -r r_kind r_name r_img r_ctx r_df r_ef r_es r_inl r_build; do
    case "${r_kind}" in
      I)
        expand "${r_name}" ""
        [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
        dir_join "${dir}" "${EXP}"
        [[ -n "${JOIN}" ]] || return 0
        if compose_is_hub "${JOIN}" $((d + 1)); then return 0; fi ;;
      S)
        svc_effective "${f}" "${r_name}" 0
        [[ "${EFF_FAIL}" -eq 0 ]] || return 0
        if [[ -n "${EFF_IMG}" ]] && image_is_hub "${EFF_IMG}"; then return 0; fi
        if [[ "${EFF_BUILD}" -eq 1 ]]; then
          [[ "${EFF_INLINE}" -eq 0 ]] || return 0
          expand "${EFF_DF}" ""
          [[ "${EXP}" != *@UNRESOLVED@* && "${EXP}" != *'$'* ]] || return 0
          dir_join "${EFF_CTX}" "${EXP}"
          [[ -n "${JOIN}" ]] || return 0
          if dockerfile_is_hub "${JOIN}"; then return 0; fi
        fi ;;
    esac
  done <<<"${rows}"
  return 1
}

# compose_flow <mode> <file>: runs lib/dockerhub-login-compose-flow.awk (modes ops
# and assign) and leaves its records in FLOW; cached per file. Not called in a
# subshell, so the cache survives.
compose_flow() {
  local mode="$1" f="$2" c
  mkkey "${f}"; c="CF${mode}_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    # ops records only come from text that names compose: skip the awk run otherwise.
    if [[ "${mode}" == ops ]] && ! rg -qi -e compose -- "${f}" 2>/dev/null; then
      printf -v "${c}" '%s' ""
    else
      printf -v "${c}" '%s' "$(LC_ALL=C awk -v mode="${mode}" -f "${flow_awk}" "${f}" 2>/dev/null || true)"
    fi
  fi
  FLOW="${!c}"
}

# compose_calls_of <function> <file>: leaves in FLOW one CALLER<RS>ARGS record per
# call of the function in the file. The file is read once (mode calls) and split
# into one variable per callee, so a lookup is a variable read.
compose_calls_of() {
  local fnk fk callee caller args v
  mkkey "$2"; fk="${KEY}"
  v="CLOK_${fk}"
  if [[ -z "${!v+x}" ]]; then
    printf -v "${v}" '%s' 1
    while IFS=$'\036' read -r callee caller args; do
      [[ -n "${callee}" ]] || continue
      mkkey "${callee}"; v="CS_${fk}_${KEY}"
      printf -v "${v}" '%s%s\n' "${!v:-}" "${caller}"$'\036'"${args}"
    done < <(LC_ALL=C awk -v mode=calls -f "${flow_awk}" "$2" 2>/dev/null || true)
  fi
  mkkey "$1"; v="CS_${fk}_${KEY}"
  FLOW="${!v:-}"
}

# compose_settle_value <raw value> <function holding it> <depth> <text file>...
# settles a -f operand, an assigned value or a call argument to literal compose
# files: appends each repo-relative file to OPFILES and returns 0, or returns 1
# when any part cannot be settled (an expression, command substitution, read, a
# glob, an array expansion, a parameter transform, an unassigned variable, a
# script argument, a chain deeper than 6, or a name that is no file of the tree).
# A variable is the union of its assignments in every text, its for-in words and
# its env value (step over job over workflow); a positional parameter takes the
# arguments of every call of the function that holds it, settled in the caller.
# A literal may sit behind a variable directory prefix: the file is found by its
# name, as the name scan does.
compose_settle_value() {
  local v="$1" fn="$2" d="$3" seg name def="" op="" k key f line cfn args a n b val
  shift 3
  [[ "${d}" -le 6 ]] || return 1
  v="${v#\"}"; v="${v%\"}"; v="${v#\'}"; v="${v%\'}"
  [[ -n "${v}" ]] || return 0
  case "${v}" in *'`'* | *'$('* | *'${{'* | *'*'* | *'?'* | *' '* | '@READ@') return 1 ;; esac
  seg="${v##*/}"
  if [[ "${seg}" != *'$'* ]]; then
    b="${v#./}"; b="${b#/}"
    if [[ -f "${root}/${b}" ]]; then OPFILES="${OPFILES}${b}"$'\n'; return 0; fi
    if [[ -n "${seg}" && -f "${root}/${seg}" ]]; then OPFILES="${OPFILES}${seg}"$'\n'; return 0; fi
    return 1
  fi
  if [[ "${seg}" =~ ^\$\{([0-9]+)(:?[-=?+])([^}]*)\}$ ]]; then
    k="${BASH_REMATCH[1]}"; def="${BASH_REMATCH[3]}"
  elif [[ "${seg}" =~ ^\$\{([0-9]+)\}$ || "${seg}" =~ ^\$([0-9])$ ]]; then
    k="${BASH_REMATCH[1]}"
  elif [[ "${seg}" =~ ^\$\{([A-Za-z_][A-Za-z0-9_]*)(:?[-=?+])([^}]*)\}$ ]]; then
    name="${BASH_REMATCH[1]}"; def="${BASH_REMATCH[3]}"
  elif [[ "${seg}" =~ ^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$ || "${seg}" =~ ^\$([A-Za-z_][A-Za-z0-9_]*)$ ]]; then
    name="${BASH_REMATCH[1]}"
  else
    return 1
  fi
  if [[ -n "${def}" && "${def}" != *'$'* ]]; then compose_settle_value "${def}" "${fn}" $((d + 1)) "$@" || return 1; fi
  if [[ -n "${k:-}" ]]; then
    [[ -n "${fn}" ]] || return 1
    key="call:${fn}:${k}"
    if [[ "${COMPOSE_SEEN}" == *"|${key}|"* ]]; then COMPOSE_CUT=1; return 0; fi
    COMPOSE_SEEN="${COMPOSE_SEEN}|${key}|"
    for f in "$@"; do
      compose_calls_of "${fn}" "${f}"
      while IFS=$'\036' read -r cfn args; do
        [[ -n "${cfn}${args}" ]] || continue
        local -a parts=()
        IFS=$'\037' read -r -a parts <<<"${args}"
        [[ "${k}" -le "${#parts[@]}" ]] || continue
        a="${parts[$((k - 1))]}"
        case "${a}" in '"$@"' | '$@' | '"$*"' | '$*' | '"${@}"') return 1 ;; esac
        compose_settle_value "${a}" "${cfn}" $((d + 1)) "$@" || return 1
      done <<<"${FLOW}"
    done
    return 0
  fi
  key="var:${name}"
  if [[ "${COMPOSE_SEEN}" == *"|${key}|"* ]]; then COMPOSE_CUT=1; return 0; fi
  COMPOSE_SEEN="${COMPOSE_SEEN}|${key}|"
  n=0
  for f in "$@"; do
    compose_flow assign "${f}"
    while IFS=$'\036' read -r cfn b val; do
      [[ "${b}" == "${name}" ]] || continue
      n=$((n + 1))
      compose_settle_value "${val}" "${cfn}" $((d + 1)) "$@" || return 1
    done <<<"${FLOW}"
  done
  proc_env "${name}"
  if [[ -n "${LOOK}" ]]; then n=$((n + 1)); compose_settle_value "${LOOK}" "" $((d + 1)) "$@" || return 1; fi
  [[ "${n}" -gt 0 || -n "${def}" ]] || return 1
  return 0
}

# compose_operand_check <operand> <function> <where> <text file>...: settles one operand. A
# settled file is checked like a named one (it also counts as a named file); an
# operand that does not settle is added to COMPOSE_HUB_FILES. Sets found and
# COMPOSE_HUB_FILES in compose_step_is_hub's scope.
compose_operand_check() {
  local op="$1" fn="$2" where="$3" cf
  shift 3
  OPFILES=""; COMPOSE_SEEN="|"
  # A cycle through call arguments contributes nothing, so an operand whose walk
  # was cut by one must also have reached a literal file: a name that only passes
  # itself through a function parameter has no source. A function with no call in
  # the reached text has no walk to cut and settles vacuously.
  COMPOSE_CUT=0
  if ! compose_settle_value "${op}" "${fn}" 0 "$@" || [[ "${COMPOSE_CUT}" -eq 1 && -z "${OPFILES//[$'\n']/}" ]]; then
    COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES}${COMPOSE_HUB_FILES:+, }${op} (${where}, not settled to a literal compose file)"
    return 0
  fi
  while IFS= read -r cf; do
    [[ -n "${cf}" ]] || continue
    found=1
    if compose_is_hub "${cf}"; then
      [[ "${COMPOSE_HUB_FILES}" == *"${cf} ("* ]] || COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES}${COMPOSE_HUB_FILES:+, }${cf} (${where})"
    fi
  done <<<"${OPFILES}"
}

# compose_scopes <text file>...: groups the reached texts by shell. A file that
# no other text sources is an entry (a script run as its own process, or the run
# text); its group is itself plus every file it sources (source or ., resolved
# like a script reference), transitively. A script that only runs another script
# shares no shell with it, so a variable or function of one is not the other's.
# Leaves SC_<key> per file: the newline-delimited union of the groups holding it.
compose_scopes() {
  local f g s m t rel key dir tok k v i seen sourced="|"
  local -a all=("$@") ents=() q=()
  for f in "${all[@]}"; do
    mkkey "${f}"; k="${KEY}"
    if [[ "${f}" == "${root}"/* ]]; then
      rel="${f#"${root}"/}"; dir="${rel%/*}"; [[ "${rel}" == */* ]] || dir="."
    else
      dir="."
    fi
    compose_flow sources "${f}"
    v=""
    while IFS=$'\036' read -r key tok; do
      [[ -n "${tok}" ]] || continue
      tok="${tok//[\"\']/}"
      tok="${tok#\$\{*\}/}"; tok="${tok#\$[A-Za-z_]*/}"; tok="${tok#\$(*)/}"
      [[ "${tok}" == *.sh || "${tok}" == *.bash ]] || continue
      resolve_token "${tok}" "${dir}"
      [[ -n "${RESOLVED}" ]] || continue
      s="${root}/${RESOLVED}"
      for g in "${all[@]}"; do
        if [[ "${g}" == "${s}" ]]; then v="${v}${s}"$'\n'; sourced="${sourced}${s}|"; fi
      done
    done <<<"${FLOW}"
    printf -v "SD_${k}" '%s' "${v}"
    printf -v "SC_${k}" '%s' $'\n'
  done
  for f in "${all[@]}"; do [[ "${sourced}" == *"|${f}|"* ]] || ents+=("${f}"); done
  [[ "${#ents[@]}" -gt 0 ]] || ents=("${all[@]}")
  for g in "${ents[@]}"; do
    q=("${g}"); seen="|${g}|"; i=0
    while [[ "${i}" -lt "${#q[@]}" ]]; do
      mkkey "${q[$i]}"; v="SD_${KEY}"; i=$((i + 1))
      while IFS= read -r t; do
        [[ -n "${t}" && "${seen}" != *"|${t}|"* ]] || continue
        seen="${seen}${t}|"; q+=("${t}")
      done <<<"${!v:-}"
    done
    for m in "${q[@]}"; do
      mkkey "${m}"; v="SC_${KEY}"
      for t in "${q[@]}"; do
        [[ "${!v}" == *$'\n'"${t}"$'\n'* ]] || printf -v "${v}" '%s%s\n' "${!v}" "${t}"
      done
    done
  done
}

# compose_scope_of <text file>: sets SCOPE to the files that share a shell with it.
compose_scope_of() {
  local v t
  SCOPE=()
  mkkey "$1"; v="SC_${KEY}"
  while IFS= read -r t; do [[ -z "${t}" ]] || SCOPE+=("${t}"); done <<<"${!v:-}"
  [[ "${#SCOPE[@]}" -gt 0 ]] || SCOPE=("$1")
}

# compose_file_entries_check <text file>: COMPOSE_FILE names the files of an
# invocation that has no -f. Every entry of every value it can hold (assignments
# in the texts, then the process env) must settle like a -f operand.
compose_file_entries_check() {
  local f cfn nm val e
  local -a entries=()
  compose_scope_of "$1"
  for f in "${SCOPE[@]}"; do
    compose_flow assign "${f}"
    while IFS=$'\036' read -r cfn nm val; do
      [[ "${nm}" == COMPOSE_FILE ]] || continue
      entries+=("${cfn}"$'\036'"${val}")
    done <<<"${FLOW}"
  done
  proc_env COMPOSE_FILE
  [[ -z "${LOOK}" ]] || entries+=($'\036'"${LOOK}")
  compose_scope_of "$1"
  for e in ${entries[@]+"${entries[@]}"}; do
    cfn="${e%%$'\036'*}"; val="${e#*$'\036'}"
    val="${val#\"}"; val="${val%\"}"
    val="${val//:-/@DD@}"; val="${val//:=/@DE@}"; val="${val//:+/@DP@}"; val="${val//:\?/@DQ@}"; val="${val//:/$'\n'}"
    val="${val//@DD@/:-}"; val="${val//@DE@/:=}"; val="${val//@DP@/:+}"; val="${val//@DQ@/:?}"
    while IFS= read -r nm; do
      [[ -n "${nm}" ]] || continue
      compose_operand_check "${nm}" "${cfn}" "COMPOSE_FILE entry" "${SCOPE[@]}"
    done <<<"${val}"
  done
}

# compose_step_is_hub <run file> <closure files>: the step reaches docker
# compose. Exempt only when it names at least one compose file and every named
# file derives non-Hub; a step that names none cannot be derived. Names in
# full-line comments are not commands and do not count. A name in a default
# (${X:-docker-compose.yaml}) is read without the :- operator dash. Sets
# COMPOSE_HUB_FILES to the named files that are not provably non-Hub.
compose_step_is_hub() {
  local tok cf found=0 f rel
  local -a texts=("$1")
  COMPOSE_HUB_FILES=""
  while IFS= read -r f; do [[ -z "${f}" ]] || texts+=("${root}/${f}"); done <<<"$2"
  for f in "${texts[@]}"; do
    [[ -f "${f}" ]] || continue
    rel="${f#"${root}"/}"
    [[ "${f}" != "$1" ]] || rel="the run text"
    while IFS= read -r tok; do
      while [[ "${tok}" == -* ]]; do tok="${tok#-}"; done
      tok="${tok#./}"; tok="${tok#/}"; cf=""
      if [[ -f "${root}/${tok}" ]]; then cf="${tok}"; elif [[ -f "${root}/${tok##*/}" ]]; then cf="${tok##*/}"; fi
      [[ -n "${cf}" ]] || continue
      found=1
      if compose_is_hub "${cf}"; then
        [[ "${COMPOSE_HUB_FILES}" == *"${cf} (named in "* ]] || COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES}${COMPOSE_HUB_FILES:+, }${cf} (named in ${rel##*/})"
      fi
    done < <({ rg -v -N -e '^[[:space:]]*#' -- "${f}" | rg -o -N --no-filename -e "${compose_file_re}" || true; } | sort -u)
  done
  # Every -f operand and COMPOSE_FILE entry must settle to checked files.
  local -a existing=()
  for f in "${texts[@]}"; do [[ -f "${f}" ]] && existing+=("${f}"); done
  local op ofn
  compose_scopes ${existing[@]+"${existing[@]}"}
  for f in ${existing[@]+"${existing[@]}"}; do
    rel="${f#"${root}"/}"
    [[ "${f}" != "$1" ]] || rel="the run text"
    compose_flow ops "${f}"
    compose_scope_of "${f}"
    while IFS=$'\036' read -r ofn op; do
      [[ -n "${op}" ]] || continue
      compose_operand_check "${op}" "${ofn}" "-f operand in ${rel##*/}" "${SCOPE[@]}"
      compose_scope_of "${f}"
    done <<<"${FLOW}"
    rg -qi -e compose -- "${f}" 2>/dev/null && compose_file_entries_check "${f}"
  done
  if [[ "${found}" -ne 1 ]]; then
    COMPOSE_HUB_FILES="(no compose file named; docker compose runs in:"
    for f in "${texts[@]:1}"; do
      [[ "${compose_files}" != *$'\n'"${f#"${root}"/}"$'\n'* ]] || COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES} ${f#"${root}"/}"
    done
    [[ "${runs_compose}" != *$'\n'"${1##*/}"$'\n'* ]] || COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES} the run text"
    COMPOSE_HUB_FILES="${COMPOSE_HUB_FILES})"
    return 0
  fi
  [[ -z "${COMPOSE_HUB_FILES}" ]] || return 0
  return 1
}
