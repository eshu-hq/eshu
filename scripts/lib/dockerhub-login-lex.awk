# dockerhub-login-lex.awk - shell command lexer for verify-dockerhub-login.sh
# (#7886).
#
# usage: awk -f dockerhub-login-lex.awk file...
#
# It reads shell text (a workflow run: body or a repository script), splits it
# into simple commands the way a shell would (quotes, comments, line
# continuations, ; & | ( ) and command or process substitution), and emits one
# record per docker mention that can matter to the Docker Hub rate limit:
#
#   <file> TAB <kind> TAB <word> US <word> ...        (US is octal 037)
#
# kinds:
#   O  a docker command that may pull or build (run, pull, create, build,
#      buildx, image pull|build, container run|create, or a subcommand the
#      lexer does not recognise); words are normalised to "docker <sub> ..."
#      with the global flags (--config, --context, -H, ...) removed.
#   C  docker compose / docker-compose; words start with docker or
#      docker-compose.
#   U  a bare docker word the lexer cannot classify (an unknown command with
#      docker as an argument, a variable invoked as a command whose name says
#      docker or that was assigned a docker value the lexer cannot follow, a
#      heredoc body run by a shell). Callers count it as Docker Hub.
#   V  a variable invoked as a command ("$rt" pull ...) that this file never
#      assigns a docker value and cannot prove literal: its value may come from
#      the environment or a sourced file. It means nothing alone.
#   D  the file mentions the word docker in a command (any command word, an
#      assignment value such as rt=docker or ${X:-docker}, or a $(command -v
#      docker) lookup). Callers fail closed when V and D meet in one run text or
#      one transitive script set: the unresolved variable may hold docker.
#
# Commands that merely inspect or stop containers (ps, exec, rm, logs, ...)
# emit nothing. A docker word in a comment or a quoted string is data, not an
# invocation, except inside "$(...)" and `...`, and inside the string handed to
# bash -c / sh -c / eval, which are lexed again.
#
# Recognised through the command position: if/elif/while/until/then/do/else/!/{
# keywords; sudo, env, command, exec, nice, nohup, xargs, timeout, time and
# similar wrappers with their flags; VAR=val prefixes; bash|sh|zsh -c STRING;
# eval STRING; any assignment whose value holds the word docker (NAME=docker,
# NAME="docker compose", NAME=(docker compose), NAME="${X:-docker}",
# NAME=$(command -v docker), also behind then/else/do), invoked later as $NAME
# or "${NAME[@]}"; and docker as the command word itself: "${X:-docker}",
# $(which docker), $(command -v docker). POSIX awk only.

BEGIN {
  US = "\037"
  split("! { } if then elif else while until do done fi esac", t, " ")
  for (i in t) KW[t[i]] = 1
  split("sudo doas env exec nice nohup xargs timeout stdbuf setsid ionice time command builtin", t, " ")
  for (i in t) WRAP[t[i]] = 1
  VF["sudo"] = " -u -g -h -p -C -T -r -t -U -D "
  VF["doas"] = " -u -C "
  VF["xargs"] = " -I -n -P -L -d -a -E -s -l "
  VF["env"] = " -u -C -S "
  VF["timeout"] = " -s -k "
  VF["nice"] = " -n "
  VF["ionice"] = " -c -n -p "
  VF["stdbuf"] = " -i -o -e "
  split("export readonly declare typeset local", t, " ")
  for (i in t) EXPORT[t[i]] = 1
  split("echo printf rg grep egrep fgrep test [ [[ which type hash case", t, " ")
  for (i in t) HARMLESS[t[i]] = 1
  split("ps images info version inspect logs exec stop kill rm rmi start restart wait cp stats top port network volume system login logout tag push save load events diff history commit rename pause unpause update attach export import search manifest context config secret swarm node service stack trust plugin builder help", t, " ")
  for (i in t) NOPULL[t[i]] = 1
  GFV = " --config --context -c --host -H --log-level -l --tlscacert --tlscert --tlskey "
  CFV = " -f --file -p --project-name --profile --env-file --project-directory --ansi --parallel --progress --context -c --host -H "
  split("ps logs down stop kill rm exec port top config version ls images events cp pause unpause restart start wait", t, " ")
  for (i in t) CNOPULL[t[i]] = 1
  # DW: docker or docker-compose as a whole word (a path prefix is fine).
  DW = "(^|[^A-Za-z0-9_.$-])docker(-compose)?([^A-Za-z0-9_./-]|$)"
  DOCKER_ID = "docker(-compose)?"
}

