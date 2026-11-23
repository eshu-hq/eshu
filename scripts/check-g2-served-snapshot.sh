#!/usr/bin/env bash
# check-g2-served-snapshot.sh — offline G2 drift gate, full inventory (v2).
# The committed full-inventory snapshot must match the in-tree served
# surface for every row (route set equality, fragment digest equality,
# pin versions, class vocabulary, survivor-set integrity), and class
# counts must hit the reconciled expects (defaults: 282 = 223 native +
# 27 tier-C + 32 relay + 0 absent; 19 of the relay rows are committed
# core-owned survivors, 13 are disclosed blockers). The gate exits 0
# only when relay-nonsurvivor + absent == 0. Satellite-side digests,
# wave coverage, and router registration re-verify only when
# G2_SATELLITE_REPO points at a satellite checkout holding the pinned
# SHA's objects (read-only plumbing, never fetched); otherwise that
# half prints a deferral note and the in-tree hard gates still bind.
# Usage (run in the worktree root):
#   bash scripts/check-g2-served-snapshot.sh            # gate with defaults/env
#   bash scripts/check-g2-served-snapshot.sh --derive   # print recomputed exports
# --derive recomputes expectations from CUTOVER plus the committed sets;
# diff its output against the reviewed expectation before eval —
# derive-then-eval passes on drifted data by construction, so it is never
# itself proof the gate holds (same rule as check-w8-port-census.sh).
# Env overrides (defaults = reconciled full-inventory counts, changed
# only by wave moves):
#   G2_SNAPSHOT, G2_EXPECT_ROWS, G2_EXPECT_NATIVE, G2_EXPECT_TIERC,
#   G2_EXPECT_RELAY, G2_EXPECT_ABSENT, G2_EXPECT_NONSURVIVOR,
#   G2_SATELLITE_REPO.
set -euo pipefail

CUTOVER="docs/internal/g2-route-inventory.md"
SNAPSHOT="${G2_SNAPSHOT:-docs/internal/g2-served-snapshot.md}"

EXPECT_ROWS="${G2_EXPECT_ROWS:-282}"
EXPECT_NATIVE="${G2_EXPECT_NATIVE:-236}"
EXPECT_TIERC="${G2_EXPECT_TIERC:-27}"
EXPECT_RELAY="${G2_EXPECT_RELAY:-19}"
EXPECT_ABSENT="${G2_EXPECT_ABSENT:-0}"
EXPECT_NONSURVIVOR="${G2_EXPECT_NONSURVIVOR:-0}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

snap_routes="${tmp_dir}/snap-routes.txt"
rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "${SNAPSHOT}" | sort >"${snap_routes}"

n_snap=$(wc -l <"${snap_routes}" | tr -d ' ')
# rg -c prints nothing on zero matches (exit 1), so default empties to 0.
native_n=$(rg -c '^\| `[^`]+` \| native-served \|' "${SNAPSHOT}" || echo 0)
tierc_n=$(rg -c '^\| `[^`]+` \| tier-C-dispatch \|' "${SNAPSHOT}" || echo 0)
relay_n=$(rg -c '^\| `[^`]+` \| relay-disclosed \|' "${SNAPSHOT}" || echo 0)
absent_n=$(rg -c '^\| `[^`]+` \| absent-disclosed \|' "${SNAPSHOT}" || echo 0)

if [ "${1:-}" = "--derive" ]; then
  echo "export G2_EXPECT_ROWS=$n_snap"
  echo "export G2_EXPECT_NATIVE=$native_n"
  echo "export G2_EXPECT_TIERC=$tierc_n"
  echo "export G2_EXPECT_RELAY=$relay_n"
  echo "export G2_EXPECT_ABSENT=$absent_n"
  python3 - "${SNAPSHOT}" <<'EOF'
import re, sys
sn = open(sys.argv[1]).read()
relay = set(re.findall(r"^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+` \| relay-disclosed \|", sn, re.M))
surv = set()
m = re.search(r"## Survivor exemption list.*?\n\n(.*?)\n\n## ", sn, re.S)
if m:
    surv = set(re.findall(r"^- `([^`]+)`", m.group(1), re.M))
