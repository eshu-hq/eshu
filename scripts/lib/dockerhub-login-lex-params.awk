# dockerhub-login-lex-params.awk - positional parameters, function scopes and
# loop or read assignments for verify-dockerhub-login.sh (#7886). It is loaded
# next to dockerhub-login-lex.awk (awk -f lex.awk -f lex-params.awk) and calls
# its functions; the lexer calls the note_* hooks below.
#
# Extra records (US is octal 037):
#   X <TAB> NAME             NAME is assigned a value the scanner cannot resolve
#                            ($(...), a backquote, ${!x}, read) in a file that
#                            itself carries a docker command word.
#   F <TAB> NAME             the file defines function NAME.
#   P <TAB> FN <TAB> K       inside function FN ("-" for the top level of the
#                            file) a variable assigned from positional parameter
#                            K ("A" for any: "$@", "$*", or after a shift) is
#                            invoked as a command, or "$K" itself is.
#   B <TAB> CMD <TAB> K <TAB> WORDS
#                            a bare docker word is argument K of command CMD (a
#                            function name or a script path). The caller decides
#                            from F and P whether CMD invokes that argument.
# A positional assignment (fail_fn="$1") is not unresolvable: its flow is the
# caller's argument, which the B records cover.

function params_reset() {
  split("", PARAM); split("", XSET); split("", FNDEF); split("", PINV); split("", SEEN)
  split("", BCMD); split("", BPOS); split("", BTXT)
  BN = 0; SHIFTED = 0; DEPTH = 0; FNCUR = ""; FNPEND = ""; FNBASE = 0
}

# positional_index returns the parameter a value reads ("1", "A" for $@ and $*),
# or "" when the value is not a bare positional expansion.
function positional_index(v,   k) {
  if (v ~ /^\$\{?[@*]\}?$/ || v ~ /^\$\{[@*]\}$/) return "A"
  if (v ~ /^\$\{?[1-9][0-9]*(:?[-=+?][^}]*)?\}?$/) {
    k = v
    sub(/^\$\{?/, "", k); sub(/[^0-9].*$/, "", k)
    return SHIFTED ? "A" : k
  }
  return ""
}

# note_value records NAME=val for the unresolvable (XSET) and positional (PARAM)
# classes; record_assign calls it before its own classification.
function note_value(name, val,   k) {
  SEEN[name] = 1
  k = positional_index(val)
  if (k != "") { PARAM[FNCUR SUBSEP name] = k; return 1 }
  if (index(val, "`") > 0 || index(val, "$(") > 0 || index(val, "${!") > 0) XSET[name] = 1
  return 0
}

# note_invoke records that positional parameter k is invoked as a command in the
# current function.
function note_invoke(k) { PINV[(FNCUR == "" ? "-" : FNCUR) SUBSEP k] = 1 }

# note_group tracks { and } so a function body has a start and an end.
function note_group(t) {
  if (t == "{") {
    DEPTH++
    if (FNPEND != "") { FNCUR = FNPEND; FNBASE = DEPTH; FNPEND = "" }
  } else if (t == "}") {
    if (FNCUR != "" && DEPTH == FNBASE) FNCUR = ""
    DEPTH--
  }
}

# note_function_word handles "function NAME [{]" and returns 1 when it consumed
# the command.
function note_function_word(w, s, n,   nm, k) {
  if (unq(w[s]) != "function" || s + 1 > n) return 0
  nm = unq(w[s + 1]); sub(/\(\)$/, "", nm)
  FNDEF[nm] = 1
  FNPEND = nm
  for (k = s + 2; k <= n; k++) if (unq(w[k]) == "{") { note_group("{"); break }
  return 1
}

# note_header handles NAME ( ) at the "(" of a function definition header.
function note_header(w, n,   q) {
  if (n != 1 || w[1] !~ /^[A-Za-z_][A-Za-z0-9_.:-]*$/) return
  q = P + 1
  while (q <= N && (A[q] == " " || A[q] == "\t")) q++
  if (A[q] == ")") { FNDEF[w[1]] = 1; FNPEND = w[1] }
}

# note_for records the loop variable of "for NAME in WORDS" as an assignment.
function note_for(w, i, n,   v, k, nm, t) {
  nm = unq(w[i + 1])
  if (nm !~ /^[A-Za-z_][A-Za-z0-9_]*$/) return
  v = ""
  for (k = i + 3; k <= n; k++) { t = unq(w[k]); v = v (v == "" ? "" : " ") t }
  if (v ~ /(^| )\$\{?[@*]\}?( |$)/) { SEEN[nm] = 1; PARAM[FNCUR SUBSEP nm] = "A"; return }
  record_assign(nm, v, 0)
}

# note_read records the variables a read builtin sets as unresolvable.
function note_read(w, i, n,   k, t, skip) {
  skip = 0
  for (k = i + 1; k <= n; k++) {
    t = unq(w[k])
    if (skip) { skip = 0; continue }
    if (t ~ /^-[pdnNtui]$/) { skip = 1; continue }
    if (t ~ /^-/) continue
    if (t ~ /^[A-Za-z_][A-Za-z0-9_]*$/) { XSET[t] = 1; SEEN[t] = 1 }
  }
}

# note_bare handles docker among the arguments of command w[i]: the words from
# docker on are the docker command the caller runs, and a bare trailing docker
# is argument (k - base) of the command, left to the caller's F and P records.
function note_bare(w, i, n, base,   k, t, d, m, j) {
  for (k = i + 1; k <= n; k++) {
    t = unq(w[k])
    if (t != "docker" && t != "docker-compose") continue
    if (k < n) {
      d[1] = t; m = 1
      for (j = k + 1; j <= n; j++) d[++m] = w[j]
      docker_cmd(d, m)
      return
    }
    BN++; BCMD[BN] = unq(w[base]); BPOS[BN] = k - base; BTXT[BN] = join_words(w, 1, n)
    return
  }
}

# params_emit prints the X, F, P and B records of the finished file.
function params_emit(   k, p, a) {
  if (FILE_D) for (k in XSET) print fname "\tX\t" k
  for (k in SEEN) print fname "\tS\t" k
  for (k in FNDEF) print fname "\tF\t" k
  for (k in PINV) { split(k, p, SUBSEP); print fname "\tP\t" p[1] "\t" p[2] }
  for (k = 1; k <= BN; k++) print fname "\tB\t" BCMD[k] "\t" BPOS[k] "\t" BTXT[k]
}
