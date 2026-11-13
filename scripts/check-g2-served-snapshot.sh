#!/usr/bin/env bash
# check-g2-served-snapshot.sh — offline G2 drift gate, Phase 1 (eshu#9669).
# The committed content-search snapshot must match the in-tree served
# surface for every row (route set equality, fragment digest equality,
# pin versions, class vocabulary), and class counts must hit the
# reconciled expects (defaults: 7 = 3 native + 0 tier-C + 4 relay +
# 0 absent). Satellite-side digests re-verify only when
# G2_SATELLITE_REPO points at a satellite checkout holding the pinned
# SHA's objects (read-only plumbing, never fetched); otherwise that
# half prints a deferral note and the in-tree hard gates still bind.
# Usage (run in the worktree root):
#   bash scripts/check-g2-served-snapshot.sh            # gate with defaults/env
#   bash scripts/check-g2-served-snapshot.sh --derive   # print recomputed exports
# --derive recomputes expectations from CUTOVER plus the snapshot classes;
# diff its output against the reviewed wave-move expectation before eval —
# derive-then-eval passes on drifted data by construction, so it is never
# itself proof the gate holds (same rule as check-w8-port-census.sh).
# Env overrides (defaults = reconciled family counts, changed only by wave moves):
#   G2_SNAPSHOT, G2_EXPECT_ROWS, G2_EXPECT_NATIVE, G2_EXPECT_TIERC,
#   G2_EXPECT_RELAY, G2_EXPECT_ABSENT, G2_SATELLITE_REPO.
set -euo pipefail

CUTOVER="go/cmd/api/CUTOVER_OWNERSHIP.md"
SNAPSHOT="${G2_SNAPSHOT:-docs/internal/g2-served-snapshot.md}"
FRAGMENT="go/internal/query/openapi/paths/search/content.go"
MOUNT="go/internal/query/contentread/content_handler.go"
SEARCH_PIN="go/internal/query/content_search_read_admission.go"
POINT_PIN="go/internal/query/content_read_admission.go"

EXPECT_ROWS="${G2_EXPECT_ROWS:-7}"
EXPECT_NATIVE="${G2_EXPECT_NATIVE:-3}"
EXPECT_TIERC="${G2_EXPECT_TIERC:-0}"
EXPECT_RELAY="${G2_EXPECT_RELAY:-4}"
EXPECT_ABSENT="${G2_EXPECT_ABSENT:-0}"

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
  exit 0
fi

fail() { echo "FAIL $1"; exit 1; }

[ "${n_snap}" -eq "${EXPECT_ROWS}" ] || fail "snapshot_rows=${n_snap} want ${EXPECT_ROWS}"
# Route set: snapshot rows must equal the CUTOVER Content-family rows.
cut_content="${tmp_dir}/cut-content.txt"
rg '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+` \| Content \|' "${CUTOVER}" \
  | rg -o '^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+`' | sort >"${cut_content}" || true
[ "$(wc -l <"${cut_content}" | tr -d ' ')" -eq "${EXPECT_ROWS}" ] \
  || fail "cutover_content_rows=$(wc -l <"${cut_content}" | tr -d ' ') want ${EXPECT_ROWS}"
if ! diff "${cut_content}" "${snap_routes}" >"${tmp_dir}/route-diff.txt"; then
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

# In-tree fragment digest must equal every tree: digest cell.
frag_sha=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "${FRAGMENT}")
n_treedigest=$(rg -c "tree:sha256:${frag_sha}" "${SNAPSHOT}" || echo 0)
[ "${n_treedigest}" -eq "${relay_n}" ] \
  || fail "tree_digest_rows=${n_treedigest} want relay=${relay_n} (fragment ${FRAGMENT})"

# Every snapshot route must be Mount-registered and fragmented in-tree.
while IFS= read -r cell; do
  route=$(printf '%s' "${cell}" | rg -o '`[^`]+`' | tr -d '`')
  [ -n "${route}" ] || fail "unparseable snapshot row: ${cell}"
  rg -q -F "HandleFunc(\"${route}\"" "${MOUNT}" \
    || fail "not Mount-registered in-tree: ${route}"
  path="${route#* }"
  rg -q -F "\"${path}\"" "${FRAGMENT}" \
    || fail "not fragmented in-tree: ${route}"
done <"${snap_routes}"

