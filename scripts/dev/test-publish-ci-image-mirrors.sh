#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
publisher="${repo_root}/scripts/dev/publish-ci-image-mirrors.sh"
mirror_doc="${repo_root}/docs/internal/ci-service-image-mirrors.md"
scratch="$(mktemp -d)"
trap 'rm -r -- "${scratch}"' EXIT

# The approved rows. This table is written out here on purpose: the publisher,
# the stub registry and the operator table are all checked against it, so a
# drift in any one of them fails instead of agreeing with a copy of itself.
# Fields: name | upstream repository and tag | GHCR package | index digest.
approved_rows=(
  'postgres-alpine|mirror.gcr.io/library/postgres:18-alpine|ci-postgres-alpine|sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
  'postgres-bookworm|mirror.gcr.io/library/postgres:18.6-bookworm|ci-postgres-bookworm|sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
  'neo4j-community|mirror.gcr.io/library/neo4j:2026-community|ci-neo4j-community|sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
  'neo4j-query-plan|mirror.gcr.io/library/neo4j|ci-neo4j-query-plan|sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f'
  'golang-alpine|mirror.gcr.io/library/golang:1.26.9-alpine|ci-golang-alpine|sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0'
  'alpine|mirror.gcr.io/library/alpine:3.21|ci-alpine|sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d'
  'buildx-xx|mirror.gcr.io/tonistiigi/xx:1.5.0|ci-buildx-xx|sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f'
  'buildkit|mirror.gcr.io/moby/buildkit:buildx-stable-1|ci-buildkit|sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea'
)
row_count="${#approved_rows[@]}"

cp "${repo_root}/scripts/dev/fixtures/ci-image-mirror-crane-stub.sh" "${scratch}/crane"
chmod +x "${scratch}/crane"

export CRANE_BIN="${scratch}/crane"
export CRANE_CALLS="${scratch}/calls"
export CRANE_STATE="${scratch}"
export CRANE_ROWS="${scratch}/rows.tsv"

row_names=()
row_sources=()
row_targets=()
row_digests=()
row_packages=()
: > "${CRANE_ROWS}"
for row in "${approved_rows[@]}"; do
  IFS='|' read -r name repository package digest <<< "${row}"
  row_names+=("${name}")
  row_sources+=("${repository}@${digest}")
  row_targets+=("ghcr.io/eshu-hq/${package}@${digest}")
  row_digests+=("${digest}")
  row_packages+=("${package}")
  printf '%s\t%s\t%s\t%s\n' "${name}" "${repository}@${digest}" \
    "ghcr.io/eshu-hq/${package}@${digest}" "${digest}" >> "${CRANE_ROWS}"
done

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  if "$@" > "${scratch}/out" 2>&1; then
    fail "unexpected success: $*"
  fi
}

copy_count() {
  rg -c '^cp --no-clobber ' "${CRANE_CALLS}" || printf '0\n'
}

# The operator table must carry exactly one row per approved copy, spelled the
# way the publisher issued it, with the destination digest equal to the source
# digest. The check reads the copies the publisher actually made.
check_documented_copies() {
  local document="$1" command source destination row
  local count=0
  while IFS= read -r command; do
    read -r _ _ source destination <<< "${command}"
    [[ "${source##*@}" == "${destination##*@}" ]] || return 1
    printf -v row "| \`%s\` | \`%s\` | \`%s\` |" "${source}" "${destination}" "${source##*@}"
    rg -Fqx -- "${row}" "${document}" || return 1
    count=$((count + 1))
  done < <(rg '^cp --no-clobber ' "${CRANE_CALLS}" | sort -u)
  [[ "${count}" -eq "${row_count}" ]] || return 1
  [[ "$(rg -c '^\| `mirror\.gcr\.io/' "${document}")" -eq "${row_count}" ]] || return 1
  check_table_is_one_block "${document}"
}

