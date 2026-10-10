#!/usr/bin/env bash
# Exercise validation and anonymous pull through the real entry point.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir "${work}/bin"
cat > "${work}/bin/skopeo" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${MOCK_IMAGE_CALLS:?}"
# Match Skopeo's Docker transport: a tag cannot accompany a digest.
for argument in "$@"; do
  if [[ "${argument}" == docker://*@sha256:* ]]; then
    repository="${argument%@*}"
    [[ "${repository##*/}" != *:* ]] || exit 65
  fi
done
case "$1" in
  inspect) printf '{}\n' ;;
  manifest-digest) printf '%s\n' "${MOCK_DIGEST:?}" ;;
  copy)
    [[ "${MOCK_COPY_FAIL:-}" != 1 ]] || exit 7
    args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
      if [[ "${args[i]}" == --digestfile ]]; then
        printf '%s\n' "${MOCK_DIGEST:?}" > "${args[i+1]}"
      fi
    done ;;
esac
MOCK
chmod +x "${work}/bin/skopeo"
export PATH="${work}/bin:${PATH}" MOCK_IMAGE_CALLS="${work}/calls"
spec="${root}/specs/ci-images.v1.json"
jq '{version, images:[.images[] | select(.id == "alpine")]}' "${spec}" > "${work}/valid.json"
MOCK_DIGEST="$(jq -r '.images[0].digest' "${work}/valid.json")"
export MOCK_DIGEST
expect_failure() {
  if bash "${root}/scripts/ci/mirror-images.sh" "$@" > "${work}/output" 2>&1; then
    echo "FAIL: command accepted invalid input: $*" >&2; exit 1
  fi
}
no_registry() {
  [[ ! -e "${MOCK_IMAGE_CALLS}" ]] || { echo 'FAIL: invalid input reached registry' >&2; exit 1; }
}
expect_failure verify "${work}/missing.json"
printf '{"version":1,"images":[]}\n' > "${work}/none.json"
expect_failure publish "${work}/none.json"
for mutation in \
  '.images[0].id="bogus"' \
  '.images[0].destination="ghcr.io/eshu-hq/ci-mirror-postgres"' \
  '.images[0].source=.images[0].destination | .images[0].destination="docker.io/library/alpine:3.21"' \
  '.images[0].source="docker.io/library/ubuntu:24.04"' \
  '.images[0].digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' \
  '.images[0].platforms=["linux/amd64"]' \
  '.images += [.images[0]]'; do
  jq "${mutation}" "${work}/valid.json" > "${work}/invalid.json"
  expect_failure publish "${work}/invalid.json"
  no_registry
done
jq '{version, images:[.images[] | select(.id == "postgres-alpine")]}' "${spec}" > "${work}/old.json"
expect_failure publish "${work}/old.json"
no_registry
printf 'PASS: altered, duplicate, and reused entries fail before registry access\n'
expect_failure publish "${work}/valid.json"
no_registry
printf 'PASS: publish requires explicit destination authfile\n'
printf '{"auths":{"ghcr.io":{"auth":"%s"}}}\n' "$(printf 'other:token' | base64 | tr -d '\n')" > "${work}/wrong-auth.json"
CI_IMAGE_DEST_AUTHFILE="${work}/wrong-auth.json" expect_failure publish "${work}/valid.json"
no_registry
printf 'PASS: wrong destination actor fails before registry access\n'
printf '{"auths":{"ghcr.io":{"auth":"%s"}}}\n' "$(printf 'linuxdynasty:token' | base64 | tr -d '\n')" > "${work}/auth.json"
CI_IMAGE_DEST_AUTHFILE="${work}/auth.json" MOCK_COPY_FAIL=1 expect_failure publish "${work}/valid.json"
[[ "$(rg -c '^copy ' "${MOCK_IMAGE_CALLS}")" == 1 ]] || { echo 'FAIL: expected authenticated copy' >&2; exit 1; }
rg -q -- '--dest-authfile' "${MOCK_IMAGE_CALLS}"
printf 'PASS: authenticated publish reaches copy and reports transport failure\n'
: > "${MOCK_IMAGE_CALLS}"
CI_IMAGE_DEST_AUTHFILE="${work}/auth.json" \
  bash "${root}/scripts/ci/mirror-images.sh" publish "${work}/valid.json" > "${work}/output" 2>&1
rg -q 'COPIED:' "${work}/output"
if rg -q 'PASS:' "${work}/output"; then echo 'FAIL: private publish claimed verification' >&2; exit 1; fi
[[ "$(rg -c '^copy ' "${MOCK_IMAGE_CALLS}")" == 1 ]] || { echo 'FAIL: publish ran anonymous pull' >&2; exit 1; }
printf 'PASS: publication does not claim anonymous verification\n'
: > "${MOCK_IMAGE_CALLS}"
bash "${root}/scripts/ci/mirror-images.sh" verify "${work}/valid.json" > "${work}/output" 2>&1
rg -q 'PASS: anonymous full image' "${work}/output"
rg -q -- '--all --preserve-digests' "${MOCK_IMAGE_CALLS}"
rg -q -- '--src-authfile' "${MOCK_IMAGE_CALLS}"
printf 'PASS: verify pulls all platforms anonymously\n'
: > "${MOCK_IMAGE_CALLS}"
MOCK_COPY_FAIL=1 expect_failure verify "${work}/valid.json"
if rg -q 'PASS:' "${work}/output"; then echo 'FAIL: denied blob reported success' >&2; exit 1; fi
printf 'PASS: denied child or layer blocks verification\n'