# Pin versions must equal the in-tree contract consts.
search_pin="ContentSearchReadAdmission $(rg -o 'ContentSearchReadAdmissionContractVersion = "[^"]+"' "${SEARCH_PIN}" | rg -o '"[^"]+"' | tr -d '"')"
point_pin="ContentPointReadAdmission $(rg -o 'ContentPointReadAdmissionContractVersion = "[^"]+"' "${POINT_PIN}" | rg -o '"[^"]+"' | tr -d '"')"
rg -q -F "${search_pin}" "${SNAPSHOT}" || fail "snapshot lacks in-tree pin ${search_pin}"
rg -q -F "${point_pin}" "${SNAPSHOT}" || fail "snapshot lacks in-tree pin ${point_pin}"

# Provenance SHAs must be full-length hex.
rg -q '^core_sha: [0-9a-f]{40}$' "${SNAPSHOT}" || fail "core_sha malformed"
sat_sha=$(rg -o '^satellite_sha: [0-9a-f]{40}$' "${SNAPSHOT}" | cut -d' ' -f2)
[ -n "${sat_sha}" ] || fail "satellite_sha malformed"

# Recorded Verification PASS line must match the recomputed counts.
rg -q "PASS g2-served-snapshot: rows=${EXPECT_ROWS} native=${EXPECT_NATIVE} tierc=${EXPECT_TIERC} relay=${EXPECT_RELAY} absent=${EXPECT_ABSENT}" \
  "${SNAPSHOT}" || fail "recorded Verification PASS line diverges from recomputed counts"

# Satellite half: re-verify only when a repo is provided (read-only plumbing).
if [ -n "${G2_SATELLITE_REPO:-}" ]; then
  sat_loc=$(rg -o 'sat_fragment: [^ ]+' "${SNAPSHOT}" | cut -d' ' -f2 | head -1)
  sat_frag="${sat_loc%@*}"
  sat_dir=$(rg -o 'sat_search_dir_listing: [^ ]+' "${SNAPSHOT}" | cut -d' ' -f2 | head -1)
  sat_dir="${sat_dir%@*}"; sat_dir="${sat_dir%/}"
  [ -n "${sat_frag}" ] && [ -n "${sat_dir}" ] || fail "source-digest block malformed"
  have=$(git -C "${G2_SATELLITE_REPO}" rev-parse --verify "${sat_sha}^{commit}" 2>/dev/null || true)
  [ "${have}" = "${sat_sha}" ] || fail "satellite SHA ${sat_sha} not present offline in ${G2_SATELLITE_REPO}"
  want_frag=$(rg -o 'sat_fragment: [^ ]+ sha256:[0-9a-f]+' "${SNAPSHOT}" \
    | rg -o 'sha256:[0-9a-f]+' | cut -d: -f2)
  got_frag=$(git -C "${G2_SATELLITE_REPO}" show "${sat_sha}:${sat_frag}" \
    | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')
  [ "${got_frag}" = "${want_frag}" ] || fail "satellite fragment digest drifted"
  want_list=$(rg -o 'sat_search_dir_listing: [^ ]+ sha256:[0-9a-f]+' "${SNAPSHOT}" \
    | rg -o 'sha256:[0-9a-f]+' | cut -d: -f2)
  got_list=$(git -C "${G2_SATELLITE_REPO}" ls-tree -r --name-only "${sat_sha}" "${sat_dir}/" \
    | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')
  [ "${got_list}" = "${want_list}" ] || fail "satellite search-dir listing drifted"
  sat_note="satellite-side verified at ${sat_sha:0:12}"
else
  sat_note="satellite-side deferred (G2_SATELLITE_REPO unset)"
fi

head_sha=$(git rev-parse HEAD)
snap_core=$(rg -o '^core_sha: [0-9a-f]{40}$' "${SNAPSHOT}" | cut -d' ' -f2)
if [ "${snap_core}" = "${head_sha}" ]; then core_note="core matches HEAD"; else core_note="note: snapshot core=${snap_core:0:12} HEAD=${head_sha:0:12}"; fi

echo "PASS g2-served-snapshot: rows=${EXPECT_ROWS} native=${EXPECT_NATIVE} tierc=${EXPECT_TIERC} relay=${EXPECT_RELAY} absent=${EXPECT_ABSENT} (${sat_note}; ${core_note})"