FNR == 1 { if (NR > 1) finish_file(); fname = FILENAME; text = "" }
{ text = text $0 "\n" }
END { if (text != "") finish_file() }

function finish_file(   c, nm, kind) {
  N = split(squeeze(text), A, "")
  P = 1
  split("", ASSIGN); split("", DOCKNAME); split("", LITNAME); split("", UNKN)
  VC_n = 0; FILE_D = 0
  HD_n = 0
  parse_list("")
  # A variable invoked as a command: a docker value assigned anywhere in the file
  # makes it unclassifiable (U); only literal non-docker assignments clear it.
  for (c = 1; c <= VC_n; c++) {
    nm = VC_nm[c]
    if (nm in DOCKNAME) kind = "U"
    else if ((nm in LITNAME) && !(nm in UNKN)) continue
    else kind = "V"
    print fname "\t" kind "\t" VC_txt[c]
  }
  if (FILE_D) print fname "\tD\t"
  text = ""
}

# squeeze collapses the spaces inside ${{ ... }} so an expression is one word; an
# expression with shell-special characters becomes ${{expr}}.
function squeeze(s,   out, a) {
  out = ""
  while (match(s, /\$\{\{[^}]*\}\}/)) {
    a = substr(s, RSTART, RLENGTH)
    gsub(/[ \t\n]+/, "", a)
    if (a !~ /^\$\{\{[]A-Za-z0-9_.[-]*\}\}$/) a = "${{expr}}"
    out = out substr(s, 1, RSTART - 1) a
    s = substr(s, RSTART + RLENGTH)
  }
  return out s
}

function unq(s,   n, f, l) {
  n = length(s)
  f = substr(s, 1, 1); l = substr(s, n, 1)
  if (n >= 2 && ((f == "\"" && l == "\"") || (f == "'" && l == "'"))) return substr(s, 2, n - 2)
  return s
}

function emit(kind, w, from, to,   i, s) {
  s = ""
  for (i = from; i <= to; i++) s = s (i > from ? US : "") w[i]
  gsub(/[\n\t]/, " ", s)
  print fname "\t" kind "\t" s
}

# lex_string lexes str as a nested script: the chars are appended to A past N.
function lex_string(str,   m, i, base, oldP, oldN, tmp) {
  m = split(str, tmp, "")
  base = N
  for (i = 1; i <= m; i++) A[base + i] = tmp[i]
  oldP = P; oldN = N
  N = base + m; P = base + 1
  parse_list("")
  P = oldP; N = oldN
}

# skip_heredocs consumes the bodies of the heredocs opened on the line just
# ended. A body run by a shell is lexed; any other body is data.
function skip_heredocs(   h, line, body, c) {
  for (h = 1; h <= HD_n; h++) {
    body = ""
    while (P <= N) {
      line = ""
      while (P <= N && A[P] != "\n") { line = line A[P]; P++ }
      P++
      if (HD_dash[h]) sub(/^\t+/, "", line)
      if (line == HD_delim[h]) break
      body = body line "\n"
    }
    if (HD_run[h]) lex_string(body)
  }
  HD_n = 0
}

# norm_word strips ${NAME:-, ${NAME:=, ${NAME+ and quotes so a default value reads
# as a plain word.
function norm_word(s) {
  gsub(/\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=+]/, " ", s)
  gsub(/[}"']/, " ", s)
  return s
}

# has_docker_word is true when s holds the word docker (or docker-compose), also
# as a ${X:-docker} default or a $(... docker) / `... docker` substitution.
function has_docker_word(s) {
  if (index(s, "$(...docker)") > 0 || index(s, "`...docker`") > 0) return 1
  return norm_word(s) ~ DW
}

# docker_alias returns the docker words a value stands for, or "" when the value
# does not hold docker. A command substitution that mentions docker is "docker".
function docker_alias(v,   t) {
  if (index(v, "$(...docker)") > 0 || index(v, "`...docker`") > 0) return "docker"
  t = norm_word(v)
  sub(/^ +/, "", t); sub(/ +$/, "", t)
  if (t ~ DW) return t
  return ""
}

