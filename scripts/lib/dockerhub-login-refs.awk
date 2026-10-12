# dockerhub-login-refs.awk - repository scripts a shell text RUNS, for
# verify-dockerhub-login.sh (#7886).
#
# usage: awk [-v bare=1] -f dockerhub-login-refs.awk file...
#
# It prints one record per distinct script path (a path ending in .sh or .bash)
# that a command in the file runs or sources:
#
#   <file> ":" <token>        (just <token> with -v bare=1)
#
# The verifier resolves each token to a file in the repository and follows it, so
# a script's docker use is not hidden by what precedes the call. Each line is cut
# into simple commands at ; & ( ) { } backquotes and a | that is followed by white
# space or another | (so several calls on one line are all seen). In one command, after any if/then/do/else/!/{ keyword and any
# NAME=value prefixes, the command word decides:
#   - a script path itself (scripts/x.sh, ./x.sh, /abs/x.sh, ${VAR}/x.sh,
#     "$(dirname "$0")/x.sh"): run it;
#   - source or . : sources the first script path after it;
#   - sudo, doas, env, exec, nice, nohup, xargs, timeout, stdbuf, setsid, ionice,
#     time, command, builtin (also path-qualified, /usr/bin/env): runs the first
#     script path after it, past any flags, values and NAME=value words;
#   - a shell (bash, sh, zsh, dash, ksh, also path-qualified, /bin/bash), as the
#     command word or after any command (retry 3 bash x.sh): runs the first word
#     after its flags (-eu, -o pipefail, --norc) when that is a script path;
#   - xargs anywhere on a line: every script path on the line is followed, because
#     the paths are piped to it (echo scripts/x.sh | xargs bash).
# A script path that is only an argument (shellcheck scripts/x.sh, test -f
# scripts/x.sh, prepr="${root}/scripts/pre-pr.sh") is data and is not followed.
# A word that merely starts with a script path (scripts/x.sh|scripts/y.sh, a case
# pattern scripts/x.sh) return ;;) in command position counts as that call: the
# gate fails closed rather than parse case patterns and data tables.
#
# Quotes and comments, per logical line (a trailing backslash joins the next
# line; any other line break ends the command):
#   - a # that starts the line or follows white space opens a comment (so $# and
#     ${x#y} are not comments);
#   - a single-quoted string is data and is skipped;
#   - a double-quoted string that begins with a script path (optionally behind
#     ${VAR}/, $VAR/ or $(...)/) is that path as one word; a double-quoted string
#     that holds $( or a backquote is code and is scanned; any other double-quoted
#     string is data and is skipped;
#   - an unterminated quote is not a string: the rest of the line is scanned.
# So a script named only in a comment, or only inside a quoted string that no
# shell evaluates (bash -c 'scripts/x.sh'), or held in a variable and run through
# it (for s in scripts/a.sh; do bash "$s"), is not followed: documented limits of
# the verifier.
#
# POSIX awk only (BSD awk, gawk and mawk).

BEGIN {
  US = "\037"
  PH = "\036"
  PHRUN = "^" PH "+"
  # A script path word; the awk ERE has no \b, so a trailing word character is
  # rejected by the callers that search inside a longer text.
  TOK = "[A-Za-z0-9_./-]+\\.(sh|bash)"
  TOKP = "^" TOK
  # "<path>, "${VAR}/<path>, "$VAR/<path> or "$(...)/<path> at the start of a
  # double-quoted string.
  QPATH = "^\"((\\$\\{?[A-Za-z_][A-Za-z0-9_]*\\}?|\\$\\([^)]*\\))/)?" TOK
  split("then do else elif if while until ! time", t, " ")
  for (i in t) KW[t[i]] = 1
  split("sudo doas env exec nice nohup xargs timeout stdbuf setsid ionice time command builtin", t, " ")
  for (i in t) WRAP[t[i]] = 1
  split("bash sh zsh dash ksh", t, " ")
  for (i in t) SHELL[t[i]] = 1
}

