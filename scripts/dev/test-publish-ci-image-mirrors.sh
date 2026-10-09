#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
publisher="${repo_root}/scripts/dev/publish-ci-image-mirrors.sh"
scratch="$(mktemp -d)"
trap 'rm -r -- "${scratch}"' EXIT

cat > "${scratch}/crane" <<'CRANE'
#!/usr/bin/env bash
set -euo pipefail
command_name="$1"
shift
printf '%s %s\n' "${command_name}" "$*" >> "${CRANE_CALLS}"
case "${command_name}" in
  digest)
    ref="$1"
    case "${ref}" in
      *ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873)
        [[ -f "${CRANE_STATE}/alpine" ]] || exit 1
        digest='sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
        ;;
      *ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c)
        [[ -f "${CRANE_STATE}/bookworm" ]] || exit 1
        digest='sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
        ;;
      *ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f)
        [[ -f "${CRANE_STATE}/neo4j" ]] || exit 1
        digest='sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
        ;;
      *postgres:18-alpine@*)
        digest='sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
        ;;
      *postgres:18.6-bookworm@*)
        digest='sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
        ;;
      *neo4j:2026-community@*)
        digest='sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
        ;;
      *) exit 2 ;;
    esac
    if [[ "${CRANE_BAD_SOURCE:-}" == true && "${ref}" == mirror.gcr.io/* ]]; then
      digest='sha256:0000000000000000000000000000000000000000000000000000000000000000'
    fi
    if [[ "${CRANE_BAD_DEST:-}" == true && "${ref}" == ghcr.io/* ]]; then
      digest='sha256:1111111111111111111111111111111111111111111111111111111111111111'
    fi
    printf '%s\n' "${digest}"
    ;;
  cp)
    [[ "$1" == '--no-clobber' ]] || exit 3
    case "$3" in
      *ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873) name=alpine ;;
      *ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c) name=bookworm ;;
      *ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f) name=neo4j ;;
      *) exit 4 ;;
    esac
    [[ ! -f "${CRANE_STATE}/${name}" ]] || exit 1
    if [[ "${CRANE_COPY_FAIL_ON:-}" == "${name}" ]]; then
      exit 1
    fi
    touch "${CRANE_STATE}/${name}"
    if [[ "${CRANE_RACE_TARGET:-}" == "${name}" ]]; then
      exit 1
    fi
    ;;
  *) exit 5 ;;
esac
CRANE
chmod +x "${scratch}/crane"

export CRANE_BIN="${scratch}/crane"
export CRANE_CALLS="${scratch}/calls"
export CRANE_STATE="${scratch}"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  if "$@" > "${scratch}/out" 2>&1; then
    fail "unexpected success: $*"
  fi
}

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
[[ ! -e "${scratch}/alpine" ]] || fail 'unreviewed SHA was copied'
export EXPECTED_REVIEWED_SHA="${GITHUB_SHA}"
export CRANE_BAD_SOURCE=true
expect_failure bash "${publisher}" publish
[[ ! -e "${scratch}/alpine" ]] || fail 'wrong source digest was copied'
unset CRANE_BAD_SOURCE

bash "${publisher}" publish > "${scratch}/out"
[[ "$(rg -c '^cp --no-clobber ' "${CRANE_CALLS}")" == 3 ]] || fail 'not exactly three guarded copies'
rg -q '^cp --no-clobber mirror.gcr.io/library/postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873 ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873$' "${CRANE_CALLS}" || fail 'Alpine source/destination mismatch'
rg -q '^cp --no-clobber mirror.gcr.io/library/postgres:18.6-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c$' "${CRANE_CALLS}" || fail 'Bookworm source/destination mismatch'
rg -q '^cp --no-clobber mirror.gcr.io/library/neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f$' "${CRANE_CALLS}" || fail 'Neo4j source/destination mismatch'
if rg -q '^cp .*ghcr.io/eshu-hq/ci-[^@: ]+:[^@ ]+$' "${CRANE_CALLS}"; then
  fail 'publisher attempted a mutable tag destination'
fi

# The existing default-branch workflow can dispatch this exact reviewed branch
# to bootstrap the packages before consumers depend on them.
export GITHUB_REF=refs/heads/fix/ci-owned-image-mirror-20261009
bash "${publisher}" publish > "${scratch}/out"
[[ "$(rg -c '^cp --no-clobber ' "${CRANE_CALLS}")" == 3 ]] || fail 'same-byte rerun tried to overwrite existing tags'

export CRANE_BAD_DEST=true
expect_failure bash "${publisher}" publish
[[ "$(rg -c '^cp --no-clobber ' "${CRANE_CALLS}")" == 3 ]] || fail 'wrong-byte existing tag was copied over'
unset CRANE_BAD_DEST

# A first run may stop after one tag; the next run must preserve it and resume.
rm -- "${scratch}/bookworm" "${scratch}/neo4j"
export CRANE_COPY_FAIL_ON=bookworm
expect_failure bash "${publisher}" publish
unset CRANE_COPY_FAIL_ON
[[ -f "${scratch}/alpine" && ! -f "${scratch}/bookworm" && ! -f "${scratch}/neo4j" ]] ||
  fail 'partial first run changed unexpected tags'
bash "${publisher}" publish > "${scratch}/out"
[[ -f "${scratch}/bookworm" && -f "${scratch}/neo4j" ]] || fail 'partial run did not resume'

# A concurrent publisher can win between the precheck and no-clobber copy.
rm -- "${scratch}/neo4j"
export CRANE_RACE_TARGET=neo4j
bash "${publisher}" publish > "${scratch}/out"
unset CRANE_RACE_TARGET
[[ -f "${scratch}/neo4j" ]] || fail 'race winner not accepted after digest recheck'

export CRANE_BAD_DEST=true
expect_failure bash "${publisher}" verify-public
unset CRANE_BAD_DEST

before_copies="$(rg -c '^cp --no-clobber ' "${CRANE_CALLS}")"
bash "${publisher}" verify-public > "${scratch}/out"
[[ "$(rg -c '^cp --no-clobber ' "${CRANE_CALLS}")" == "${before_copies}" ]] || fail 'verification wrote a destination'

printf 'PASS: publisher rejects unsafe contexts, validates sources and destinations, and verifies without writing\n'