print(f"export G2_EXPECT_NONSURVIVOR={len(relay - surv)}")
EOF
  exit 0
fi

fail() { echo "FAIL $1"; exit 1; }

[ "${n_snap}" -eq "${EXPECT_ROWS}" ] || fail "snapshot_rows=${n_snap} want ${EXPECT_ROWS}"
# Route set: snapshot rows must equal the CUTOVER universe.
cut_routes="${tmp_dir}/cut-routes.txt"
rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' "${CUTOVER}" | sort >"${cut_routes}" || true
[ "$(wc -l <"${cut_routes}" | tr -d ' ')" -eq "${EXPECT_ROWS}" ] \
  || fail "cutover_rows=$(wc -l <"${cut_routes}" | tr -d ' ') want ${EXPECT_ROWS}"
if ! diff "${cut_routes}" "${snap_routes}" >"${tmp_dir}/route-diff.txt"; then
  echo "FAIL route set mismatch:"; cat "${tmp_dir}/route-diff.txt"; exit 1
fi

# Every table row: 9 pipe fields, route + class + served_by + digest + pin +
# mount + evidence all non-blank (one-position patterns hide hollowed cells).
bad=$(awk -F'|' '/^\| `/ { if (NF!=9) { print NR": NF="NF; next } for (i=2;i<=8;i++) if ($i ~ /^[[:space:]]*$/) { print NR": empty col "i; break } }' "${SNAPSHOT}")
[ -z "${bad}" ] || { echo "FAIL empty cells:"; printf '%s\n' "${bad}"; exit 1; }

# Class vocabulary restricted to the four G2 classes.
n_c5bad=$(rg -o '^\| `[^`]+` \| (native-served|tier-C-dispatch|relay-disclosed|absent-disclosed) \|' \
  "${SNAPSHOT}" | wc -l | tr -d ' ')
[ "${n_c5bad}" -eq "${EXPECT_ROWS}" ] || fail "class_known=${n_c5bad} want ${EXPECT_ROWS}"

# served_by must derive from class.
bad_served=$(awk -F'|' '/^\| `/ {
  c=$3; s=$4;
  gsub(/^[ \t]+|[ \t]+$/, "", c); gsub(/^[ \t]+|[ \t]+$/, "", s);
  if ((c=="native-served" && s!="satellite-native") ||
      (c=="tier-C-dispatch" && s!="tier-C") ||
      (c=="relay-disclosed" && s!="core-relay") ||
      (c=="absent-disclosed" && s!="none-absent")) print NR": "c" -> "s}' "${SNAPSHOT}")
[ -z "${bad_served}" ] || { echo "FAIL served_by mismatch:"; printf '%s\n' "${bad_served}"; exit 1; }

# Recomputed counts from classes.
[ "${native_n}" -eq "${EXPECT_NATIVE}" ] || fail "native=${native_n} want ${EXPECT_NATIVE}"
[ "${tierc_n}" -eq "${EXPECT_TIERC}" ] || fail "tierc=${tierc_n} want ${EXPECT_TIERC}"
[ "${relay_n}" -eq "${EXPECT_RELAY}" ] || fail "relay=${relay_n} want ${EXPECT_RELAY}"
[ "${absent_n}" -eq "${EXPECT_ABSENT}" ] || fail "absent=${absent_n} want ${EXPECT_ABSENT}"

