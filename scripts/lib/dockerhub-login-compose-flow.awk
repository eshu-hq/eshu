# dockerhub-login-compose-flow.awk - reads shell text for the compose file flow of
# verify-dockerhub-login.sh (#7886). It is a bounded reader, not a shell: quotes,
# $(...) and backquotes keep a word whole, backslash continuations join, heredoc
# bodies and full-line comments are skipped, and a quoted string is data unless
# it follows -c or eval. One mode per run, chosen with -v mode=:
#
#   ops      prints FUNCTION <RS> OPERAND <RS> LINE (RS is octal 036), the operand as written, of every -f,
#            --file, --file=X and -f=X option that belongs to a compose
#            invocation. An option belongs to one when its command names compose
#            and the option comes before the compose subcommand ("docker compose
#            logs -f db" and "rm -f x" carry no file operand), or when one of the
#            command's -f operands is itself a docker-compose*.y*ml name.
#   assign   prints FUNCTION <RS> NAME <RS> VALUE for every assignment, local
#            or declare, for-in word, and "@READ@" for a name that read, mapfile
#            or printf -v fills. FUNCTION is "" at the top level.
#   shadow   prints W, P, R and D records for the PATH-shadow model: W<RS>line<RS>dir
#            (an executable named docker written to dir), P<RS>line<RS>dir<RS>sub
#            (dir prepended to PATH), R<RS>line (PATH overwritten, env -i, unset
#            PATH) and D<RS>line<RS>plain|abs (a docker command word). sub is 1
#            for a command in a subshell, where a prepend does not reach the
#            parent shell.
#   prepare  prints FUNCTION <RS> LINE <RS> SUB for every call of
#            eshu_ci_compose_prepare. SUB is 1 for a call in a subshell or
#            command substitution, whose export does not reach the parent shell.
#   callat   prints CALLEE <RS> CALLER <RS> LINE for every command whose first
#            word is a plain name (a function call lookup with line numbers).
#   inv      prints compose invocation facts, one record per command:
#              I<RS>function<RS>line<RS>sub<RS>overlay<RS>arrays<RS>pull   a docker
#              compose or docker-compose command (also behind env, sudo, ...);
#              overlay is 1 when a word names ESHU_CI_COMPOSE_OVERLAY or a
#              variable assigned from it; arrays lists the "${NAME[@]}"
#              expansions (comma separated); pull is 1 for up, run, build, pull,
#              create and watch, and for a command whose subcommand is not a
#              literal word (an invocation that may pull);
#              A<RS>function<RS>line<RS>sub<RS>NAME<RS>overlay   an array
#              assignment NAME=(...) or NAME+=(...), overlay as above;
#              W<RS>function<RS>line<RS>sub   a call of scripts/ci/compose.sh,
#              which prepares and appends the overlay itself.
#   sources  prints FUNCTION <RS> PATH for every source or . command (the first
#            non-flag word, as written).
#   calls    prints CALLEE <RS> CALLER <RS> ARG1 <US> ARG2 ... for every command
#            whose first word is a plain name (US is octal 037). CALLER is the
#            function holding the call, "" at the top level.
#
# An array assignment (NAME=(...) or NAME+=(...)) is read as an invocation's
# option list, so a -f in it is an operand. The attached form -fFILE is read only
# after a compose word. A function runs from its NAME() { header to the "}" at
# the header's indent.

BEGIN {
  PAREN = 0; SUBC = 0
  PULLS = " up run build pull create watch "
  SUBS = " up down logs ps run exec build config pull push create start stop restart rm kill top port images events cp ls pause unpause version wait watch attach commit export stats scale publish convert alpha "
  CTRL = " if then do else elif while until ! { ( time exec command builtin local declare typeset export readonly "
  US = "\037"; SEP = "\036"
  FN = ""; FNIND = ""; ACC = ""; HERE = ""
}

# A heredoc body is data up to its delimiter line.
HERE != "" {
  w = $0
  gsub(/^[[:space:]]+|[[:space:]]+$/, "", w)
  if (w == HERE) HERE = ""
  next
}

/^[[:space:]]*#/ && ACC == "" { next }

{
  line = $0
  if (FN != "" && FNIND != "" && line ~ ("^" FNIND "\\}")) { FN = ""; FNIND = ""; next }
  if (FN != "" && FNIND == "" && line ~ /^\}/) { FN = ""; next }
  if (line ~ /\\[[:space:]]*$/) { sub(/\\[[:space:]]*$/, "", line); ACC = ACC line " "; next }
  line = ACC line
  ACC = ""
  w = heredoc_word(line)
  if (w != "") HERE = w
  header(line)
  scan(HREST, FN, 0)
  if (ONELINE) { FN = ""; FNIND = ""; ONELINE = 0 }
}