# A line that ends in a backslash continues on the next line, so the next line is
# not in command position (a quoted path that starts a continuation line of a
# for-list or an argument list is data). A line with a comment is never joined: a
# backslash at the end of a comment does not continue it.
FNR == 1 { if (buf != "") { scan(buf); buf = "" } hd_flush(); fname = FILENAME; HDn = 0 }
# A heredoc body is the stdin of its command, not commands, unless a shell reads
# it. HDrun is set by hd_open from the line that opened it. Lines of an unquoted
# body that hold $( or a backquote are scanned (they execute at expansion).
HDn > 0 {
  line = $0
  if (HDdash[HDi]) sub(/^\t+/, "", line)
  if (line == HDword[HDi]) { HDnl = 0; HDi++; if (HDi > HDn) HDn = 0; next }
  HDL[++HDnl] = line
  if (HDrun[HDi] || (!HDquoted[HDi] && (index(line, "$(") > 0 || index(line, "`") > 0))) scan(line)
  next
}
{
  if ($0 ~ /\\$/ && $0 !~ /\\\\$/ && $0 !~ /(^|[ \t])#/) { buf = buf substr($0, 1, length($0) - 1) " "; next }
  full = buf $0; buf = ""
  scan(full)
  hd_open(full)
}
END { if (buf != "") scan(buf); hd_flush() }

# hd_flush: a heredoc that never found its delimiter was not a heredoc (an
# arithmetic << or a shape this reader misjudged). Its lines are commands: scan
# them, so a misread never hides a call.
function hd_flush(   k) {
  if (HDn > 0) for (k = 1; k <= HDnl; k++) scan(HDL[k])
  HDn = 0; HDnl = 0
}

# hd_open finds the heredocs a logical line opens (<<WORD, <<-WORD, <<'WORD',
# <<"WORD", <<\WORD; not <<< and not inside quotes or a comment) and arms the body
# handler. A body is run as code when the line holds a shell word (bash, sh, zsh,
# dash, ksh, eval, source, a dot command) or ends in a pipe, && or || (the body
# feeds a command on a later line), or when the heredoc is written to a file
# (> file, >> file, tee: the file may be run later). An arithmetic << ($(( )),
# (( )), [[ ]], let) opens no heredoc, and a heredoc without a delimiter line is
# read as commands again (hd_flush).
function hd_open(s,   i, n, ch, q, w, d, lit, code, run, tail, t, e) {
  n = length(s); q = ""; HDn = 0; HDnl = 0
  code = s
  run = (code ~ /(^|[^A-Za-z0-9_.\/-])(bash|sh|zsh|dash|ksh|eval|source)([^A-Za-z0-9_]|$)/ || code ~ /(^|[ \t;&|(])\.[ \t]/ || code ~ /(\||&&)[ \t]*$/)
  t = s
  gsub(/'[^']*'/, "", t); gsub(/"[^"]*"/, "", t)
  gsub(/[0-9]*>>?[ \t]*\/dev\/null/, "", t); gsub(/[0-9]*>&[0-9-]*/, "", t); gsub(/&>[ \t]*\/dev\/null/, "", t)
  if (t ~ /(^|[^<>=-])>/ || t ~ /(^|[ \t|;&(])tee[ \t]/) run = 1
  for (i = 1; i <= n; i++) {
    ch = substr(s, i, 1)
    if (q != "") { if (ch == "\\" && q == "\"") i++; else if (ch == q) q = ""; continue }
    if (ch == "'" || ch == "\"") { q = ch; continue }
    if (ch == "#" && (i == 1 || substr(s, i - 1, 1) ~ /[ \t]/)) break
    if (substr(s, i, 2) == "((" || substr(s, i, 3) == "$((") {
      e = index(substr(s, i + 2), "))")
      if (e == 0) break
      i = i + 2 + e; continue
    }
    if (substr(s, i, 2) == "[[") {
      e = index(substr(s, i + 2), "]]")
      if (e == 0) break
      i = i + 2 + e; continue
    }
    if (substr(s, i, 4) ~ /^let[ \t]/ && (i == 1 || substr(s, i - 1, 1) ~ /[ \t;&|(]/)) break
    if (ch == "<" && substr(s, i + 1, 1) == "<" && substr(s, i + 2, 1) != "<" && (i == 1 || substr(s, i - 1, 1) != "<")) {
      d = 0; lit = 0; i += 2
      if (substr(s, i, 1) == "-") { d = 1; i++ }
      while (substr(s, i, 1) ~ /[ \t]/) i++
      ch = substr(s, i, 1)
      if (ch == "'" || ch == "\"") { lit = 1; i++ } else if (ch == "\\") { lit = 1; i++ }
      w = ""
      while (substr(s, i, 1) ~ /[A-Za-z0-9_]/) { w = w substr(s, i, 1); i++ }
      if (w == "") continue
      HDn++; HDword[HDn] = w; HDdash[HDn] = d; HDquoted[HDn] = lit; HDrun[HDn] = run
      if (substr(s, i, 1) == "'" || substr(s, i, 1) == "\"") i++
      i--
    }
  }
  HDi = 1
}

# first_special returns the index of the first quote, backslash or # in s, or 0.
function first_special(s,   i, best, k, ch) {
  best = 0
  split("\" ' # \\", ch, " ")
  for (i = 1; i <= 4; i++) {
    k = index(s, ch[i])
    if (k > 0 && (best == 0 || k < best)) best = k
  }
  return best
}

# close_dq returns the index of the closing double quote in s (s starts after the
# opening quote), skipping backslash escapes, or 0 when the string is unterminated.
function close_dq(s,   i, n, ch) {
  n = length(s)
  for (i = 1; i <= n; i++) {
    ch = substr(s, i, 1)
    if (ch == "\\") i++
    else if (ch == "\"") return i
  }
  return 0
}

function base(w) { sub(/^.*\//, "", w); return w }
# tokof returns the script path a word starts with, or "": the path must end the
# word or be followed by a non-word character (scripts/x.sh|scripts/y.sh, x.sh:).
function tokof(w) {
  sub(PHRUN, "", w)
  if (!match(w, TOKP) || substr(w, RLENGTH + 1, 1) ~ /[A-Za-z0-9_]/) return ""
  return substr(w, 1, RLENGTH)
}
function istok(w) { return tokof(w) != "" }

# follow records the script path word w (a leading ${VAR}/ placeholder and the
# slash after it go: the verifier resolves repo-relative paths).
function follow(w) {
  w = tokof(w)
  sub(/^\/+/, "", w)
  if ((fname SUBSEP w) in SEEN) return
  SEEN[fname SUBSEP w] = 1
  if (bare) print w
  else print fname ":" w
}

# scan builds the code text of one line (comments dropped, data strings blanked,
# a leading script path in a double-quoted string kept) and then walks its simple
# commands.
function scan(line,   out, k, c, pre, rest, j, body, last, content) {
  out = ""
  while (line != "") {
    k = first_special(line)
    if (k == 0) { out = out line; break }
    pre = substr(line, 1, k - 1)
    c = substr(line, k, 1)
    out = out pre
    rest = substr(line, k + 1)
    if (c == "\\") { out = out " "; line = substr(rest, 2); continue }
    if (c == "#") {
      last = substr(out, length(out), 1)
      if (out == "" || last == " " || last == "\t") break
      out = out "#"; line = rest; continue
    }
    if (c == "'") {
      j = index(rest, "'")
      if (j == 0) { line = rest; continue }
      out = out US; line = substr(rest, j + 1); continue
    }
    # c is a double quote.
    if (match(substr(line, k), QPATH) && substr(line, k + RLENGTH, 1) !~ /[A-Za-z0-9_]/) {
      content = substr(line, k + 1, RLENGTH - 1)
      sub(/^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?\//, "/", content)
      sub(/^\$\([^)]*\)\//, "/", content)
      # Glue the path to a preceding word (name="${root}/x.sh" stays one assignment
      # word); a quoted path that starts a word stands alone.
      if (match(content, TOK)) {
        last = substr(out, length(out), 1)
        out = out ((last == "" || last ~ /[ \t;&|(){}`]/) ? " " : "") substr(content, RSTART, RLENGTH) " "
      }
      line = substr(line, k + RLENGTH)
      j = index(line, "\"")
      line = (j == 0) ? line : substr(line, j + 1)
      continue
    }
    j = close_dq(rest)
    if (j == 0) { line = rest; continue }
    body = substr(rest, 1, j - 1)
    gsub(/\\./, " ", body)
    if (index(body, "$(") > 0 || index(body, "`") > 0) out = out " " body " "
    else out = out US
    line = substr(rest, j + 1)
  }
  commands(out)
}

# commands cuts the code text of a line into simple commands and follows the
# scripts each one runs. A case pattern that starts with a script path
# (scripts/x.sh) return ;;) is followed like a command: the gate fails closed.
function commands(s,   n, i, ch, nx, cur, ns, SEG, nw, W, j, k, w, c, xargs, na, ALL) {
  # ${VAR} and $VAR are one placeholder; $(...)/ before a path too.
  gsub(/\$\{[^}]*\}/, PH, s)
  gsub(/\$\([^)]*\)\//, PH "/", s)
  gsub(/\$[A-Za-z_][A-Za-z0-9_]*/, PH, s)
  # A pipe is a separator only before white space, ||, or the end of the line.
  n = length(s); cur = ""; ns = 0
  for (i = 1; i <= n; i++) {
    ch = substr(s, i, 1)
    nx = substr(s, i + 1, 1)
    if (ch == "|" && nx != "|" && nx != "" && nx != " " && nx != "\t") { cur = cur ch; continue }
    if (ch == "|" && nx == "|") i++
    if (ch ~ /[;&{}()`|]/) { SEG[++ns] = cur; cur = ""; continue }
    cur = cur ch
  }
  SEG[++ns] = cur
  xargs = 0; na = 0
  for (i = 1; i <= ns; i++) {
    nw = split(SEG[i], W, /[ \t]+/)
    n = 0
    for (j = 1; j <= nw; j++) if (W[j] != "") { n++; SEGW[n] = W[j]; ALL[++na] = W[j]; if (base(W[j]) == "xargs") xargs = 1 }
    if (n == 0) continue
    k = 1
    while (k <= n && ((SEGW[k] in KW) || SEGW[k] ~ /^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=/)) k++
    if (k > n) continue
    c = SEGW[k]
    if (istok(c)) follow(c)
    else if (c == "source" || c == "." || (base(c) in WRAP)) {
      for (j = k + 1; j <= n; j++) if (istok(SEGW[j])) { follow(SEGW[j]); break }
    }
    for (j = k; j <= n; j++) {
      if (!(base(SEGW[j]) in SHELL) || istok(SEGW[j])) continue
      w = j + 1
      while (w <= n && SEGW[w] ~ /^[-+]/) { if (SEGW[w] ~ /^[-+][oO]$/) w++; w++ }
      if (w <= n && istok(SEGW[w])) follow(SEGW[w])
    }
  }
  if (xargs) for (j = 1; j <= na; j++) if (istok(ALL[j])) follow(ALL[j])
}