# record_assign stores NAME=val (isarr: val is the text of NAME=(...)). A docker
# value is sticky: a later non-docker assignment (then rt=podman) does not erase
# it. LITNAME / UNKN track whether every assignment is a literal the file can
# prove is not docker: no command substitution, and no $ unless the value (or a
# ${X:-default}) names a script (ends in .sh or .bash). For an array only the first word, the
# command, counts.
function record_assign(name, val, isarr,   al, cls) {
  al = docker_alias(val)
  if (al != "") { ASSIGN[name] = al; DOCKNAME[name] = 1; return }
  if (!(name in DOCKNAME)) ASSIGN[name] = val
  cls = val
  if (isarr) { sub(/^ +/, "", cls); sub(/ .*$/, "", cls) }
  if (index(cls, "`") > 0 || index(cls, "$(") > 0) UNKN[name] = 1
  else if (index(cls, "$") > 0 && cls !~ /\.(sh|bash)[}"]*$/) UNKN[name] = 1
  else LITNAME[name] = 1
}

# assign_record stores NAME=value for a command made only of assignments.
function assign_record(w, from, n,   k, e, name, val) {
  for (k = from; k <= n; k++) {
    e = index(w[k], "=")
    name = substr(w[k], 1, e - 1); sub(/\+$/, "", name)
    val = unq(substr(w[k], e + 1))
    record_assign(name, val)
  }
}

# run_alias runs a docker alias (the words al) with the command's own arguments
# w[from..n]. It returns 1 when the alias starts with docker or docker-compose.
function run_alias(al, w, from, n,   m0, d0, t, d, m, k) {
  m0 = split(al, d0, " ")
  t = unq(d0[1])
  if (m0 > 0 && (t == "docker" || t == "docker-compose" || t ~ /\/docker(-compose)?$/)) {
    d[1] = (t ~ /compose$/) ? "docker-compose" : "docker"
    m = 1
    for (k = 2; k <= m0; k++) d[++m] = d0[k]
    for (k = from; k <= n; k++) d[++m] = w[k]
    docker_cmd(d, m)
    return 1
  }
  return 0
}

# join_words joins w[from..to] with US for a record.
function join_words(w, from, to,   i, s) {
  s = ""
  for (i = from; i <= to; i++) s = s (i > from ? US : "") w[i]
  gsub(/[\n\t]/, " ", s)
  return s
}

# compose_cmd emits C unless the compose subcommand (after its -f/-p/--profile
# flags) neither pulls nor builds, or the command is bare.
function compose_cmd(o, from, m,   j, t) {
  j = from
  while (j <= m) {
    t = unq(o[j])
    if (index(CFV, " " t " ") > 0) { j += 2; continue }
    if (t ~ /^-/) { j++; continue }
    break
  }
  if (j <= m && (unq(o[j]) in CNOPULL)) return
  emit("C", o, 1, m)
}

function docker_cmd(d, m,   j, t, s, o, k, nx) {
  if (d[1] == "docker-compose") { compose_cmd(d, 2, m); return }
  j = 2
  while (j <= m) {
    t = unq(d[j])
    if (index(GFV, " " t " ") > 0) { j += 2; continue }
    if (t ~ /^--(config|context|host|log-level|tlscacert|tlscert|tlskey)=/ || t ~ /^-[cHl]./ || t ~ /^-/) { j++; continue }
    break
  }
  if (j > m) return
  s = unq(d[j])
  o[1] = "docker"
  for (k = j; k <= m; k++) o[k - j + 2] = d[k]
  m = m - j + 2
  nx = unq(o[3])
  if (s == "compose") { compose_cmd(o, 3, m); return }
  if (s == "image") { if (nx == "pull" || nx == "build") emit("O", o, 1, m); return }
  if (s == "container") { if (nx == "run" || nx == "create") emit("O", o, 1, m); return }
  if (s in NOPULL) return
  emit("O", o, 1, m)
}

# end_cmd classifies one simple command (words w[1..n]).
function end_cmd(w, n,   i, s, t, cw, k, d, m, v, nm, all, envw) {
  if (n == 0) return
  for (k = 1; k <= n; k++) if (has_docker_word(w[k])) { DOCK_SEEN++; FILE_D = 1 }
  # Leading keywords (then, else, do, if, !, {, ...) are not part of the command.
  s = 1
  while (s <= n && (unq(w[s]) in KW)) s++
  if (s > n) return
  i = s
  if (unq(w[i]) in EXPORT) { i++; while (i <= n && w[i] ~ /^-/) i++ }
  all = 1
  for (k = i; k <= n; k++) if (w[k] !~ /^[A-Za-z_][A-Za-z0-9_]*\+?=/) { all = 0; break }
  if (all && i <= n) { assign_record(w, i, n); return }
  i = s
  envw = 0
  while (i <= n) {
    t = unq(w[i])
    if (t in KW || t ~ /^[A-Za-z_][A-Za-z0-9_]*\+?=/) { i++; continue }
    # env "${vars[@]}" cmd and env ${x:+NAME=$x} cmd: the expansion holds
    # NAME=value words, not the command.
    if (envw && (t ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*\[[@*]\]\}$/ || t ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*:\+/)) { i++; continue }
    if (t in WRAP) {
      if (t == "env") envw = 1
      if (t == "command" && unq(w[i + 1]) ~ /^-[A-Za-z]*[vV]/) return
      i++
      while (i <= n && unq(w[i]) ~ /^-./) i += (index(VF[t], " " unq(w[i]) " ") > 0) ? 2 : 1
      if (t == "timeout" && i <= n) i++
      continue
    }
    break
  }
  if (i > n) return
  cw = unq(w[i])
  if (cw ~ /(^|\/)(ba|z|da|a|k)?sh$/) {
    for (k = i + 1; k < n; k++) {
      t = unq(w[k])
      if (t == "-o" || t == "+o" || t == "-O" || t == "+O") { k++; continue }
      if (t ~ /^-[A-Za-z]*c[A-Za-z]*$/) { lex_string(unq(w[k + 1])); return }
      if (t !~ /^[-+]/) break
    }
    return
  }
  if (cw == "eval") {
    v = ""
    for (k = i + 1; k <= n; k++) v = v unq(w[k]) " "
    lex_string(v)
    return
  }
  if (cw == "docker" || cw == "docker-compose" || cw ~ /\/docker$/ || cw ~ /\/docker-compose$/) {
    d[1] = (cw ~ /compose$/) ? "docker-compose" : "docker"
    m = 1
    for (k = i + 1; k <= n; k++) d[++m] = w[k]
    docker_cmd(d, m)
    return
  }
  # docker as the command word through a substitution or a default.
  if (index(cw, "$(...docker)") == 1 || index(cw, "`...docker`") == 1) {
    if (!run_alias("docker", w, i + 1, n)) emit("U", w, 1, n)
    return
  }
  if (cw ~ /^\$\{?[A-Za-z_][A-Za-z0-9_]*(\[[@*]\])?\}?$/ || cw ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=+?]/) {
    nm = cw
    sub(/^\$\{?/, "", nm); sub(/[^A-Za-z0-9_].*$/, "", nm)
    if (cw ~ /^\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=]/ && docker_alias(cw) != "") {
      if (!run_alias(docker_alias(cw), w, i + 1, n)) emit("U", w, 1, n)
      return
    }
    if (nm in ASSIGN && run_alias(ASSIGN[nm], w, i + 1, n)) return
    if (tolower(nm) ~ /docker|compose|^dc$|_dc$|^dc_/) { emit("U", w, 1, n); return }
    # Unresolved here: finish_file decides from every assignment in the file.
    VC_n++; VC_nm[VC_n] = nm; VC_txt[VC_n] = join_words(w, 1, n)
    return
  }
  if (cw in HARMLESS) return
  for (k = i + 1; k <= n; k++) {
    t = unq(w[k])
    if (t == "docker" || t == "docker-compose") { emit("U", w, 1, n); return }
  }
}