# A blank line ends a Markdown table, so a row after one renders as plain text
# and an operator reads a shorter table than the publisher copies. The header,
# separator and every upstream row must follow each other with no gap, and the
# line after the last row must not be a table row.
check_table_is_one_block() {
  local document="$1" header offset line
  header="$(rg -n -m1 -F '| Upstream index | Eshu GHCR reference | Index digest |' "${document}" | cut -d: -f1)"
  [[ -n "${header}" ]] || return 1
  [[ "$(sed -n "$((header + 1))p" "${document}")" == '| --- | --- | --- |' ]] || return 1
  for ((offset = 2; offset <= row_count + 1; offset++)); do
    line="$(sed -n "$((header + offset))p" "${document}")"
    [[ "${line}" == '| `mirror.gcr.io/'* ]] || return 1
  done
  line="$(sed -n "$((header + row_count + 2))p" "${document}")"
  [[ "${line}" != '|'* ]]
}

for index in "${!row_names[@]}"; do
  [[ "${row_sources[index]##*@}" == "${row_digests[index]}" &&
     "${row_targets[index]##*@}" == "${row_digests[index]}" ]] ||
    fail "approved row ${row_names[index]} does not preserve its digest"
done

expect_failure bash "${publisher}" unknown
[[ ! -e "${CRANE_CALLS}" ]] || fail 'invalid mode called crane'

export GITHUB_REPOSITORY=attacker/fork
export GITHUB_EVENT_NAME=workflow_dispatch
export GITHUB_REF=refs/heads/main
expect_failure bash "${publisher}" publish
[[ ! -e "${CRANE_CALLS}" ]] || fail 'fork context called crane'

export GITHUB_REPOSITORY=eshu-hq/eshu
export GITHUB_REF=refs/heads/feature
expect_failure bash "${publisher}" publish
[[ ! -e "${CRANE_CALLS}" ]] || fail 'feature ref called crane'

export GITHUB_REF=refs/heads/main
export GITHUB_EVENT_NAME=pull_request
expect_failure bash "${publisher}" publish
[[ ! -e "${CRANE_CALLS}" ]] || fail 'PR event called crane'

export GITHUB_EVENT_NAME=workflow_dispatch
export GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export EXPECTED_REVIEWED_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
expect_failure bash "${publisher}" publish
[[ ! -e "${CRANE_CALLS}" ]] || fail 'unreviewed SHA called crane'
export EXPECTED_REVIEWED_SHA="${GITHUB_SHA}"
export CRANE_BAD_SOURCE=true
expect_failure bash "${publisher}" publish
for name in "${row_names[@]}"; do
  [[ ! -e "${scratch}/${name}" ]] || fail "wrong source digest was copied for ${name}"
done
unset CRANE_BAD_SOURCE

bash "${publisher}" publish > "${scratch}/out"
[[ "$(copy_count)" == "${row_count}" ]] || fail "not exactly ${row_count} guarded copies"
check_documented_copies "${mirror_doc}" || fail 'operator image table differs from publisher copies'
for index in "${!row_names[@]}"; do
  rg -Fqx -- "cp --no-clobber ${row_sources[index]} ${row_targets[index]}" "${CRANE_CALLS}" ||
    fail "${row_names[index]} source/destination mismatch"
  # A row whose digest drifted in the operator table must be detected.
  sed "s/${row_digests[index]}/sha256:0000000000000000000000000000000000000000000000000000000000000000/g" \
    "${mirror_doc}" > "${scratch}/drifted-doc.md"
  if check_documented_copies "${scratch}/drifted-doc.md"; then
    fail "seeded operator image-table digest drift was not detected for ${row_names[index]}"
  fi
  # A row missing from the operator table must be detected.
  rg -Fv -- "${row_targets[index]}" "${mirror_doc}" > "${scratch}/missing-doc.md" || true
  if check_documented_copies "${scratch}/missing-doc.md"; then
    fail "seeded missing operator table row was not detected for ${row_names[index]}"
  fi
done
# A blank line inside the table splits it; the rows after it render as text.
for split_after in 1 2 4 "$((row_count - 1))"; do
  header="$(rg -n -m1 -F '| Upstream index |' "${mirror_doc}" | cut -d: -f1)"
  awk -v at="$((header + 1 + split_after))" '{ print } NR == at { print "" }' "${mirror_doc}" \
    > "${scratch}/split-doc.md"
  if check_documented_copies "${scratch}/split-doc.md"; then
    fail "seeded blank line after table row ${split_after} was not detected"
  fi