END { if (ACC != "") scan(ACC, FN, 0) }

# heredoc_word returns the delimiter of a "<<WORD" outside quotes and $(...), or "".
function heredoc_word(s,   i, c, q, d, len, w) {
  q = ""; d = 0; len = length(s)
  for (i = 1; i <= len; i++) {
    c = substr(s, i, 1)
    if (q != "") { if (q == "\"" && c == "\\") i++; else if (c == q) q = ""; continue }
    if (c == "\\") { i++; continue }
    if (c == "\"" || c == "'" || c == "`") { q = c; continue }
    if (c == "$" && substr(s, i + 1, 1) == "(") { d++; i++; continue }
    if (d > 0) { if (c == ")") d--; continue }
    if (c == "<" && substr(s, i + 1, 1) == "<") {
      if (substr(s, i + 2, 1) == "<") { i += 2; continue }
      w = substr(s, i + 2)
      sub(/^-/, "", w); sub(/^[[:space:]]*/, "", w)
      if (w !~ /^[A-Za-z_"']/) return ""
      sub(/^["']/, "", w)
      sub(/["'].*$|[^A-Za-z0-9_].*$/, "", w)
      return w
    }
  }
  return ""
}

# header starts a function scope when the line is "name() {" or "function name".
function header(s,   nm, ind, rest) {
  nm = ""; HREST = s
  if (match(s, /^[[:space:]]*function[[:space:]]+[A-Za-z_][A-Za-z0-9_.:-]*([[:space:]]*\(\))?/)) {
    nm = substr(s, RSTART, RLENGTH); rest = substr(s, RSTART + RLENGTH)
    sub(/^[[:space:]]*function[[:space:]]+/, "", nm); sub(/[[:space:]]*\(\)$/, "", nm)
  } else if (match(s, /^[[:space:]]*[A-Za-z_][A-Za-z0-9_.:-]*[[:space:]]*\(\)/)) {
    nm = substr(s, RSTART, RLENGTH); rest = substr(s, RSTART + RLENGTH)
    sub(/^[[:space:]]+/, "", nm); sub(/[[:space:]]*\(\)$/, "", nm)
  }
  if (nm == "") return
  ind = s; sub(/[^[:space:]].*$/, "", ind)
  FN = nm; FNIND = ind
  sub(/^[[:space:]]*\{/, "", rest)
  HREST = rest
  if (rest ~ /\}[[:space:]]*;?[[:space:]]*$/) ONELINE = 1
}

# tokenize splits s into words T[1..n]; ";" marks a command boundary. Quoted
# strings and $(...) stay whole. Returns n.
function tokenize(s, T,   i, c, nx, pv, cur, q, depth, iq, len, n) {
  n = 0; cur = ""; q = ""; depth = 0; iq = ""; len = length(s)
  for (i = 1; i <= len; i++) {
    c = substr(s, i, 1)
    if (depth > 0) {
      cur = cur c
      if (iq != "") { if (iq == "\"" && c == "\\") { i++; cur = cur substr(s, i, 1) } else if (c == iq) iq = "" }
      else if (c == "\"" || c == "'") iq = c
      else if (c == "(") depth++
      else if (c == ")") depth--
      continue
    }
    if (q != "") {
      if (q == "\"" && c == "$" && substr(s, i + 1, 1) == "(") { cur = cur "$("; i++; depth = 1; continue }
      cur = cur c
      if (q == "\"" && c == "\\") { i++; cur = cur substr(s, i, 1) } else if (c == q) q = ""
      continue
    }
    if (c == "\\") { cur = cur c substr(s, i + 1, 1); i++; continue }
    if (c == "\"" || c == "'" || c == "`") { q = c; cur = cur c; continue }
    if (c == "$" && substr(s, i + 1, 1) == "(") { cur = cur "$("; i++; depth = 1; continue }
    if (c == "(" || c == ")") {
      if (cur != "") { T[++n] = cur; cur = "" }
      T[++n] = c
      continue
    }
    if (c == " " || c == "\t") { if (cur != "") { T[++n] = cur; cur = "" } continue }
    if (c == ";" || c == "|") {
      if (cur != "") { T[++n] = cur; cur = "" }
      T[++n] = ";"
      if (substr(s, i + 1, 1) == "|" || substr(s, i + 1, 1) == "&") i++
      continue
    }
    if (c == "&") {
      nx = substr(s, i + 1, 1); pv = substr(s, i - 1, 1)
      if (nx == "&" || (pv != ">" && pv != "<" && nx != ">")) {
        if (cur != "") { T[++n] = cur; cur = "" }
        T[++n] = ";"
        if (nx == "&") i++
        continue
      }
    }
    cur = cur c
  }
  if (cur != "") T[++n] = cur
  return n
}

# unq strips one layer of matching quotes.
function unq(w) {
  if (w ~ /^".*"$/ || w ~ /^'.*'$/ || w ~ /^`.*`$/) return substr(w, 2, length(w) - 2)
  return w
}

# subst_scan scans the $(...) and backquote bodies inside word w as commands.
function subst_scan(w, fn, depth,   p, d, i, c, body, rest) {
  rest = w
  while ((p = index(rest, "$(")) > 0) {
    d = 1; body = ""
    for (i = p + 2; i <= length(rest); i++) {
      c = substr(rest, i, 1)
      if (c == "(") d++
      else if (c == ")") { d--; if (d == 0) break }
      body = body c
    }
    scan(body, fn, depth + 1)
    rest = substr(rest, i + 1)
  }
}

# scan reads one logical line (or the body of a -c string or $(...)) as commands.
function scan(s, fn, depth,   T, n, a, b, i, k, o, c) {
  if (depth > 3) return
  n = tokenize(s, T)
  a = 1
  for (i = 1; i <= n + 1; i++) {
    if (i > n || T[i] == ";") {
      if (i - 1 >= a) {
        o = 0; c = 0
        for (k = a; k <= i - 1; k++) { if (T[k] == "(") o++; else if (T[k] == ")") c++ }
        if (T[a + 1] == ")" && o == 0) c--
        # SUBC: the command runs in a subshell (inside ( ), $( ) or a -c string)
        SUBC = (depth > 0 || PAREN > 0 || T[a] == "(") ? 1 : 0
        command(T, a, i - 1, fn, depth)
        if (depth == 0) { PAREN += o - c; if (PAREN < 0) PAREN = 0 }
      }
      a = i + 1
    }
  }
}

# command handles the words T[a..b] of one command.
function command(T, a, b, fn, depth,   i, w, k, nm, v, lw, w0, s, j, argn, args, skip, had, haspath, pathv) {
  for (i = a; i <= b; i++) if (index(T[i], "$(") > 0 || substr(T[i], 1, 1) == "`") subst_scan(T[i], fn, depth)
  # leading control words, declaration keywords and flags
  while (a <= b) {
    w = T[a]
    if (index(CTRL, " " w " ") > 0 || (w ~ /^-[A-Za-z]+$/ && a > 1 && index(CTRL, " " T[a - 1] " ") > 0)) { a++; continue }
    if (substr(w, 1, 2) == "$(" && w !~ /\)/) { a++; continue }
    break
  }
  # a case pattern ("name)" or "*)") ahead of a command
  if (a + 1 <= b && T[a + 1] == ")") a += 2
  if (a > b) return
  if (mode == "assign" && T[a] == "for" && a + 2 <= b && T[a + 2] == "in") {
    nm = T[a + 1]
    for (i = a + 3; i <= b; i++) { if (T[i] == "do") break; print fn SEP nm SEP T[i] }
    return
  }
  # leading NAME=value words
  asgc = 0; haspath = 0; pathv = ""; ARRNM = ""
  while (a <= b && T[a] ~ /^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/) {
    w = T[a]; nm = w; sub(/[\[+=].*$/, "", nm)
    if (nm == "PATH") { haspath = 1; v = w; sub(/^PATH\+?=/, "", v); pathv = v }
    if (tolower(nm) ~ /compose/) asgc = 1
    if (T[a + 1] == "(") { asgc = 1; ARRNM = nm }
    v = w; sub(/^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/, "", v)
    if (mode == "assign") print fn SEP nm SEP v
    if (mode == "inv" && index(v, "ESHU_CI_COMPOSE_OVERLAY") > 0) OVLALIAS[nm] = 1
    a++
  }
  if (mode == "shadow" && haspath && a > b) path_note(pathv)
  if (a > b) return
  w0 = unq(T[a])
  if (mode == "shadow") shadow_cmd(T, a, b, w0)
  if (mode == "assign") {
    if (w0 == "read" || w0 == "mapfile" || w0 == "readarray" || (w0 == "printf" && T[a + 1] == "-v")) {
      for (i = a + 1; i <= b; i++) {
        w = unq(T[i])
        if (w0 == "printf") { if (i == a + 2) print fn SEP w SEP "@READ@"; continue }
        if (w ~ /^-/) continue
        if (w ~ /^[A-Za-z_][A-Za-z0-9_]*$/) print fn SEP w SEP "@READ@"
      }
    }
  }
  if (mode == "sources" && (w0 == "source" || w0 == ".")) {
    for (i = a + 1; i <= b; i++) { if (T[i] !~ /^-/) { print fn SEP T[i]; break } }
  }
  if (mode == "prepare" && w0 == "eshu_ci_compose_prepare") print fn SEP NR SEP SUBC
  if (mode == "callat" && w0 ~ /^[A-Za-z_][A-Za-z0-9_.:-]*$/) print w0 SEP fn SEP NR
  if (mode == "inv") inv(T, a, b, ARRNM, fn)
  if (w0 == "eval") { s = ""; for (i = a + 1; i <= b; i++) s = s " " unq(T[i]); scan(s, fn, depth + 1) }
  if (w0 ~ /(^|\/)(bash|sh|zsh|dash|ksh)$/) {
    for (i = a + 1; i < b; i++) if (T[i] ~ /^-[A-Za-z]*c$/) { scan(unq(T[i + 1]), fn, depth + 1); break }
  }
  if (mode == "calls" && w0 ~ /^[A-Za-z_][A-Za-z0-9_.:-]*$/) {
    args = ""; skip = 0; argn = 0
    for (i = a + 1; i <= b; i++) {
      w = T[i]
      if (skip) { skip = 0; continue }
      if (w ~ /^[0-9]*(>>?|<|&>|>&)$/) { skip = 1; continue }
      if (w ~ /^[0-9]*(>>?|<|&>|>&)./) continue
      args = args (argn++ ? US : "") w
    }
    print w0 SEP fn SEP args
  }
  if (mode == "ops") ops(T, a, b, asgc, fn)
}

# ops emits the compose -f operands of the command T[a..b].
function ops(T, a, b, ctx, fn,   i, w, lw, inc, subc, suba, an, cn, lit, k, A, C, op) {
  inc = ctx; subc = 0; suba = 0; an = 0; cn = 0; lit = 0
  for (i = a; i <= b; i++) {
    w = T[i]; lw = w; gsub(/["'`]/, "", lw)
    op = ""
    if (lw == "-f" || lw == "--file") { if (i < b) { i++; op = T[i] } }
    else if (lw ~ /^--file=/ || lw ~ /^-f=/) op = substr(w, index(w, "=") + 1)
    else if (inc && !subc && lw ~ /^-f./ && lw !~ /^-f[a-zA-Z]+$/ && lw !~ /[[:space:]]/ && w !~ /^["'`]/) op = substr(w, 3)
    else {
      if (tolower(lw) ~ /compose/) inc = 1
      else if (index(SUBS, " " lw " ") > 0) { subc = 1; suba = 1 }
      continue
    }
    if (op == "") continue
    if (!suba) A[++an] = op
    if (inc && !subc) C[++cn] = op
    k = op; gsub(/["'`]/, "", k)
    if (k ~ /docker-compose[A-Za-z0-9_.-]*\.ya?ml$/) lit = 1
  }
  if (lit) { for (k = 1; k <= an; k++) print fn SEP A[k] SEP NR }
  else { for (k = 1; k <= cn; k++) print fn SEP C[k] SEP NR }
}

# inv emits the compose invocation facts of the command T[a..b] (mode inv). arr is
# the name of an array assigned by the command, or "".
function inv(T, a, b, arr, fn,   i, w, lw, ovl, comp, arrs, nm, w0, base, k, nx, sc, pull, al) {
  ovl = 0; comp = 0; arrs = ""
  w0 = unq(T[a]); base = w0; sub(/^.*\//, "", base)
  for (i = a; i <= b; i++) {
    w = T[i]; lw = w; gsub(/["'`]/, "", lw)
    if (index(w, "ESHU_CI_COMPOSE_OVERLAY") > 0) ovl = 1
    else for (al in OVLALIAS) if (index(w, "$" al) > 0 || index(w, "${" al) > 0) ovl = 1
    if (lw ~ /^\$\{?[A-Za-z_][A-Za-z0-9_]*\[[@*]\]\}?$/) {
      nm = lw; sub(/^\$\{?/, "", nm); sub(/\[.*$/, "", nm)
      arrs = arrs (arrs == "" ? "" : ",") nm
    }
  }
  if (arr != "") { print "A" SEP fn SEP NR SEP SUBC SEP arr SEP ovl; return }
  # the command word, behind a wrapper
  k = a
  if (base ~ /^(env|sudo|doas|exec|command|builtin|nohup|time|timeout|nice|stdbuf|setsid|ionice)$/) {
    for (k = a + 1; k <= b; k++) {
      lw = unq(T[k])
      if (lw ~ /^-/ || lw ~ /^[A-Za-z_][A-Za-z0-9_]*=/) continue
      break
    }
  }
  if (k > b) return
  lw = unq(T[k]); base = lw; sub(/^.*\//, "", base)
  if (lw ~ /ci\/compose\.sh$/) { print "W" SEP fn SEP NR SEP SUBC; return }
  if ((base == "bash" || base == "sh") && k < b && unq(T[k + 1]) ~ /ci\/compose\.sh$/) { print "W" SEP fn SEP NR SEP SUBC; return }
  if (base == "docker-compose") comp = 1
  else if (base == "docker") {
    for (i = k + 1; i <= b; i++) {
      nx = unq(T[i])
      if (nx ~ /^-/) { if (nx ~ /^(--context|--config|-H|--host|-c|-l|--log-level)$/) i++; continue }
      if (nx == "compose") comp = 1
      break
    }
  }
  if (!comp) return
  # the subcommand: the first word that names one
  sc = ""
  for (i = k + 1; i <= b; i++) {
    nx = unq(T[i])
    if (index(SUBS, " " nx " ") > 0) { sc = nx; break }
  }
  pull = (sc == "" || index(PULLS, " " sc " ") > 0) ? 1 : 0
  print "I" SEP fn SEP NR SEP SUBC SEP ovl SEP arrs SEP pull
}

# path_note records a standalone PATH assignment: a prepend (the value starts
# with a directory and keeps $PATH) is "P", an overwrite is "R", an append does
# nothing for the shadow.
function path_note(v,   q, first) {
  q = unq(v)
  if (q ~ /\$PATH|\$\{PATH[}:]/) {
    if (q ~ /^(\$PATH|\$\{PATH\})/) return
    first = q; sub(/:.*$/, "", first)
    print "P" SEP NR SEP first SEP SUBC
  } else print "R" SEP NR
}

# stub_target records a write of an executable named docker into a directory.
function stub_target(p,   d) {
  if (p ~ /\/docker$/) { d = p; sub(/\/docker$/, "", d); print "W" SEP NR SEP d SEP SUBC }
}

# shadow_cmd records, for one command: stub writes (redirect, cp, install, ln, mv,
# tee), PATH resets (env -i, unset PATH) and docker command words ("plain" for the
# bare word, "abs" for a path or command -p, which a PATH prepend does not shadow).
function shadow_cmd(T, a, b, w0,   i, t, p, last) {
  for (i = a; i <= b; i++) {
    t = T[i]
    if (t ~ /^[0-9]*>>?$/) { if (i < b) stub_target(unq(T[i + 1])); continue }
    if (t ~ /^[0-9]*>>?./) { p = t; sub(/^[0-9]*>>?/, "", p); stub_target(unq(p)) }
  }
  last = ""
  for (i = a + 1; i <= b; i++) if (T[i] !~ /^-/ && T[i] !~ /^[0-9]*[<>]/) last = T[i]
  if ((w0 == "cp" || w0 == "install" || w0 == "ln" || w0 == "mv") && last != "") stub_target(unq(last))
  if (w0 == "tee") for (i = a + 1; i <= b; i++) if (T[i] !~ /^-/) stub_target(unq(T[i]))
  if (w0 == "env") for (i = a + 1; i <= b; i++) if (T[i] == "-i" || T[i] == "--ignore-environment") print "R" SEP NR
  if (w0 == "unset") for (i = a + 1; i <= b; i++) if (unq(T[i]) == "PATH") print "R" SEP NR
  if (w0 == "docker" || w0 == "docker-compose") print "D" SEP NR SEP "plain"
  else if (w0 ~ /\/(docker|docker-compose)$/) print "D" SEP NR SEP "abs"
  else if (w0 ~ /^(env|sudo|doas|exec|nice|nohup|timeout|stdbuf|setsid|ionice|time|command|builtin|xargs)$/ || w0 ~ /\/(env|sudo)$/) {
    # a wrapper runs the docker word it is given: plain unless the wrapper sets
    # PATH itself, names a path, or is command -p (a PATH prepend never shadows it)
    p = ""
    for (i = a + 1; i <= b; i++) {
      t = unq(T[i])
      if (t ~ /^PATH=/) print "R" SEP NR
      if (w0 == "command" && t ~ /^-[a-z]*p/) p = "abs"
      if (t == "docker" || t == "docker-compose") { print "D" SEP NR SEP (p == "" ? "plain" : p); break }
      if (t ~ /\/(docker|docker-compose)$/) { print "D" SEP NR SEP "abs"; break }
    }
  }
}
