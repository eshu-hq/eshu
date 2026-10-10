# dockerhub-login-vconf.awk - settles the variable and argument records of one
# step for verify-dockerhub-login.sh (#7886). One input line per fact, tab
# separated; the first field is the kind:
#   A SRC NAME         NAME is assigned a docker value in SRC (the run text, a
#                      reached script, or "env" for the step, job or workflow env)
#   X SRC NAME         NAME is assigned an unresolvable value in SRC, which names
#                      docker
#   S SRC NAME         SRC assigns NAME at all (any value)
#   E env NAME         NAME is set from an unresolved ${{ }} expression in the env
#   F NAME             a reached script defines function NAME
#   P SRC FN K         in SRC, function FN (- for the top level) invokes a
#                      variable assigned from positional parameter K (A: any)
#   V SRC D WORDS      SRC invokes a variable as a command; D is 1 when SRC itself
#                      names docker
#   B SRC CMD K WORDS  SRC passes a bare docker as argument K of function CMD
#   BS SRC FILE K WORDS  the same for a script CMD resolved to FILE ("?" when it
#                      cannot be resolved)
# It prints "SRC: WORDS" for the first conflict, and nothing when none:
#   - a V whose variable SRC itself assigns is settled by SRC's own assignments
#     (an A or X of SRC); a V whose variable SRC never assigns takes the value of
#     an assignment anywhere in the reached scripts or env: an A or X, or an E
#     value when SRC names docker, may hold docker;
#   - a B whose callee invokes that argument (a P record), or whose callee is not
#     defined in the reached scripts, may run docker.
# A V with no assignment anywhere is a value from outside the repository.
BEGIN { FS = "\t" }
$1 == "A" { a[$3] = 1; as[$2 SUBSEP $3] = 1; next }
$1 == "X" { x[$3] = 1; xs[$2 SUBSEP $3] = 1; next }
$1 == "S" { s[$2 SUBSEP $3] = 1; next }
$1 == "E" { e[$3] = 1; next }
$1 == "F" { f[$2] = 1; next }
$1 == "P" { p[$2 SUBSEP $3 SUBSEP $4] = 1; pf[$3 SUBSEP $4] = 1; next }
$1 == "V" { nv++; vs[nv] = $2; vd[nv] = $3; vt[nv] = $4; next }
$1 == "B" { nb++; bs[nb] = $2; bc[nb] = $3; bk[nb] = $4; bt[nb] = $5; bf[nb] = ""; next }
$1 == "BS" { nb++; bs[nb] = $2; bc[nb] = ""; bf[nb] = $3; bk[nb] = $4; bt[nb] = $5; next }
END {
  for (i = 1; i <= nv; i++) {
    split(vt[i], w, "\037")
    n = w[1]
    gsub(/["{}$]|\[[@*]\]/, "", n)
    sub(/[^A-Za-z0-9_].*$/, "", n)
    if (n == "") continue
    if ((vs[i] SUBSEP n) in s) hit = ((vs[i] SUBSEP n) in as) || ((vs[i] SUBSEP n) in xs)
    else hit = (n in a) || (n in x) || ((n in e) && vd[i] == 1)
    if (hit) { print vs[i] ": " gensub_us(vt[i]); exit }
  }
  for (i = 1; i <= nb; i++) {
    hit = 0
    if (bf[i] != "") {
      if (bf[i] == "?") hit = 1
      else if (((bf[i] SUBSEP "-" SUBSEP bk[i]) in p) || ((bf[i] SUBSEP "-" SUBSEP "A") in p)) hit = 1
    } else if (!(bc[i] in f)) hit = 1
    else if (((bc[i] SUBSEP bk[i]) in pf) || ((bc[i] SUBSEP "A") in pf)) hit = 1
    if (hit) { print bs[i] ": " gensub_us(bt[i]); exit }
  }
}
function gensub_us(s) { gsub(/\037/, " ", s); return s }
