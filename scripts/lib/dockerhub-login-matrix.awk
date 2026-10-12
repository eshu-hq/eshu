# dockerhub-login-matrix.awk - static matrix expansion of one step's run text for
# verify-dockerhub-login.sh (#7886).
#
# usage: awk -v job=JOB -v mfile=matrix.tsv -f dockerhub-login-matrix.awk run.txt
#
# mfile holds one fact per line, tab separated: "I JOB k=v;;k=v" (an include
# entry) and "X JOB KEY v1@@v2@@v3" (an axis). The cells are the axis
# combinations, each include entry overlaid on the combinations it does not
# contradict (or kept alone when none match). The run text is printed once per
# distinct cell with every ${{ matrix.KEY }} replaced by that cell's value. A
# job with no static cell, or a key the cell lacks, leaves the expression in
# place, and the caller counts an expression as unresolved.
BEGIN {
  FS = "\t"
  na = 0; ni = 0
  while ((getline line < mfile) > 0) {
    n = split(line, f, "\t")
    if (f[2] != job) continue
    if (f[1] == "X") { na++; an[na] = f[3]; nv[na] = split(f[4], av_tmp, "@@"); for (i = 1; i <= nv[na]; i++) av[na, i] = av_tmp[i] }
    else if (f[1] == "I") { ni++; inc[ni] = f[3] }
  }
  close(mfile)
  nc = 1; cell[1] = ""
  for (a = 1; a <= na; a++) {
    m = 0
    for (c = 1; c <= nc; c++)
      for (i = 1; i <= nv[a]; i++) { m++; nxt[m] = cell[c] (cell[c] == "" ? "" : ";;") an[a] "=" av[a, i] }
    nc = m
    for (c = 1; c <= nc; c++) cell[c] = nxt[c]
  }
  if (na == 0) { nc = 0 }
  for (e = 1; e <= ni; e++) {
    matched = 0
    for (c = 1; c <= nc; c++) {
      if (compatible(cell[c], inc[e])) { cell[c] = overlay(cell[c], inc[e]); matched = 1 }
    }
    if (!matched) { nc++; cell[nc] = inc[e] }
  }
  text = ""
}
{ text = text $0 "\n" }
END {
  if (nc == 0) { printf "%s", text; exit }
  for (c = 1; c <= nc; c++) {
    out = text
    np = split(cell[c], pr, ";;")
    for (p = 1; p <= np; p++) {
      eq = index(pr[p], "=")
      if (eq == 0) continue
      k = substr(pr[p], 1, eq - 1); v = substr(pr[p], eq + 1)
      gsub(/\\/, "\\\\", v); gsub(/&/, "\\\\&", v)
      gsub("\\$\\{\\{[ \t]*matrix\\." k "[ \t]*\\}\\}", v, out)
    }
    if (!(out in seen)) { seen[out] = 1; printf "%s", out }
  }
}
function value_of(s, key,   n, pr, i, eq) {
  n = split(s, pr, ";;")
  for (i = 1; i <= n; i++) { eq = index(pr[i], "="); if (substr(pr[i], 1, eq - 1) == key) return substr(pr[i], eq + 1) }
  return "\001none"
}
function compatible(c, e,   n, pr, i, eq, k, v, cv) {
  n = split(e, pr, ";;")
  for (i = 1; i <= n; i++) {
    eq = index(pr[i], "="); k = substr(pr[i], 1, eq - 1); v = substr(pr[i], eq + 1)
    cv = value_of(c, k)
    if (cv != "\001none" && cv != v) return 0
  }
  return 1
}
function overlay(c, e,   n, pr, i, eq, k, out) {
  out = c
  n = split(e, pr, ";;")
  for (i = 1; i <= n; i++) {
    eq = index(pr[i], "="); k = substr(pr[i], 1, eq - 1)
    if (value_of(c, k) == "\001none") out = out (out == "" ? "" : ";;") pr[i]
  }
  return out
}