done
# A surplus upstream row in the operator table must be detected.
{
  cat "${mirror_doc}"
  printf '| `mirror.gcr.io/library/extra@sha256:%064d` | `ghcr.io/eshu-hq/ci-extra@sha256:%064d` | `sha256:%064d` |\n' 1 1 1
} > "${scratch}/surplus-doc.md"
if check_documented_copies "${scratch}/surplus-doc.md"; then
  fail 'seeded surplus operator table row was not detected'
fi
if rg -q '^cp .*ghcr.io/eshu-hq/ci-[^@: ]+:[^@ ]+$' "${CRANE_CALLS}"; then
  fail 'publisher attempted a mutable tag destination'
fi

# Publisher/row drift: a destination package the approved table does not know
# must not be written, whichever row carries it.
for index in "${!row_names[@]}"; do
  package="${row_packages[index]}"
  sed "s/|${package}|/|${package}-drift|/" "${publisher}" > "${scratch}/drifted-publisher.sh"
  if cmp -s "${publisher}" "${scratch}/drifted-publisher.sh"; then
    fail "publisher has no row for package ${package}"
  fi
  rm -f -- "${scratch}/${row_names[index]}"
  expect_failure bash "${scratch}/drifted-publisher.sh" publish
  [[ ! -e "${scratch}/${row_names[index]}" ]] ||
    fail "drifted publisher wrote ${row_names[index]}"
  bash "${publisher}" publish > "${scratch}/out"
  [[ -f "${scratch}/${row_names[index]}" ]] || fail "rerun did not restore ${row_names[index]}"
done

# A malformed row that follows a valid row must stop both modes with a named
# error. It must not inherit the valid row's name, source, target or digest and
# publish or verify that row a second time under the broken row's position.
zero_digest='sha256:0000000000000000000000000000000000000000000000000000000000000000'
malformed_rows=(
  'broken|mirror.gcr.io/library/broken'
  ''
  "|mirror.gcr.io/library/x|ci-x|${zero_digest}"
  'bad-digest|mirror.gcr.io/library/x|ci-x|sha256:abc'
  "extra|mirror.gcr.io/library/x|ci-x|${zero_digest}|surplus"
)
for malformed in "${malformed_rows[@]}"; do
  awk -v bad="${malformed}" '
    { print }
    /^  \047postgres-alpine\|/ && !done { print "  \047" bad "\047"; done = 1 }
  ' "${publisher}" > "${scratch}/malformed-publisher.sh"
  [[ "$(rg -c '^  .postgres-alpine\|' "${scratch}/malformed-publisher.sh")" == 1 ]] ||
    fail 'seeded malformed publisher lost its first row'
  [[ "$(awk '/^readonly mirror_rows=\(/ { inside = 1; next } inside && /^\)/ { exit } inside { n++ } END { print n }' \
    "${scratch}/malformed-publisher.sh")" == "$((row_count + 1))" ]] ||
    fail "seeded malformed row was not inserted: ${malformed}"
  for mode in publish verify-public; do
    before_copies="$(copy_count)"
    expect_failure bash "${scratch}/malformed-publisher.sh" "${mode}"
    rg -Fq -- 'malformed mirror row' "${scratch}/out" ||
      fail "${mode}: malformed row was not reported by name: ${malformed}"
    [[ "$(copy_count)" == "${before_copies}" ]] ||
      fail "${mode}: malformed row caused a copy: ${malformed}"
    # The first row is read once. A second line means the broken row reused it.
    [[ "$(rg -c '^(published|already published|public) postgres-alpine ' "${scratch}/out")" == 1 ]] ||
      fail "${mode}: malformed row reused the previous row: ${malformed}"
  done
done

# After bootstrap, the publisher must reject the temporary branch.
before_copies="$(copy_count)"
export GITHUB_REF=refs/heads/fix/ci-owned-image-mirror-20261009
expect_failure bash "${publisher}" publish
[[ "$(copy_count)" == "${before_copies}" ]] || fail 'bootstrap ref copied existing digests'
export GITHUB_REF=refs/heads/main
bash "${publisher}" publish > "${scratch}/out"
[[ "$(copy_count)" == "${before_copies}" ]] || fail 'same-byte rerun tried to recopy existing digests'

