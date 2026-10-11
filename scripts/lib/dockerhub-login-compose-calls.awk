# dockerhub-login-compose-calls.awk - compose call facts for verify-dockerhub-login.sh
# (#7886). It is read together with lib/dockerhub-login-compose-flow.awk (awk -f
# flow -f calls) and adds the "inv" mode: one record per compose call, in file
# order, so that the overlay state of an array or a variable is the state at the
# call (the last assignment before it wins, a reset clears it).
#
#   I<RS>function<RS>line<RS>sub<RS>status<RS>origin line<RS>origin function<RS>arrays
#   S<RS>name<RS>origin line<RS>origin function      (end of file: an array's state)
#
# status is one of:
#   A  a call that can pull and applies the overlay: an -f or --file operand names
#      "${ESHU_CI_COMPOSE_OVERLAY}" (or a variable assigned from it), or an array
#      expansion or compose command array holds such an operand at that point.
#      The origin names where the overlay entered the call: the prepare call must
#      have run before that line.
#   W  a call through scripts/ci/compose.sh, which prepares and appends it itself.
#   N  a call whose subcommand is a literal word that pulls no image (config,
#      down, logs, ps, exec, ...). It needs no overlay.
#   P  a call that can pull and has no overlay. Every other literal subcommand,
#      and a subcommand that is not a literal word, can pull.
#   X  as P, but it expands arrays this file never assigns; the arrays field lists
#      them (comma separated) and the caller looks at the files of the same shell.
#   U  a command whose command word is a variable and that passes the word
#      "compose": a compose call the reader cannot place. It counts as a call that
#      pulls and has no overlay.
# A compose call is any command with a word docker-compose, or docker followed by
# compose (past docker's global flags and their values), or a command whose first
# word expands a compose command array (NAME=(docker compose ...); "${NAME[@]}" up).
# Wrappers (timeout 300, nice -n 5, sudo -u root, retry 3, xargs) need no list: the
# whole command is searched. The overlay counts only as the value of -f or --file,
# never as --env-file. sub is 1 for a command in a subshell, a command
# substitution, a pipeline or a background job.

BEGIN {
  NOPULL = " config convert down logs ps exec ls version images top port events stop kill rm pause unpause start restart cp wait attach stats help "
  VALFLAGS = " -f --file -p --project-name --profile --env-file --project-directory --ansi --progress --parallel --context -c --config -H --host -l --log-level "
  DOCKVAL = " --context -c --config -H --host -l --log-level --tlscacert --tlscert --tlskey "
}

