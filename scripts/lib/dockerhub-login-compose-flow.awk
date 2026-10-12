# dockerhub-login-compose-flow.awk - reads shell text for the compose file flow of
# verify-dockerhub-login.sh (#7886). It is a bounded reader, not a shell: quotes,
# $(...) and backquotes keep a word whole, backslash continuations join, heredoc
# bodies and full-line comments are skipped, and a quoted string is data unless
# it follows -c or eval. One mode per run, chosen with -v mode=:
#
#   ops      prints FUNCTION <RS> OPERAND <RS> LINE <RS> WRAPPER (RS is octal 036), the operand as written, of every -f,
#            --file, --file=X and -f=X option that belongs to a compose
#            invocation. An option belongs to one when its command names compose
#            and the option comes before the compose subcommand ("docker compose
#            logs -f db" and "rm -f x" carry no file operand), or when one of the
#            command's -f operands is itself a docker-compose*.y*ml name.
#            WRAPPER is 1 when the command is a wrapper call (is_wrapper).
#   assign   prints FUNCTION <RS> NAME <RS> VALUE for every assignment, local
#            or declare, for-in word, and "@READ@" for a name that read, mapfile
#            or printf -v fills. FUNCTION is "" at the top level.
#   shadow   prints W, P, R and D records for the PATH-shadow model: W<RS>line<RS>dir
#            (an executable named docker written to dir), P<RS>line<RS>dir<RS>sub
#            (dir prepended to PATH), R<RS>line (PATH overwritten, env -i, unset
#            PATH) and D<RS>line<RS>plain|abs (a docker command word). sub is 1
#            for a command in a subshell, where a prepend does not reach the
#            parent shell.
#   wnames   prints FUNCTION <RS> NAME for every docker-compose*.y*ml name that sits
#            inside a call of the CI compose wrapper (see is_wrapper), one record
#            per occurrence, so the caller can tell a name used only by wrapper
#            calls from a name a raw call also uses.
#   wsum     prints one line, WRAPPER <RS> RAW <RS> LOOKALIKE: the number of
#            wrapper calls, the number of other commands that name compose, and
#            the number of other commands that look like a wrapper call: a word
#            that ends in compose.sh, a -f or -p option and a -- word (another
#            directory, a longer name, a second prefix).
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
  PAREN = 0; SUBC = 0; STRN = 0
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
function tokenize(s, T, S,   i, c, nx, pv, cur, q, depth, iq, len, n) {
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
      nx = substr(s, i + 1, 1)
      S[n] = (c == ";") ? ";" : ((nx == "|") ? "||" : "|")
      if (nx == "|" || nx == "&") i++
      continue
    }
    if (c == "&") {
      nx = substr(s, i + 1, 1); pv = substr(s, i - 1, 1)
      if (nx == "&" || (pv != ">" && pv != "<" && nx != ">")) {
        if (cur != "") { T[++n] = cur; cur = "" }
        T[++n] = ";"
        S[n] = (nx == "&") ? "&&" : "&"
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
function scan(s, fn, depth,   T, S, n, a, b, i, k, o, c, pk, nk) {
  if (depth > 3) return
  n = tokenize(s, T, S)
  a = 1
  for (i = 1; i <= n + 1; i++) {
    if (i > n || T[i] == ";") {
      if (i - 1 >= a) {
        o = 0; c = 0
        for (k = a; k <= i - 1; k++) { if (T[k] == "(") o++; else if (T[k] == ")") c++ }
        if (T[a + 1] == ")" && o == 0) c--
        # SUBC: the command runs in a subshell (inside ( ), $( ) or a -c string)
        SUBC = (depth > 0 || PAREN > 0 || T[a] == "(") ? 1 : 0
        # a pipeline element and a background job run in a subshell
        pk = (a > 1) ? S[a - 1] : ""; nk = (i <= n) ? S[i] : ""
        if (pk == "|" || nk == "|" || nk == "&") SUBC = 1
        command(T, a, i - 1, fn, depth)
        if (depth == 0) { PAREN += o - c; if (PAREN < 0) PAREN = 0 }
      }
      a = i + 1
    }
  }
}

# command handles the words T[a..b] of one command.
function command(T, a, b, fn, depth,   i, w, k, nm, v, lw, w0, s, j, argn, args, skip, had, haspath, pathv, isw, lk, sep, flg, pmo) {
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
  asgc = 0; haspath = 0; pathv = ""; pmo = 0
  while (a <= b && T[a] ~ /^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/) {
    w = T[a]; nm = w; sub(/[\[+=].*$/, "", nm)
    # a prefix that turns GITHUB_ACTIONS or CI image mode off in front of the wrapper
    if (nm == "GITHUB_ACTIONS" || nm == "ESHU_CI_IMAGES") {
      v = w; sub(/^[^=]*=/, "", v)
      if (nm == "GITHUB_ACTIONS" ? unq(v) != "true" : unq(v) != "1") pmo = 1
    }
    if (nm == "PATH") { haspath = 1; v = w; sub(/^PATH\+?=/, "", v); pathv = v }
    if (tolower(nm) ~ /compose/) asgc = 1
    if (T[a + 1] == "(") asgc = 1
    v = w; sub(/^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/, "", v)
    if (mode == "assign") print fn SEP nm SEP v
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
  isw = (mode == "ops" || mode == "wnames" || mode == "wsum") ? is_wrapper(T, a, b) : 0
  if (pmo) isw = 0
  # NAME=(bash .../scripts/ci/compose.sh args --): a command array that holds the wrapper
  if (isw == 0 && T[a] == "(" && (mode == "ops" || mode == "wnames" || mode == "wsum")) isw = is_wrapper(T, a + 1, b)
  if (mode == "wnames" && isw) wnames(T, a, b, fn)
  if (mode == "wsum") {
    if (isw) NW++
    else {
      for (i = a; i <= b; i++) if (tolower(T[i]) ~ /compose/) { NRC++; break }
      lk = 0; sep = 0; flg = 0
      for (i = a; i <= b; i++) {
        w = unq(T[i])
        if (w ~ /(^|\/)compose\.sh[A-Za-z0-9_.~-]*$/) lk = 1
        else if (w == "--") sep = 1
        else if (w == "-f" || w == "-p" || w == "--file" || w == "--project-name") flg = 1
      }
      if (lk && sep && flg) NLK++
    }
  }
  # a literal string handed to eval or bash -c is read as commands
  if (w0 == "eval") { s = ""; for (i = a + 1; i <= b; i++) s = s " " unq(T[i]); STRN++; scan(s, fn, depth + 1); STRN-- }
  if (w0 ~ /(^|\/)(bash|sh|zsh|dash|ksh)$/) {
    for (i = a + 1; i < b; i++) if (T[i] ~ /^-[A-Za-z]*c$/) { STRN++; scan(unq(T[i + 1]), fn, depth + 1); STRN--; break }
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
  if (mode == "ops") ops(T, a, b, asgc, fn, isw)
}

# ops emits the compose -f operands of the command T[a..b].
function ops(T, a, b, ctx, fn, isw,   i, w, lw, inc, subc, suba, an, cn, lit, k, A, C, op) {
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
  if (lit) { for (k = 1; k <= an; k++) print fn SEP A[k] SEP NR SEP isw + 0 }
  else { for (k = 1; k <= cn; k++) print fn SEP C[k] SEP NR SEP isw + 0 }
}

# is_wrapper is 1 when the command T[a..b] runs the CI compose wrapper: its command
# word is the literal path tail scripts/ci/compose.sh, with no prefix, ./, or one
# variable and a slash, behind any of bash, sh, exec, timeout, nice, sudo -E, env or a
# NAME=value word. A path held entirely in a variable, bash -c and any other
# wrapper (xargs, retry) are not the wrapper: they are raw calls. Words that run
# before the path decide the call too (headbad): a compose call, or a command that
# removes GITHUB_ACTIONS or CI image mode from the wrapper's environment.
function is_wrapper(T, a, b,   i, w, base, j) {
  # the literal wrapper path right after bash or sh anywhere in the command: a runner
  # function (retry, a capture helper) that is handed "bash scripts/ci/compose.sh ..."
  for (j = a; j < b; j++) {
    w = unq(T[j]); base = w; sub(/^.*\//, "", base)
    if (base != "bash" && base != "sh") continue
    i = j + 1
    while (i <= b && unq(T[i]) ~ /^-/) { if (unq(T[i]) ~ /^-[A-Za-z]*c$/) break; i++ }
    if (i <= b && unq(T[i]) !~ /^-/ && wpath(T[i]) && !headbad(T, a, j - 1)) return 1
  }
  i = a
  while (i <= b) {
    w = unq(T[i]); base = w; sub(/^.*\//, "", base)
    if (w ~ /^[A-Za-z_][A-Za-z0-9_]*=/) { i++; continue }
    if (base == "timeout") {
      i++
      while (i <= b && unq(T[i]) ~ /^-/) { if (unq(T[i]) ~ /^(-k|-s|--kill-after|--signal)$/) i++; i++ }
      if (i <= b && unq(T[i]) ~ /^[0-9][0-9.]*[smhd]?$/) i++
      continue
    }
    if (base == "nice") {
      i++
      while (i <= b && unq(T[i]) ~ /^-/) { if (unq(T[i]) ~ /^(-n|--adjustment)$/) i++; i++ }
      continue
    }
    if (base == "sudo") {
      i++
      while (i <= b && unq(T[i]) ~ /^-/) { if (unq(T[i]) ~ /^(-u|-g|-h|-p|-C|-D|-R|-T|-U)$/) i++; i++ }
      continue
    }
    if (base == "env") {
      i++
      while (i <= b && (unq(T[i]) ~ /^-/ || unq(T[i]) ~ /^[A-Za-z_][A-Za-z0-9_]*=/)) { if (unq(T[i]) ~ /^(-u|-C|-S|--unset|--chdir)$/) i++; i++ }
      continue
    }
    if (base == "exec" || base == "command" || base == "nohup" || base == "time") {
      i++
      while (i <= b && unq(T[i]) ~ /^-[A-Za-z]+$/) i++
      continue
    }
    if (base == "bash" || base == "sh") {
      i++
      while (i <= b && unq(T[i]) ~ /^-/) { if (unq(T[i]) ~ /^-[A-Za-z]*c$/) return 0; i++ }
      return (i <= b) ? (wpath(T[i]) && !headbad(T, a, i - 1)) : 0
    }
    break
  }
  return (i <= b) ? (wpath(T[i]) && !headbad(T, a, i - 1)) : 0
}

# wpath is 1 for the wrapper path tail with an allowed prefix.
function wpath(w,   q) {
  q = unq(w)
  return (q ~ /^(\.\/|\$\{[A-Za-z_][A-Za-z0-9_]*\}\/|\$[A-Za-z_][A-Za-z0-9_]*\/|"\$[A-Za-z_][A-Za-z0-9_]*"\/|"\$\{[A-Za-z_][A-Za-z0-9_]*\}"\/)?scripts\/ci\/compose\.sh$/) ? 1 : 0
}

# wnames prints every docker-compose*.y*ml name inside the words of a wrapper call.
function wnames(T, a, b, fn,   i, w) {
  for (i = a; i <= b; i++) {
    w = T[i]
    while (match(w, /[A-Za-z0-9_.\/-]*docker-compose[A-Za-z0-9_.-]*\.ya?ml/)) {
      print fn SEP substr(w, RSTART, RLENGTH)
      w = substr(w, RSTART + RLENGTH)
    }
  }
}

END { if (mode == "wsum") print NW + 0 SEP NRC + 0 SEP NLK + 0 }

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
