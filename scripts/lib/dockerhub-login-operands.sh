#!/usr/bin/env bash
#
# dockerhub-login-operands.sh - the -f operands and COMPOSE_FILE entries of docker
# compose invocations, for verify-dockerhub-login.sh (#7886). Sourced, never run,
# after dockerhub-login-compose.sh.
#
# lib/dockerhub-login-compose-flow.awk reads every -f operand and every
# COMPOSE_FILE entry of a compose invocation. Each must settle to literal compose
# files of the tree: by the literal itself, by every assignment (in the script and
# the files it sources), loop list, env value and default of the variable it
# names, or by the arguments of every call of the function whose parameter it is.
# An operand that does not (an unassigned or expression-valued variable, an
# array expansion, a file the tree lacks) leaves the invocation unresolved,
# whatever its other -f files hold: an override-only file is not a waiver for a
# base file the gate cannot read.
#
# "$ESHU_CI_COMPOSE_OVERLAY" as an operand is an unresolved variable like any
# other: it settles nothing. A call that applies the overlay is safe only through
# the CI compose wrapper (ci_wrapper_active in lib/dockerhub-login-ledger.sh),
# which the caller exempts before it reads operands.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

# compose_cache_key <file>: sets KEY for a per-file cache. A run-text file lives
# under ${tmp}, and the same path holds another text in the next workflow, so its
# key includes a checksum of the content.
compose_cache_key() {
  local sum=""
  if [[ "$1" == "${tmp}"/* ]]; then sum="$(cksum <"$1" 2>/dev/null | tr -s ' ' '_')"; fi
  mkkey "$1${sum:+:${sum}}"
}

# compose_flow <mode> <file>: runs lib/dockerhub-login-compose-flow.awk (modes ops
# and assign) and leaves its records in FLOW; cached per file. Not called in a
# subshell, so the cache survives.
compose_flow() {
  local mode="$1" f="$2" c
  compose_cache_key "${f}"; c="CF${mode}_${KEY}"
  if [[ -z "${!c+x}" ]]; then
    # ops records only come from text that names compose: skip the awk run otherwise.
    if [[ "${mode}" == ops || "${mode}" == wnames || "${mode}" == wsum ]] && ! rg -qi -e compose -- "${f}" 2>/dev/null; then
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
  compose_cache_key "$2"; fk="${KEY}"
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

# compose_cond_literals <${{ expression }}>: returns 0 and leaves the result
# literals of a conditional chain in COND_LITS, one per line. The expression must
# be "cond && 'lit' || cond && 'lit' || ... || 'lit'": cut at the top-level ||
# (outside quotes and parentheses), every part but the last ends in "&& 'lit'",
# and the last is one literal. Anything else (a bare value arm, "a || 'b'", no
# literal at the end) returns 1, so the operand is not settled.
compose_cond_literals() {
  local e="$1" i ch n q="" depth=0 cur="" part lit
  local -a parts=()
  COND_LITS=""
  e="${e#\$\{\{}"; e="${e%\}\}}"
  n=${#e}
  for ((i = 0; i < n; i++)); do
    ch="${e:i:1}"
    if [[ -n "${q}" ]]; then
      cur+="${ch}"; [[ "${ch}" != "${q}" ]] || q=""
      continue
    fi
    case "${ch}" in
      "'") q="'"; cur+="${ch}" ;;
      "(") depth=$((depth + 1)); cur+="${ch}" ;;
      ")") depth=$((depth - 1)); cur+="${ch}" ;;
      "|")
        if [[ "${depth}" -eq 0 && "${e:i+1:1}" == "|" ]]; then parts+=("${cur}"); cur=""; i=$((i + 1)); else cur+="${ch}"; fi ;;
      *) cur+="${ch}" ;;
    esac
  done
  [[ -z "${q}" && "${depth}" -eq 0 ]] || return 1
  parts+=("${cur}")
  [[ "${#parts[@]}" -ge 2 ]] || return 1
  for ((i = 0; i < ${#parts[@]}; i++)); do
    part="${parts[$i]}"
    if [[ "$i" -eq $((${#parts[@]} - 1)) ]]; then
      [[ "${part}" =~ ^[[:space:]]*\'([^\']*)\'[[:space:]]*$ ]] || return 1
    else
      [[ "${part}" =~ \&\&[[:space:]]*\'([^\']*)\'[[:space:]]*$ ]] || return 1
    fi
    lit="${BASH_REMATCH[1]}"
    COND_LITS="${COND_LITS}${lit}"$'\n'
  done
  return 0
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
  # ${{ c1 && 'a' || c2 && 'b' || 'c' }}: the value is one of the quoted literals,
  # and every one is settled. Any other shape of ${{ }} is not settled below.
  if [[ "${v}" == '${{'*'}}' ]] && compose_cond_literals "${v}"; then
    local lit
    while IFS= read -r lit; do
      compose_settle_value "${lit}" "${fn}" $((d + 1)) "$@" || return 1
    done <<<"${COND_LITS}"
    return 0
  fi
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

# compose_wrapper_seen <text file>...: 0 when a text holds a call of the CI compose
# wrapper that is not settled (CI image mode is off or the wrapper is not in the
# tree), or a command whose word ends in compose.sh but is not the wrapper call
# (a path held in a variable, another directory, a longer name). Such a command
# runs Compose at run time, so the step is read as a compose step and judged by
# the images its files name.
compose_wrapper_seen() {
  local f nw nrc nlk
  local active=1
  ci_wrapper_active || active=0
  for f in "$@"; do
    [[ -f "${f}" ]] || continue
    compose_flow wsum "${f}"
    IFS=$'\036' read -r nw nrc nlk <<<"${FLOW}"
    [[ "${nlk:-0}" -eq 0 ]] || return 0
    [[ "${active}" -eq 1 || "${nw:-0}" -eq 0 ]] || return 0
  done
  return 1
}

# compose_wrapper_seen_in <run text file> <newline-separated repo-relative files>:
# compose_wrapper_seen over the run text and the files.
compose_wrapper_seen_in() {
  local f
  local -a texts=("$1")
  while IFS= read -r f; do [[ -z "${f}" ]] || texts+=("${root}/${f}"); done <<<"$2"
  compose_wrapper_seen "${texts[@]}"
}

# compose_operand_check <operand> <function> <where> <text file>...: settles one operand. A
# settled file is checked like a named one (it also counts as a named file); an
# operand that does not settle is added to COMPOSE_HUB_FILES. Sets found and
# COMPOSE_HUB_FILES in compose_step_is_hub's scope.
compose_operand_check() {
  local op="$1" fn="$2" where="$3" cf q
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
