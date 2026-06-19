#!/usr/bin/env bash
#
# run-oci-localtls-identity-proof.sh — prove the OCI image-IDENTITY hop against a
# REAL localhost OCI registry served over TLS, with NO cloud account (issue
# #3080, umbrella #3130).
#
# What this proves that the seeded image-identity proof does not:
#   The OCI-registry collector can actually TALK to an HTTPS registry it trusts
#   via a custom CA, list a tag, fetch the manifest, and emit the image-identity
#   fact carrying the registry-reported sha256 digest. The digest is observed
#   from a live registry, not seeded.
#
# Flow:
#   1. Mint an EPHEMERAL CA + server cert (openssl, at runtime — never committed).
#   2. Run a local `registry:2` over TLS using that cert (orbstack docker).
#   3. Build the synthetic demo image and push it to the local TLS registry.
#   4. Run the env-gated Go proof (TestLiveLocalTLSRegistryImageIdentity), which
#      drives the real OCI collector Source against the registry with the custom
#      CA and asserts an image-identity manifest fact + digest.
#
# Everything is synthetic/local: the registry is 127.0.0.1, the image is the
# demo's own synthetic app, and all key material is generated into a temp dir
# that is removed on exit. No secrets, no private keys, and no real registry are
# committed or contacted.
#
# Requirements: docker (orbstack), openssl, a Go toolchain, and (recommended)
# oras (https://oras.land). oras pushes the image while trusting the ephemeral
# CA directly, so a clean docker daemon does not need the CA installed in its
# per-host trust store. Without oras the script falls back to `docker push`,
# which only works when the daemon already trusts the CA. No network egress
# beyond pulling registry:2 / the node base image.
#
# Usage:
#   ESHU_SRC=/path/to/eshu ./run-oci-localtls-identity-proof.sh [--keep]
#
set -euo pipefail

ESHU_SRC="${ESHU_SRC:-$(cd "$(dirname "$0")/../../.." && pwd)}"
DEMO_DIR="${ESHU_SRC}/examples/supply-chain-demo"
WORKDIR="$(mktemp -d)"
CERT_DIR="${WORKDIR}/certs"
REGISTRY_NAME="${REGISTRY_NAME:-eshu-localtls-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5443}"
REGISTRY_HOST="127.0.0.1:${REGISTRY_PORT}"
REPOSITORY="library/vuln-demo-app"
REFERENCE="1.0.0"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

log() { printf '\n=== %s ===\n' "$*"; }

cleanup() {
  docker rm -f "${REGISTRY_NAME}" >/dev/null 2>&1 || true
  if [ "${KEEP}" -eq 1 ]; then
    echo "Leaving cert dir ${CERT_DIR} (--keep)."
  else
    rm -rf "${WORKDIR}"
  fi
}
trap cleanup EXIT

for tool in docker openssl go; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "FAIL: ${tool} is required" >&2; exit 1; }
done

log "1. Mint ephemeral CA + server cert for ${REGISTRY_HOST} (runtime only)"
mkdir -p "${CERT_DIR}"
# Single self-signed cert acting as its own CA, valid for localhost + 127.0.0.1.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -nodes -keyout "${CERT_DIR}/registry.key" -out "${CERT_DIR}/registry.crt" \
  -days 1 -subj "/CN=eshu-localtls-registry" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1
cp "${CERT_DIR}/registry.crt" "${CERT_DIR}/ca.pem"
chmod 600 "${CERT_DIR}/registry.key"

log "2. Run registry:2 over TLS on ${REGISTRY_HOST}"
docker rm -f "${REGISTRY_NAME}" >/dev/null 2>&1 || true
docker run -d --name "${REGISTRY_NAME}" \
  -p "${REGISTRY_PORT}:5000" \
  -v "${CERT_DIR}:/certs:ro" \
  -e REGISTRY_HTTP_ADDR=0.0.0.0:5000 \
  -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/registry.crt \
  -e REGISTRY_HTTP_TLS_KEY=/certs/registry.key \
  registry:2 >/dev/null

log "3. Wait for the registry to answer /v2/ over TLS"
ready=0
for i in $(seq 1 30); do
  if curl -sf --cacert "${CERT_DIR}/ca.pem" "https://${REGISTRY_HOST}/v2/" >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep 1