export CRANE_BAD_DEST=true
expect_failure bash "${publisher}" publish
[[ "$(copy_count)" == "${before_copies}" ]] || fail 'wrong-byte existing digest was copied over'
unset CRANE_BAD_DEST

# Each row alone: a wrong existing destination digest stops the publisher and
# names the row; a wrong source digest stops it before any copy.
for index in "${!row_names[@]}"; do
  name="${row_names[index]}"
  before_copies="$(copy_count)"
  export CRANE_BAD_DEST_ROW="${name}"
  expect_failure bash "${publisher}" publish
  rg -Fq -- "${name} existing digest conflict" "${scratch}/out" ||
    fail "wrong destination digest for ${name} was not reported by name"
  [[ "$(copy_count)" == "${before_copies}" ]] || fail "wrong destination digest for ${name} was copied over"
  unset CRANE_BAD_DEST_ROW

  rm -- "${scratch}/${name}"
  export CRANE_BAD_SOURCE_ROW="${name}"
  expect_failure bash "${publisher}" publish
  rg -Fq -- "${name} source digest mismatch" "${scratch}/out" ||
    fail "wrong source digest for ${name} was not reported by name"
  [[ ! -e "${scratch}/${name}" ]] || fail "wrong source digest for ${name} was copied"
  unset CRANE_BAD_SOURCE_ROW
  bash "${publisher}" publish > "${scratch}/out"
  [[ -f "${scratch}/${name}" ]] || fail "rerun did not restore ${name}"
done

# A first run may stop after one digest; the next run must preserve it and resume.
for index in "${!row_names[@]}"; do
  [[ "${index}" -eq 0 ]] || rm -- "${scratch}/${row_names[index]}"
done
export CRANE_COPY_FAIL_ON="${row_names[1]}"
expect_failure bash "${publisher}" publish
unset CRANE_COPY_FAIL_ON
[[ -f "${scratch}/${row_names[0]}" ]] || fail 'partial first run lost the first digest'
for index in "${!row_names[@]}"; do
  [[ "${index}" -eq 0 || ! -e "${scratch}/${row_names[index]}" ]] ||
    fail "partial first run changed ${row_names[index]}"
done
bash "${publisher}" publish > "${scratch}/out"
for name in "${row_names[@]}"; do
  [[ -f "${scratch}/${name}" ]] || fail "partial run did not resume ${name}"
done

# A concurrent publisher can win between the precheck and no-clobber copy.
for name in "${row_names[@]}"; do
  rm -- "${scratch}/${name}"
  export CRANE_RACE_TARGET="${name}"
  bash "${publisher}" publish > "${scratch}/out"
  unset CRANE_RACE_TARGET
  [[ -f "${scratch}/${name}" ]] || fail "race winner not accepted after digest recheck for ${name}"
done

export CRANE_BAD_DEST=true
expect_failure bash "${publisher}" verify-public
unset CRANE_BAD_DEST
for index in "${!row_names[@]}"; do
  export CRANE_BAD_DEST_ROW="${row_names[index]}"
  expect_failure bash "${publisher}" verify-public
  rg -Fq -- "${row_names[index]} public digest mismatch" "${scratch}/out" ||
    fail "public digest mismatch for ${row_names[index]} was not reported by name"
  unset CRANE_BAD_DEST_ROW
done

before_copies="$(copy_count)"
: > "${CRANE_CALLS}"
bash "${publisher}" verify-public > "${scratch}/out"
[[ "$(copy_count)" == 0 ]] || fail 'verification wrote a destination'
[[ "${before_copies}" -ge "${row_count}" ]] || fail 'copy history was lost'
for index in "${!row_names[@]}"; do
  rg -Fqx -- "digest ${row_targets[index]}" "${CRANE_CALLS}" ||
    fail "verification skipped ${row_names[index]}"
done
[[ "$(rg -c '^digest ' "${CRANE_CALLS}")" == "${row_count}" ]] ||
  fail "verification did not read exactly ${row_count} digests"

printf 'PASS: publisher rejects unsafe contexts, validates sources and destinations, and verifies without writing\n'
