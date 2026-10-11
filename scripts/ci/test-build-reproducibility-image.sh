#!/usr/bin/env bash
# Exercise the build entry point with captured Docker argv and missing pins.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir "${work}/bin"
cat > "${work}/bin/docker" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$@" > "${MOCK_BUILD_ARGS:?}"
exit "${MOCK_BUILD_EXIT:-0}"
MOCK
chmod +x "${work}/bin/docker"
export PATH="${work}/bin:${PATH}" MOCK_BUILD_ARGS="${work}/args"
for name in ESHU_XX_IMAGE ESHU_GO_IMAGE ESHU_ALPINE_IMAGE; do
  value="$(jq -r --arg name "${name}" '.images[] | select(.env == $name) | .destination + "@" + .digest' "${root}/specs/ci-images.v1.json")"
  export "${name}=${value}"
done
bash "${root}/scripts/ci/build-reproducibility-image.sh" "${work}/out" 100
for name in ESHU_XX_IMAGE ESHU_GO_IMAGE ESHU_ALPINE_IMAGE; do
  rg -q -F -- "${name}=${!name}" "${MOCK_BUILD_ARGS}"
done
rg -q -F -- 'SOURCE_DATE_EPOCH=100' "${MOCK_BUILD_ARGS}"
rg -q -F -- 'ESHU_VERSION=reproducibility-test' "${MOCK_BUILD_ARGS}"
rg -q -F -- '--no-cache' "${MOCK_BUILD_ARGS}"
rg -q -F -- '--provenance=false' "${MOCK_BUILD_ARGS}"
printf 'PASS: all three mirrored bases and reproducibility options reach Docker\n'
cp "${MOCK_BUILD_ARGS}" "${work}/before"
if ESHU_GO_IMAGE='' bash "${root}/scripts/ci/build-reproducibility-image.sh" "${work}/out" 100; then
  echo 'FAIL: missing Go mirror accepted' >&2; exit 1
fi
cmp "${work}/before" "${MOCK_BUILD_ARGS}"
if ESHU_GO_IMAGE=golang:latest bash "${root}/scripts/ci/build-reproducibility-image.sh" "${work}/out" 100; then
  echo 'FAIL: unapproved Go mirror accepted' >&2; exit 1
fi
cmp "${work}/before" "${MOCK_BUILD_ARGS}"
printf 'PASS: invalid or missing pins fail before Docker\n'
if MOCK_BUILD_EXIT=9 bash "${root}/scripts/ci/build-reproducibility-image.sh" "${work}/out" 100; then
  echo 'FAIL: build failure was hidden' >&2; exit 1
fi
printf 'PASS: Docker build failure remains blocking\n'