# parse_list lexes commands until the terminator (")" or a backtick), or EOF.
function parse_list(term,   w, n, cur, inw, c, nx, j, s, q, dash, run, k, nm, dbr, ds) {
  n = 0; cur = ""; inw = 0; dbr = 0
  while (P <= N) {
    # Inside [[ ... ]] the && || ( ) < > words are test operators, not separators.
    if (!dbr && n >= 1 && !inw && w[n] == "[[" && (n == 1 || (unq(w[n - 1]) in KW))) dbr = 1
    if (dbr && n >= 1 && !inw && w[n] == "]]") dbr = 0
    c = A[P]
    if (c == "\\") {
      nx = A[P + 1]
      if (nx == "\n") { P += 2; continue }
      cur = cur c nx; inw = 1; P += 2; continue
    }
    if (c == "'") {
      s = ""; P++
      while (P <= N && A[P] != "'") { s = s A[P]; P++ }
      P++
      cur = cur "'" s "'"; inw = 1; continue
    }
    if (c == "\"") {
      s = ""; P++
      while (P <= N && A[P] != "\"") {
        if (A[P] == "\\") {
          if (A[P + 1] == "\n") { P += 2; continue }
          s = s A[P] A[P + 1]; P += 2; continue
        }
        if (A[P] == "$" && A[P + 1] == "(" && A[P + 2] != "(") { P += 2; ds = DOCK_SEEN; parse_list(")"); s = s (DOCK_SEEN > ds ? "$(...docker)" : "$(...)"); continue }
        if (A[P] == "`") { P++; ds = DOCK_SEEN; parse_list("`"); s = s (DOCK_SEEN > ds ? "`...docker`" : "`...`"); continue }
        s = s A[P]; P++
      }
      P++
      cur = cur "\"" s "\""; inw = 1; continue
    }
    if (c == "$" && A[P + 1] == "(") {
      if (A[P + 2] == "(") {
        j = P + 3
        while (j <= N && !(A[j] == ")" && A[j + 1] == ")")) j++
        P = j + 2; cur = cur "$((...))"; inw = 1; continue
      }
      P += 2; ds = DOCK_SEEN; parse_list(")"); cur = cur (DOCK_SEEN > ds ? "$(...docker)" : "$(...)"); inw = 1; continue
    }
    if (c == "`") {
      P++
      if (term == "`") { if (inw) { n++; w[n] = cur }; end_cmd(w, n); return }
      ds = DOCK_SEEN; parse_list("`"); cur = cur (DOCK_SEEN > ds ? "`...docker`" : "`...`"); inw = 1; continue
    }
    if (c == " " || c == "\t") { if (inw) { n++; w[n] = cur; cur = ""; inw = 0 }; P++; continue }
    if (c == "#" && !inw) {
      while (P <= N && A[P] != "\n") P++
      continue
    }
    if (c == "\n" || c == ";" || (!dbr && (c == "|" || (c == "&" && substr(cur, length(cur)) != ">" && substr(cur, length(cur)) != "<")))) {
      if (inw) { n++; w[n] = cur; cur = ""; inw = 0 }
      end_cmd(w, n); n = 0; dbr = 0
      P++
      if (c == "\n" && HD_n > 0) skip_heredocs()
      continue
    }
    if (c == "(" && !dbr) {
      if (inw && substr(cur, length(cur)) == "=" && cur ~ /^[A-Za-z_][A-Za-z0-9_]*\+?=$/) {
        # NAME=(a b c): record the array as the variable's value.
        nm = substr(cur, 1, length(cur) - 1); sub(/\+$/, "", nm)
        s = ""; P++
        while (P <= N && A[P] != ")") { s = s A[P]; P++ }
        P++
        gsub(/["'\n]/, "", s)
        if (has_docker_word(s)) { DOCK_SEEN++; FILE_D = 1 }
        record_assign(nm, s, 1)
        cur = ""; inw = 0; continue
      }
      if (inw) { n++; w[n] = cur; cur = ""; inw = 0 }
      end_cmd(w, n); n = 0
      P++; continue
    }
    if (c == ")" && (!dbr || term == ")")) {
      if (inw) { n++; w[n] = cur; cur = ""; inw = 0 }
      end_cmd(w, n); n = 0
      P++
      if (term == ")") return
      continue
    }
    if (c == "<" && A[P + 1] == "<") {
      if (A[P + 2] == "<") { cur = cur "<<<"; inw = 1; P += 3; continue }
      j = P + 2; dash = 0
      if (A[j] == "-") { dash = 1; j++ }
      while (A[j] == " " || A[j] == "\t") j++
      q = A[j]; s = ""
      if (q == "'" || q == "\"") { j++; while (j <= N && A[j] != q) { s = s A[j]; j++ } }
      else { while (j <= N && A[j] !~ /[ \t\n;&|()<>]/) { s = s A[j]; j++ } }
      run = 0
      for (k = 1; k <= n; k++) if (unq(w[k]) ~ /(^|\/)((ba|z|da|a|k)?sh|ssh|su)$/) run = 1
      if (inw && unq(cur) ~ /(^|\/)((ba|z|da|a|k)?sh|ssh|su)$/) run = 1
      HD_n++; HD_delim[HD_n] = s; HD_dash[HD_n] = dash; HD_run[HD_n] = run
      cur = cur "<<"; inw = 1; P += 2; continue
    }
    cur = cur c; inw = 1; P++
  }
  if (inw) { n++; w[n] = cur }
  end_cmd(w, n)
}
