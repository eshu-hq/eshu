# dockerhub-login-compose-flow.awk - reads shell text for the compose file flow of
# verify-dockerhub-login.sh (#7886). It is a bounded reader, not a shell: quotes,
# $(...) and backquotes keep a word whole, backslash continuations join, heredoc
# bodies and full-line comments are skipped, and a quoted string is data unless
# it follows -c or eval. One mode per run, chosen with -v mode=:
#
#   ops      prints FUNCTION <RS> OPERAND (RS is octal 036), the operand as written, of every -f,
#            --file, --file=X and -f=X option that belongs to a compose
#            invocation. An option belongs to one when its command names compose
#            and the option comes before the compose subcommand ("docker compose
#            logs -f db" and "rm -f x" carry no file operand), or when one of the
#            command's -f operands is itself a docker-compose*.y*ml name.
#   assign   prints FUNCTION <RS> NAME <RS> VALUE for every assignment, local
#            or declare, for-in word, and "@READ@" for a name that read, mapfile
#            or printf -v fills. FUNCTION is "" at the top level.
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
function scan(s, fn, depth,   T, n, a, b, i) {
  if (depth > 3) return
  n = tokenize(s, T)
  a = 1
  for (i = 1; i <= n + 1; i++) {
    if (i > n || T[i] == ";") {
      if (i - 1 >= a) command(T, a, i - 1, fn, depth)
      a = i + 1
    }
  }
}

# command handles the words T[a..b] of one command.
function command(T, a, b, fn, depth,   i, w, k, nm, v, lw, w0, s, j, argn, args, skip, had) {
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
  asgc = 0
  while (a <= b && T[a] ~ /^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/) {
    w = T[a]; nm = w; sub(/[\[+=].*$/, "", nm)
    if (tolower(nm) ~ /compose/) asgc = 1
    if (T[a + 1] == "(") asgc = 1
    v = w; sub(/^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/, "", v)
    if (mode == "assign") print fn SEP nm SEP v
    a++
  }
  if (a > b) return
  w0 = unq(T[a])
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
  if (lit) { for (k = 1; k <= an; k++) print fn SEP A[k] }
  else { for (k = 1; k <= cn; k++) print fn SEP C[k] }
}