# ovl_origin returns the origin line when the word w, used as a -f operand, is the
# overlay or a variable assigned from it, and sets OVF to the function it came
# from. It returns 0 for any other word (a name that only starts like an alias,
# a variable reassigned to a file).
function ovl_origin(w,   q, nm) {
  q = unq(w)
  if (q ~ /^\$\{?ESHU_CI_COMPOSE_OVERLAY(\}|:[-=+?].*\})?$/) { OVF = CURFN; return NR }
  if (match(q, /^\$\{?[A-Za-z_][A-Za-z0-9_]*/)) {
    nm = substr(q, RSTART, RLENGTH); sub(/^\$\{?/, "", nm)
    if (q == "$" nm || q == "${" nm "}" || q ~ ("^\\$\\{" nm ":[-=+?].*\\}$")) {
      if (VARL[nm] > 0) { OVF = VARF[nm]; return VARL[nm] }
    }
  }
  return 0
}

# ovl_pick keeps the earliest origin of the overlay a call uses.
function ovl_pick(line, f) {
  if (line > 0 && (BEST == 0 || line < BEST)) { BEST = line; BESTF = f }
}

# inv_var records NAME=value: the variable holds the overlay from this line, or
# nothing after a later assignment.
function inv_var(nm, v, fn,   o) {
  CURFN = fn
  o = ovl_origin(v)
  VARL[nm] = o
  VARF[nm] = (o > 0) ? OVF : ""
}

# array_name returns NAME for the word "${NAME[@]}" (also [*], quoted or not).
function array_name(w,   q) {
  q = unq(w)
  if (q ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*\[[@*]\]\}$/) { sub(/^\$\{/, "", q); sub(/\[.*$/, "", q); return q }
  return ""
}

# is_compose_at returns the index of the compose word of the command T[a..b], or 0.
function is_compose_at(T, a, b,   i, j, u, base, nx) {
  for (i = a; i <= b; i++) {
    u = unq(T[i]); base = u; sub(/^.*\//, "", base)
    if (base == "docker-compose") return i
    if (base == "docker") {
      for (j = i + 1; j <= b; j++) {
        nx = unq(T[j])
        if (nx ~ /^-/) { if (nx !~ /=/ && index(DOCKVAL, " " nx " ") > 0) j++; continue }
        if (nx == "compose") return j
        break
      }
    }
  }
  return 0
}

# inv_array handles NAME=(words) and NAME+=(words) (T[a] is the opening paren).
function inv_array(nm, plus, T, a, b, fn,   i, w, prev, ov, isc, an, o) {
  CURFN = fn; BEST = 0; BESTF = ""; isc = 0
  for (i = a + 1; i <= b; i++) {
    w = unq(T[i])
    if (w == ")") break
    if (prev == "-f" || prev == "--file") { o = ovl_origin(T[i]); ovl_pick(o, OVF) }
    else if (w ~ /^(--file|-f)=/) { o = ovl_origin(substr(w, index(w, "=") + 1)); ovl_pick(o, OVF) }
    an = array_name(T[i])
    if (an != "" && ARRL[an] > 0) ovl_pick(ARRL[an], ARRF[an])
    prev = w
  }
  if (is_compose_at(T, a + 1, b) > 0) isc = 1
  ARRSEEN[nm] = 1
  if (plus) {
    if (ARRL[nm] > 0) ovl_pick(ARRL[nm], ARRF[nm])
    CMDARR[nm] = CMDARR[nm] || isc
  } else {
    CMDARR[nm] = isc
  }
  ARRL[nm] = BEST; ARRF[nm] = BESTF
}

# inv_call classifies one command and prints its I record when it is a compose call.
function inv_call(T, a, b, fn,   i, ci, an, status, sc, j, u, ext, prev, first, lead, k) {
  CURFN = fn; BEST = 0; BESTF = ""; ext = ""
  for (i = a; i <= b; i++) {
    if (unq(T[i]) ~ /ci\/compose\.sh$/) {
      print "I" SEP fn SEP NR SEP SUBC SEP "W" SEP 0 SEP "" SEP ""
      return
    }
  }
  first = unq(T[a])
  ci = is_compose_at(T, a, b)
  lead = array_name(T[a])
  if (ci == 0 && lead != "" && CMDARR[lead]) {
    ci = a
    if (ARRL[lead] > 0) ovl_pick(ARRL[lead], ARRF[lead])
  }
  if (ci == 0) {
    # a variable as the command word that passes "compose": a call the reader cannot place
    if (first ~ /^\$/ || first ~ /^\$\{/) {
      for (i = a + 1; i <= b; i++) if (unq(T[i]) == "compose") {
        print "I" SEP fn SEP NR SEP SUBC SEP "U" SEP 0 SEP "" SEP ""
        return
      }
    }
    return
  }
  # options before the subcommand; the first word that is not an option is the subcommand
  sc = ""; prev = ""
  for (j = ci + 1; j <= b; j++) {
    u = unq(T[j])
    # an array expansion or "$@" before the subcommand is an option list
    if (array_name(T[j]) != "" || u ~ /^\$(@|\{@\})$/) continue
    if (u ~ /^-/) {
      if (u ~ /^(--file|-f)=/) { ovl_pick(ovl_origin(substr(u, index(u, "=") + 1)), OVF); continue }
      if (index(VALFLAGS, " " u " ") > 0 && j < b) {
        if (u == "-f" || u == "--file") ovl_pick(ovl_origin(T[j + 1]), OVF)
        j++
      }
      continue
    }
    sc = u
    break
  }
  for (i = a; i <= b; i++) {
    an = array_name(T[i])
    if (an == "" || (i == a && ci == a)) continue
    if (ARRL[an] > 0) ovl_pick(ARRL[an], ARRF[an])
    else if (!ARRSEEN[an]) ext = ext (ext == "" ? "" : ",") an
  }
  if (sc != "" && index(NOPULL, " " sc " ") > 0) status = "N"
  else if (BEST > 0) status = "A"
  else if (ext != "") status = "X"
  else status = "P"
  print "I" SEP fn SEP NR SEP SUBC SEP status SEP BEST SEP BESTF SEP ext
}

END {
  if (mode == "inv") for (k in ARRSEEN) print "S" SEP k SEP ARRL[k] SEP ARRF[k]
}
