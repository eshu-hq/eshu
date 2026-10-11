#!/usr/bin/env bash
#
# dockerhub-login-dockerfile.sh - Dockerfile base images for
# verify-dockerhub-login.sh (#7886). Sourced, never run, after
# dockerhub-login-images.sh.
#
# A FROM can pull from Docker Hub by its literal ref or by a global ARG it names.
# docker build gives an ARG its value from --build-arg and otherwise from the
# default in the Dockerfile; the process environment reaches an ARG only through
# a --build-arg that names it. The caller states the build args of the build it
# is judging in DF_ARGS (newline-separated NAME=VALUE, values already expanded),
# empty when the build passes none. An ARG with neither a build arg nor a default
# is unresolved and counts as Docker Hub, like a default that is a Docker Hub
# ref: a fall-through to a Docker Hub default is a reach.
#
# The caller defines: root, expand, image_is_hub, DF_ARGS.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

DF_ARGS=""

# df_arg_value <name>: sets DF_VALUE and returns 0 when DF_ARGS names the arg.
df_arg_value() {
  local line
  DF_VALUE=""
  while IFS= read -r line; do
    [[ "${line%%=*}" == "$1" && "${line}" == *=* ]] || continue
    DF_VALUE="${line#*=}"
    return 0
  done <<<"${DF_ARGS}"
  return 1
}

# dockerfile_refs <repo-relative Dockerfile>: prints one line per external FROM
# ref, "F<TAB>ref", and one line per global ARG default before the first FROM,
# "A<TAB>name<TAB>default". Stage aliases and scratch are not external.
dockerfile_refs() {
  LC_ALL=C awk '
    function unq(v) { if (v ~ /^".*"$/ || v ~ /^'"'"'.*'"'"'$/) return substr(v, 2, length(v) - 2); return v }
    { line = $0; sub(/[ \t]+#.*$/, "", line) }
    /^[ \t]*#/ { next }
    { if (line ~ /\\[ \t]*$/) { sub(/\\[ \t]*$/, "", line); acc = acc line " "; next }
      line = acc line; acc = "" }
    {
      u = line; sub(/^[ \t]+/, "", u)
      if (toupper(substr(u, 1, 4)) == "ARG " && !seen) {
        rest = substr(u, 5); pos = index(rest, "=")
        if (pos > 1) { nm = substr(rest, 1, pos - 1); gsub(/[ \t]/, "", nm); printf "A\t%s\t%s\n", nm, unq(substr(rest, pos + 1)) }
        else { nm = rest; gsub(/[ \t]/, "", nm); printf "A\t%s\t\n", nm }
        next
      }
      if (toupper(substr(u, 1, 5)) == "FROM " || toupper(substr(u, 1, 5)) == "FROM\t") {
        seen = 1
        n = split(u, f, /[ \t]+/); ref = ""; al = ""
        for (i = 2; i <= n; i++) {
          if (f[i] ~ /^--/) continue
          if (ref == "") { ref = f[i]; continue }
          if (toupper(f[i]) == "AS" && i < n) { al = f[i + 1]; break }
        }
        if (ref != "" && ref != "scratch" && !(ref in aliases)) printf "F\t%s\n", ref
        if (al != "") aliases[al] = 1
      }
    }' "${root}/$1"
}

# dockerfile_is_hub <repo-relative Dockerfile>: 0 when any FROM can pull from
# Docker Hub or cannot be resolved (also when the file is unreadable); 1 when
# every external FROM resolves to another registry. The ARGs resolve from
# DF_ARGS, then the Dockerfile default, never the process environment.
dockerfile_is_hub() {
  local f="$1" kind a b ref val name rest n
  local -a names=() defaults=()
  [[ -f "${root}/${f}" ]] || return 0
  while IFS=$'\t' read -r kind a b; do
    case "${kind}" in
      A) names+=("${a}"); defaults+=("${b}") ;;
      F)
        ref="${a}"; n=0
        while [[ "${ref}" == *'$'* && "${n}" -lt 8 ]]; do
          n=$((n + 1))
          if [[ "${ref}" =~ \$\{([A-Za-z_][A-Za-z0-9_]*)(:?[-=])?([^}]*)\} ]]; then
            name="${BASH_REMATCH[1]}"; rest="${BASH_REMATCH[0]}"; val="@UNRESOLVED@"
            if df_arg_value "${name}"; then val="${DF_VALUE}"
            else
              local i
              for ((i = ${#names[@]} - 1; i >= 0; i--)); do
                if [[ "${names[$i]}" == "${name}" && -n "${defaults[$i]}" ]]; then val="${defaults[$i]}"; break; fi
              done
              if [[ "${val}" == "@UNRESOLVED@" && -n "${BASH_REMATCH[2]}" && -n "${BASH_REMATCH[3]}" ]]; then val="${BASH_REMATCH[3]}"; fi
            fi
            ref="${ref/"${rest}"/${val}}"
          elif [[ "${ref}" =~ \$([A-Za-z_][A-Za-z0-9_]*) ]]; then
            name="${BASH_REMATCH[1]}"; rest="${BASH_REMATCH[0]}"; val="@UNRESOLVED@"
            if df_arg_value "${name}"; then val="${DF_VALUE}"
            else
              local j
              for ((j = ${#names[@]} - 1; j >= 0; j--)); do
                if [[ "${names[$j]}" == "${name}" && -n "${defaults[$j]}" ]]; then val="${defaults[$j]}"; break; fi
              done
            fi
            ref="${ref/"${rest}"/${val}}"
          else
            break
          fi
        done
        if image_is_hub "${ref}"; then return 0; fi ;;
    esac
  done < <(dockerfile_refs "${f}")
  return 1
}