done
[ "${ready}" -eq 1 ] || { echo "FAIL: registry did not become ready" >&2; docker logs "${REGISTRY_NAME}" >&2 || true; exit 1; }

log "4. Build the synthetic demo image and push it to the local TLS registry"
IMAGE="${REGISTRY_HOST}/${REPOSITORY}:${REFERENCE}"
# The docker daemon only trusts custom registry CAs installed under its
# per-host cert path (for example /etc/docker/certs.d/<host>/ca.crt), which the
# ephemeral CA minted above is not. So `docker push` to the TLS registry fails
# with an x509 error on a clean daemon. The registry-population step must not
# depend on daemon trust, so prefer a CA-aware client (`oras`, which takes the
# CA directly) and fall back to `docker push` only when the daemon already
# trusts the CA. The collector proof in step 5 trusts the CA itself and never
# needs the docker daemon to trust anything.
pushed=0
if command -v oras >/dev/null 2>&1; then
  echo "pushing with oras using the ephemeral CA (no daemon trust required)"
  OCI_LAYOUT_DIR="${WORKDIR}/oci-layout"
  mkdir -p "${OCI_LAYOUT_DIR}"
  # buildx can emit an OCI image layout directly, so the image never has to flow
  # through the daemon's registry client to be pushed.
  if docker buildx build -f "${DEMO_DIR}/Dockerfile" \
      --output "type=oci,dest=${WORKDIR}/image-oci.tar" "${DEMO_DIR}" >/dev/null 2>&1; then
    tar -xf "${WORKDIR}/image-oci.tar" -C "${OCI_LAYOUT_DIR}"
    if oras cp --from-oci-layout --to-ca-file "${CERT_DIR}/ca.pem" \
        "${OCI_LAYOUT_DIR}:${REFERENCE}" "${IMAGE}" >/dev/null 2>&1; then
      pushed=1
    fi
  fi
fi
if [ "${pushed}" -eq 0 ]; then
  echo "oras unavailable or failed; trying docker push (needs daemon CA trust)"
  docker build -f "${DEMO_DIR}/Dockerfile" -t "${IMAGE}" "${DEMO_DIR}" >/dev/null
  if docker push "${IMAGE}" >/dev/null 2>&1; then
    pushed=1
  fi
fi
if [ "${pushed}" -eq 0 ]; then
  echo "FAIL: could not push the demo image to the local TLS registry." >&2
  echo "      Install 'oras' (https://oras.land) so the push can trust the" >&2
  echo "      ephemeral CA directly, or install ${CERT_DIR}/ca.pem under your" >&2
  echo "      docker engine's per-host cert path and re-run. The collector proof" >&2
  echo "      below does NOT need the docker daemon to trust the CA." >&2
  exit 1
fi
# Read the registry-served digest back over TLS so the printed digest is the
# one the collector will observe, independent of how the image was pushed.
PUSHED_DIGEST="$(curl -sf --cacert "${CERT_DIR}/ca.pem" -o /dev/null -D - \
  -H "Accept: application/vnd.oci.image.index.v1+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json,application/vnd.docker.distribution.manifest.list.v2+json" \
  "https://${REGISTRY_HOST}/v2/${REPOSITORY}/manifests/${REFERENCE}" 2>/dev/null \
  | awk -F': ' 'tolower($1)=="docker-content-digest"{gsub(/\r/,"",$2); print $2}')"
echo "pushed: ${IMAGE} (${PUSHED_DIGEST:-digest unknown})"

log "5. Run the OCI collector against the live TLS registry (custom-CA trust)"
(
  cd "${ESHU_SRC}/go"
  ESHU_OCI_LOCALTLS_LIVE=1 \
  ESHU_OCI_LOCALTLS_URL="https://${REGISTRY_HOST}" \
  ESHU_OCI_LOCALTLS_REPOSITORY="${REPOSITORY}" \
  ESHU_OCI_LOCALTLS_REFERENCE="${REFERENCE}" \
  ESHU_OCI_LOCALTLS_CA_CERT_PATH="${CERT_DIR}/ca.pem" \
  go test ./internal/collector/ociregistry/ociruntime/ \
    -run TestLiveLocalTLSRegistryImageIdentity -count=1 -v
)

echo
echo "PASS: the OCI-registry collector trusted a custom CA, scanned a live"
echo "      localhost TLS registry:2, and emitted the image-identity fact with"
echo "      the registry-observed sha256 digest. No cloud account, no seeding."
