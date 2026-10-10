#!/usr/bin/env bash
# Exercise parent-owned overlay reuse without pulling images or starting Docker.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT
mkdir -p "${test_dir}/bin"
cat >"${test_dir}/bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${FAKE_DOCKER_CALLS}"
if [[ "$*" == *"config --format json" ]]; then
    printf 'render\n' >>"${FAKE_DOCKER_COUNT}"
    if [[ "${FAKE_DOCKER_FAIL_RENDER:-0}" == "1" ]]; then exit 1; fi
    printf '{"services":{"postgres":{"image":"postgres:18-alpine"}}}\n'
fi
SH
chmod +x "${test_dir}/bin/docker"
export PATH="${test_dir}/bin:${PATH}" FAKE_DOCKER_COUNT="${test_dir}/renders" FAKE_DOCKER_CALLS="${test_dir}/calls"
export ESHU_CI_IMAGES=1
while IFS= read -r pin; do
    export "${pin?}"
done < <(python3 -c 'import json; x=json.load(open("specs/ci-images.v1.json")); print("\n".join("{env}={destination}@{digest}".format(**r) for r in x["images"] if r["id"] in {"postgres-alpine", "neo4j-community", "xx", "golang", "alpine"}))')
# shellcheck source=scripts/lib/ci-image-compose.sh
source scripts/lib/ci-image-compose.sh
mkdir -p "${test_dir}/helper/scripts/lib" "${test_dir}/helper/specs" "${test_dir}/external-stack"
cp scripts/lib/ci-image-compose.sh "${test_dir}/helper/scripts/lib/ci-image-compose.sh"
cp specs/ci-images.v1.json "${test_dir}/helper/specs/ci-images.v1.json"
signature_before="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
printf '\n' >>"${test_dir}/helper/specs/ci-images.v1.json"
signature_after="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
[[ "${signature_before}" != "${signature_after}" ]] || {
    echo "helper ledger changes must invalidate an external stack overlay" >&2
    exit 1
}
# shellcheck disable=SC2016  # Literal Compose interpolation syntax.
printf 'services:\n  extra:\n    image: $EXTRA_IMAGE\n' >"${test_dir}/external-stack/extra.yml"
EXTRA_IMAGE=one
export EXTRA_IMAGE
signature_before="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" --file="$2/extra.yml"' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
EXTRA_IMAGE=two
export EXTRA_IMAGE
signature_after="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" --file="$2/extra.yml"' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
[[ "${signature_before}" != "${signature_after}" ]] || {
    echo "unbraced interpolation and equals-form files must invalidate a changed overlay" >&2
    exit 1
}
eshu_ci_compose_prepare "${repo_root}" -f docker-compose.yaml
overlay_path="${ESHU_CI_COMPOSE_OVERLAY}"
[[ "$(stat -f %Lp "${ESHU_CI_COMPOSE_OVERLAY}" 2>/dev/null || stat -c %a "${ESHU_CI_COMPOSE_OVERLAY}")" == "600" ]]
bash scripts/ci/compose.sh -f docker-compose.yaml -- ps
bash scripts/ci/compose.sh -f docker-compose.yaml -- exec -T postgres true
rg --fixed-strings --quiet -- "-f docker-compose.yaml -f ${overlay_path} ps" "${FAKE_DOCKER_CALLS}"
rg --fixed-strings --quiet -- "-f docker-compose.yaml -f ${overlay_path} exec -T postgres true" "${FAKE_DOCKER_CALLS}"
[[ "$(wc -l <"${FAKE_DOCKER_COUNT}")" -eq 1 ]] || {
    echo "prepared overlay should render exactly once" >&2
    exit 1
}
if bash scripts/ci/compose.sh -f docker-compose.neo4j.yml -- ps >/dev/null 2>&1; then
    echo "changed Compose file order must invalidate prepared overlay" >&2
    exit 1
fi
good_go_pin="${ESHU_GO_IMAGE}"
ESHU_GO_IMAGE="ghcr.io/eshu-hq/wrong@sha256:$(printf 'a%.0s' {1..64})"
export ESHU_GO_IMAGE
if bash scripts/ci/compose.sh -f docker-compose.yaml -- ps >/dev/null 2>&1; then
    echo "changed pin must invalidate prepared overlay" >&2
    exit 1
fi
eshu_ci_compose_cleanup
[[ ! -e "${overlay_path}" ]]
ESHU_GO_IMAGE="${good_go_pin}"
export ESHU_GO_IMAGE
cp docker-compose.yaml "${test_dir}/stack.yaml"
eshu_ci_compose_prepare "${repo_root}" -f "${test_dir}/stack.yaml"
printf '\n# changed after prepare\n' >>"${test_dir}/stack.yaml"
if bash scripts/ci/compose.sh -f "${test_dir}/stack.yaml" -- ps >/dev/null 2>&1; then
    echo "changed Compose file contents must invalidate prepared overlay" >&2
    exit 1
fi
eshu_ci_compose_cleanup
if FAKE_DOCKER_FAIL_RENDER=1 bash -c 'source scripts/lib/ci-image-compose.sh; eshu_ci_compose_prepare "$PWD" -f docker-compose.yaml' >/dev/null 2>&1; then
    echo "failed Compose render must stop before a runtime command" >&2
    exit 1
fi
echo "test-ci-image-compose: PASS"
