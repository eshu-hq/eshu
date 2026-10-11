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
    printf '%s\n' "${FAKE_DOCKER_RENDER:-{\"services\":{\"postgres\":{\"image\":\"postgres:18-alpine\"}}}}"
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
cp scripts/lib/ci_image_compose.py "${test_dir}/helper/scripts/lib/ci_image_compose.py"
cp specs/ci-images.v1.json "${test_dir}/helper/specs/ci-images.v1.json"
cp scripts/lib/ci_image_compose_signature.py "${test_dir}/helper/scripts/lib/ci_image_compose_signature.py"
signature_before="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
printf '\n' >>"${test_dir}/helper/specs/ci-images.v1.json"
signature_after="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
[[ "${signature_before}" != "${signature_after}" ]] || {
    echo "helper ledger changes must invalidate an external stack overlay" >&2
    exit 1
}
signature_before="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
printf '\n# changed helper\n' >>"${test_dir}/helper/scripts/lib/ci_image_compose_signature.py"
signature_after="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
[[ "${signature_before}" != "${signature_after}" ]] || {
    echo "signature helper mutation must invalidate prepared overlay" >&2
    exit 1
}
signature_before="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
printf '\n# changed generator\n' >>"${test_dir}/helper/scripts/lib/ci_image_compose.py"
signature_after="$(bash -c 'source "$1"; eshu_ci_compose_signature "$2" -f docker-compose.yaml' _ "${test_dir}/helper/scripts/lib/ci-image-compose.sh" "${test_dir}/external-stack")"
[[ "${signature_before}" != "${signature_after}" ]] || {
    echo "overlay generator mutation must invalidate prepared overlay" >&2
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
[[ "$(stat -c %a "${ESHU_CI_COMPOSE_OVERLAY}" 2>/dev/null || stat -f %Lp "${ESHU_CI_COMPOSE_OVERLAY}")" == "600" ]]
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
mkdir -p "${test_dir}/auth/scripts/lib" "${test_dir}/auth/go"
cp scripts/run-auth-mcp-e2e.sh "${test_dir}/auth/scripts/run-auth-mcp-e2e.sh"
cp scripts/lib/auth_e2e_cli.sh "${test_dir}/auth/scripts/lib/auth_e2e_cli.sh"
cp scripts/lib/ci-image-compose.sh "${test_dir}/auth/scripts/lib/ci-image-compose.sh"
cat >"${test_dir}/bin/go" <<'SH'
#!/usr/bin/env bash
printf 'called\n' >>"${MOCK_GO_CALLS:?}"
exit 19
SH
printf '#!/usr/bin/env bash\nexit 0\n' >"${test_dir}/bin/node"
chmod +x "${test_dir}/bin/go" "${test_dir}/bin/node"
if MOCK_GO_CALLS="${test_dir}/go-calls" FAKE_DOCKER_CALLS="${test_dir}/auth-docker-calls" \
    bash "${test_dir}/auth/scripts/run-auth-mcp-e2e.sh" --module catalog-sweep >/dev/null 2>&1; then
    echo "failed catalog policy unexpectedly succeeded" >&2
    exit 1
fi
[[ -s "${test_dir}/go-calls" && ! -e "${test_dir}/auth-docker-calls" ]] || {
    echo "catalog policy failure must precede CI overlay preparation" >&2
    exit 1
}
# The wrapper is the trusted unit of the Docker Hub gate: it either runs a stack
# whose every image is covered by the ledger, or it exits 1 before a pulling call.
: >"${test_dir}/calls"
rm -f "${test_dir}/renders"
unset ESHU_CI_COMPOSE_OVERLAY ESHU_CI_COMPOSE_SIGNATURE
uncovered='{"services":{"db":{"image":"redis:7"}}}'
if FAKE_DOCKER_RENDER="${uncovered}" bash -c 'source scripts/lib/ci-image-compose.sh; eshu_ci_compose_prepare "$PWD" -f docker-compose.yaml' >/dev/null 2>&1; then
    echo "an uncovered Docker Hub image must fail the overlay render" >&2
    exit 1
fi
if FAKE_DOCKER_RENDER="${uncovered}" bash scripts/ci/compose.sh -f docker-compose.yaml -- up -d >/dev/null 2>&1; then
    echo "the wrapper must exit 1 for an uncovered Docker Hub image" >&2
    exit 1
fi
if rg --quiet -e ' up -d' "${test_dir}/calls"; then
    echo "the wrapper must not run up for an uncovered Docker Hub image" >&2
    exit 1
fi
: >"${test_dir}/calls"
if GITHUB_ACTIONS=true ESHU_CI_IMAGES=0 bash scripts/ci/compose.sh -f docker-compose.yaml -- up -d >/dev/null 2>&1; then
    echo "the wrapper must refuse to run in CI without CI image mode" >&2
    exit 1
fi
[[ ! -s "${test_dir}/calls" ]] || {
    echo "the CI guard must stop the wrapper before any docker call" >&2
    exit 1
}
eshu_ci_compose_prepare "${repo_root}" -f docker-compose.yaml
if bash scripts/ci/compose.sh -f docker-compose.yaml -f "${test_dir}/external-stack/extra.yml" -- up -d >/dev/null 2>&1; then
    echo "an extra -f file after prepare must invalidate the prepared overlay" >&2
    exit 1
fi
eshu_ci_compose_cleanup
# The signature hashes the value of every variable a root Compose file names, so
# a prepared overlay refuses a call after any such export. This is the contract
# that makes a script-level prepare unsafe in a script that exports later.
unset ESHU_CI_COMPOSE_OVERLAY ESHU_CI_COMPOSE_SIGNATURE
: >"${test_dir}/calls"
if (
    eshu_ci_compose_prepare "${repo_root}" -f docker-compose.yaml &&
    export ESHU_HOME="${test_dir}/home" &&
    bash scripts/ci/compose.sh -f docker-compose.yaml -- up -d
) >/dev/null 2>&1; then
    echo "an export of a Compose-referenced variable after prepare must invalidate the overlay" >&2
    exit 1
fi
eshu_ci_compose_cleanup
# The golden gate's order is: lock, preflight, exports, a call behind a variable
# prefix. Run the gate's own libraries in that order with the real wrapper, in a
# script of its own so that set -e is real (a || list would switch it off).
unset ESHU_CI_COMPOSE_OVERLAY ESHU_CI_COMPOSE_SIGNATURE
: >"${test_dir}/calls"
cat >"${test_dir}/golden-order.sh" <<'SH'
set -euo pipefail
export GITHUB_ACTIONS=true
use_compose=1 graph_service=nornicdb
ESHU_POSTGRES_PORT=15432 ESHU_NEO4J_PASSWORD=change-me ESHU_POSTGRES_PASSWORD=change-me
compose_args=(-p eshu-golden -f docker-compose.yaml)
log() { :; }
die() { printf 'die: %s\n' "$*" >&2; exit 1; }
release_live_gate_lock() { :; }
. "${repo_root}/scripts/lib/golden-corpus-ci-compose.sh"
. "${repo_root}/scripts/lib/golden-corpus-readiness.sh"
host_tcp_port_open() { return 0; }
golden_corpus_preflight_ci_overlay
export ESHU_HOME="${TEST_HOME}" ESHU_API_KEY=golden-key ESHU_EMIT_DATAFLOW=true
export ESHU_FILESYSTEM_ROOT="${TEST_HOME}/corpus" ESHU_REPO_SOURCE_MODE=filesystem
start_golden_corpus_backends
SH
repo_root="${repo_root}" TEST_HOME="${test_dir}/home" bash "${test_dir}/golden-order.sh" \
    >"${test_dir}/golden-order.out" 2>&1 || {
    echo "the golden gate order (preflight, exports, prefixed up) must run through the wrapper" >&2
    cat "${test_dir}/golden-order.out" >&2
    exit 1
}
rg --quiet -e ' up -d nornicdb postgres$' -e ' up -d nornicdb postgres ' "${test_dir}/calls" || {
    echo "the golden gate order must reach the wrapped up call" >&2
    exit 1
}
# The gate shell must hold no prepared overlay: later libraries export Compose
# variables (Ask source, metrics source) after the first call.
if rg --quiet -e 'eshu_ci_compose_prepare' "${repo_root}/scripts/verify-golden-corpus-gate.sh" \
    "${repo_root}"/scripts/lib/golden-corpus-*.sh; then
    echo "the golden gate must not prepare an overlay in its own shell" >&2
    exit 1
fi
echo "test-ci-image-compose: PASS"