# Survivor integrity + tier-C exactness against the committed sets, and the
# non-survivor blocker list. The gate exits 0 only when no non-survivor
# relay row and no absent row remains.
nonsurv_out="${tmp_dir}/nonsurvivors.txt"
if ! python3 - "${SNAPSHOT}" >"${nonsurv_out}" 2>"${tmp_dir}/set-err.txt" <<'EOF'
import re, sys
sys.path.insert(0, "scripts/lib")
from g2_snapshot_data import SURVIVOR_ROUTES, TIER_C_ROUTES
sn = open(sys.argv[1]).read()
rowclass = {}
for m in re.finditer(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| (\S+) \|", sn, re.M):
    rowclass[f"{m.group(1)} {m.group(2)}"] = m.group(3)
relay = {r for r, c in rowclass.items() if c == "relay-disclosed"}
tierc = {r for r, c in rowclass.items() if c == "tier-C-dispatch"}
surv = set(SURVIVOR_ROUTES)
if not surv <= relay:
    print(f"survivor outside relay: {sorted(surv - relay)}")
    sys.exit(1)
if tierc != set(TIER_C_ROUTES):
    print(f"tier-C drift: extra={sorted(tierc - set(TIER_C_ROUTES))} missing={sorted(set(TIER_C_ROUTES) - tierc)}")
    sys.exit(1)
for r in sorted(relay - surv):
    print(r)
EOF
then
  echo "FAIL survivor/tier-C integrity:"; cat "${tmp_dir}/set-err.txt" "${nonsurv_out}"; exit 1
fi
nonsurv_n=$(wc -l <"${nonsurv_out}" | tr -d ' ')
[ "${nonsurv_n}" -eq "${EXPECT_NONSURVIVOR}" ] \
  || fail "nonsurvivor=${nonsurv_n} want ${EXPECT_NONSURVIVOR}"

# In-tree tree:sha256 digests must equal the recomputed fragment bytes.
if ! python3 - "${SNAPSHOT}" >"${tmp_dir}/digest-err.txt" 2>&1 <<'EOF'
import hashlib, re, sys
from pathlib import Path
sys.path.insert(0, "scripts/lib")
from g2_snapshot_derive import fragment_index
sn = open(sys.argv[1]).read()
frags = fragment_index(".")
bad = []
for m in re.finditer(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| relay-disclosed \| core-relay \| tree:sha256:([0-9a-f]{64}) \|", sn, re.M):
    method, path, want = m.group(1), m.group(2), m.group(3)
    flist = frags.get(path)
    if not flist:
        bad.append(f"{method} {path}: no in-tree fragment")
        continue
    got = hashlib.sha256(Path(flist[0]).read_bytes()).hexdigest()
    if got != want:
        bad.append(f"{method} {path}: digest drift in {flist[0]}")
if bad:
    print("\n".join(bad))
    sys.exit(1)
EOF
then
  echo "FAIL in-tree digest drift:"; cat "${tmp_dir}/digest-err.txt"; exit 1
fi

# Every snapshot route must be Mount-registered in-tree.
while IFS= read -r cell; do
  route=$(printf '%s' "${cell}" | rg -o '`[^`]+`' | tr -d '`')
  [ -n "${route}" ] || fail "unparseable snapshot row: ${cell}"
  if ! rg -q -F --glob '!*_test.go' --glob '*.go' "\"${route}\"" go/internal; then
    fail "not Mount-registered in-tree: ${route}"
  fi
done <"${snap_routes}"

# Pin versions must equal the in-tree contract consts (or `none`).
bad_pin=0
while IFS= read -r pin; do
  pin=$(printf '%s' "${pin}" | sed -e 's/^ *//' -e 's/ *$//')
  [ "${pin}" = "none" ] && continue
  name=${pin% *}; ver=${pin#* }
  case "${name}" in
    *Version) const="${name}" ;;
    *) const="${name}ContractVersion" ;;
  esac
  if ! rg -q -F "${const} = \"${ver}\"" go 2>/dev/null; then
    echo "missing pin const: ${pin}" >&2
    bad_pin=1
  fi
done < <(awk -F'|' '/^\| `(GET|POST|PATCH|PUT|DELETE) / {print $6}' "${SNAPSHOT}")
[ "${bad_pin}" -eq 0 ] || fail "pin const missing in-tree"

# Provenance SHAs must be full-length hex.
rg -q '^core_sha: [0-9a-f]{40}$' "${SNAPSHOT}" || fail "core_sha malformed"
sat_sha=$(rg -o '^satellite_sha: [0-9a-f]{40}$' "${SNAPSHOT}" | cut -d' ' -f2)
[ -n "${sat_sha}" ] || fail "satellite_sha malformed"

# Recorded Verification PASS line must match the recomputed counts.
rg -q "PASS g2-served-snapshot: rows=${EXPECT_ROWS} native=${EXPECT_NATIVE} tierc=${EXPECT_TIERC} relay=${EXPECT_RELAY} absent=${EXPECT_ABSENT} nonsurvivor=${EXPECT_NONSURVIVOR}" \
  "${SNAPSHOT}" || fail "recorded Verification PASS line diverges from recomputed counts"

# Satellite half: re-verify only when a repo is provided (read-only plumbing).
if [ -n "${G2_SATELLITE_REPO:-}" ]; then
  if ! python3 - "${SNAPSHOT}" "${G2_SATELLITE_REPO}" "${sat_sha}" >"${tmp_dir}/sat-err.txt" 2>&1 <<'EOF'
import hashlib, re, subprocess, sys
from pathlib import Path
sys.path.insert(0, "scripts/lib")
from g2_snapshot_data import DIRECT_TERMINATE, WAVE_TERMINATE_OVERRIDE
sn, repo, sha = sys.argv[1], sys.argv[2], sys.argv[3]
def show(path):
    p = subprocess.run(["git", "-C", repo, "show", f"{sha}:{path}"],
                       capture_output=True)
    if p.returncode != 0:
        print(f"satellite blob missing: {path}")
        sys.exit(1)
    return p.stdout
router = show("api/router.go").decode()
bad = []
for m in re.finditer(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| native-served \| satellite-native \| sat:sha256:([0-9a-f]{64}) \|", sn, re.M):
    method, path, want = m.group(1), m.group(2), m.group(3)
    row = m.group(0)
    sm = re.search(r"sat (\S+) .*?router (.*?) viaNative @", row)
    if not sm:
        bad.append(f"{method} {path}: unparseable sat evidence")
        continue
    src = sm.group(1)
    if src in WAVE_TERMINATE_OVERRIDE:
        term = WAVE_TERMINATE_OVERRIDE[src]
    elif src.endswith("_wave.go"):
        term = "api/" + Path(src).name.replace("_wave.go", "_terminate.go")
    elif src in DIRECT_TERMINATE:
        term = DIRECT_TERMINATE[src]
    else:
        bad.append(f"{method} {path}: unknown sat source {src}")
        continue
    got = hashlib.sha256(show(term)).hexdigest()
    if got != want:
        bad.append(f"{method} {path}: satellite digest drift in {term}")
        continue
    # Row must sit in the source's frozen table (waves) or consts (direct).
    body = show(src).decode(errors="replace")
    if path not in body:
        bad.append(f"{method} {path}: not covered by {src}")
        continue
    # Registration line from the evidence must exist verbatim in router.go.
    for rl in sm.group(2).split(" ;; "):
        rl = rl.strip()
        if rl and rl not in router:
            bad.append(f"{method} {path}: registration absent from router.go: {rl[:80]}")
if bad:
    print("\n".join(bad))
    sys.exit(1)
EOF
  then
    echo "FAIL satellite-side drift:"; cat "${tmp_dir}/sat-err.txt"; exit 1
  fi
  sat_note="satellite-side verified at ${sat_sha:0:12}"
else
  sat_note="satellite-side deferred (G2_SATELLITE_REPO unset)"
fi

head_sha=$(git rev-parse HEAD)
snap_core=$(rg -o '^core_sha: [0-9a-f]{40}$' "${SNAPSHOT}" | cut -d' ' -f2)
if [ "${snap_core}" = "${head_sha}" ]; then core_note="core matches HEAD"; else core_note="note: snapshot core=${snap_core:0:12} HEAD=${head_sha:0:12}"; fi

echo "PASS g2-served-snapshot: rows=${EXPECT_ROWS} native=${EXPECT_NATIVE} tierc=${EXPECT_TIERC} relay=${EXPECT_RELAY} absent=${EXPECT_ABSENT} nonsurvivor=${nonsurv_n} (${sat_note}; ${core_note})"
[ "${nonsurv_n}" -eq 0 ] && [ "${absent_n}" -eq 0 ] || {
  echo "GATE RED: ${nonsurv_n} non-survivor relay rows + ${absent_n} absent rows:"
  cat "${nonsurv_out}"
  exit 1
}
