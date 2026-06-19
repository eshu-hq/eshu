#!/usr/bin/env bash
#
# run-image-identity-proof.sh — complete the supply-chain image-IDENTITY sub-hop
# that run-full-chain-proof.sh deliberately skips (issue #3061).
#
# It proves the chain:
#
#   registry image digest (oci_registry.image_manifest)
#     -> SBOM document/component + attestation subject binding
#     -> reducer_container_image_identity (exact_digest, image_ref)
#     -> reducer_sbom_attestation_attachment (subject digest matched)
#     -> reducer_supply_chain_impact_finding with image_ref populated
#
# using the demo's synthetic `synthetic-vulnerable-npm` package and a synthetic
# advisory, with NO real registry and NO real OSV feed.
#
# HONESTY — what is seeded vs collector-derived:
#   SEEDED (tier-1 SQL, examples/supply-chain-demo/scripts/seed-image-identity-facts.sql):
#     oci_registry.image_manifest, sbom.document, sbom.component,
#     attestation.statement, vulnerability.cve, vulnerability.affected_package.
#     These stand in for the OCI-registry, SBOM-attestation, and OSV collectors,
#     which are registry/feed-gated and cannot run offline.
#   COLLECTOR/REDUCER-DERIVED at runtime (NOT seeded):
#     reducer_container_image_identity, reducer_sbom_attestation_attachment,
#     reducer_supply_chain_impact_finding. The image-identity reducer ties the
#     seeded registry digest to the K8s Deployment manifest in the corpus (which
#     references the SAME digest) to classify exact_digest and emit image_ref.
#
# Everything is synthetic/public: registry demo.invalid (RFC 6761 reserved),
# digest sha256:1111…1111, CVE-2026-SYNTHETIC-NPM, GHSA-synthetic-npm-0001.
#
# Requirements: docker compose and an eshu source checkout. NO network egress to
# OSV is needed (the advisory is seeded, not fetched).
#
# Usage:
#   ESHU_SRC=/path/to/eshu ./run-image-identity-proof.sh [--keep]
#
set -euo pipefail

ESHU_SRC="${ESHU_SRC:-$(cd "$(dirname "$0")/../../.." && pwd)}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SEED_SQL="${SCRIPT_DIR}/seed-image-identity-facts.sql"
PROJECT="${PROJECT:-eshu-demo-image-identity}"
RUNDIR="${RUNDIR:-$(mktemp -d)}"
API_KEY="${ESHU_API_KEY:-demo-image-identity-key}"
PG_PASSWORD="${ESHU_POSTGRES_PASSWORD:-demo-image-identity}"
DIGEST="sha256:1111111111111111111111111111111111111111111111111111111111111111"
IMAGE_REF="demo.invalid/vuln-demo-app@${DIGEST}"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

COMPOSE="docker compose --env-file ${RUNDIR}/run.env -p ${PROJECT} -f ${ESHU_SRC}/docker-compose.remote-e2e.yaml"
API="http://localhost:38080"

log() { printf '\n=== %s ===\n' "$*"; }

cleanup() {
  if [ "$KEEP" -eq 1 ]; then
    echo "Leaving stack '${PROJECT}' up (--keep). Tear down with:"
    echo "  ${COMPOSE} down -v --remove-orphans"
    return
  fi
  log "Tearing down ${PROJECT}"
  ${COMPOSE} down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

if [ ! -f "${SEED_SQL}" ]; then
  echo "FAIL: seed SQL not found at ${SEED_SQL}" >&2
  exit 1
fi

log "1. Corpus: K8s Deployment referencing the seeded image digest"
# The image-identity reducer needs a SOURCE image reference (this manifest) to
# pair with the SEEDED registry digest so it can classify exact_digest and emit
# image_ref. The digest here MUST equal the one in seed-image-identity-facts.sql.
APP="${RUNDIR}/corpus/vuln-demo-app"
mkdir -p "${APP}/k8s"
cat > "${APP}/k8s/deployment.yaml" <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vuln-demo-app
  namespace: demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vuln-demo-app
  template:
    metadata:
      labels:
        app: vuln-demo-app
    spec:
      containers:
        - name: app
          image: ${IMAGE_REF}
          ports:
            - containerPort: 8080
YAML
( cd "${APP}" && git init -q \
  && git -c user.email=demo@example.invalid -c user.name=demo add -A \
  && git -c user.email=demo@example.invalid -c user.name=demo commit -qm "vuln demo: k8s deployment referencing seeded image digest" )

log "2. run.env (dummy creds for unselected collectors, port overrides, API key)"
cat > "${RUNDIR}/run.env" <<EOF
ESHU_REMOTE_E2E_PROJECT_NAME=${PROJECT}
ESHU_POSTGRES_PASSWORD=${PG_PASSWORD}
ESHU_NEO4J_PASSWORD=${PG_PASSWORD}
ESHU_API_KEY=${API_KEY}
ESHU_AUTO_GENERATE_API_KEY=false
ESHU_FILESYSTEM_HOST_ROOT=${RUNDIR}/corpus
NVD_API_KEY=
# Dummy values only satisfy parse-time \${VAR:?} for collectors we do NOT start.
ESHU_AWS_E2E_ACCOUNT_ID=000000000000
ESHU_AWS_E2E_REGION=us-east-1
ESHU_AWS_FRESHNESS_TOKEN=dummy
ESHU_AWS_REDACTION_KEY=dummy
ESHU_ECR_OCI_REGION=us-east-1
ESHU_ECR_OCI_REGISTRY_ID=000000000000
ESHU_ECR_OCI_REPOSITORY=dummy
ESHU_SECURITY_ALERT_GITHUB_TOKEN=dummy
ESHU_SECURITY_ALERT_REPOSITORY=owner/repo
ESHU_TFSTATE_REDACTION_KEY=dummy
ESHU_TFSTATE_REDACTION_RULESET_VERSION=v1
ESHU_TFSTATE_S3_BUCKET=dummy
ESHU_TFSTATE_S3_KEY=dummy.tfstate
ESHU_TFSTATE_S3_REGION=us-east-1
ESHU_POSTGRES_PORT=35432
NEO4J_HTTP_PORT=37474
NEO4J_BOLT_PORT=37687
ESHU_HTTP_PORT=38080
ESHU_API_METRICS_PORT=39464
ESHU_MCP_PORT=38081
ESHU_MCP_METRICS_PORT=39468
ESHU_WORKFLOW_COORDINATOR_HTTP_PORT=38082
ESHU_WORKFLOW_COORDINATOR_METRICS_PORT=39469
ESHU_PROJECTOR_HTTP_PORT=38084
ESHU_PROJECTOR_METRICS_PORT=39475
ESHU_RESOLUTION_ENGINE_METRICS_PORT=39466
ESHU_BOOTSTRAP_METRICS_PORT=39467
ESHU_INGESTER_METRICS_PORT=39465
ESHU_COLLECTOR_VULNERABILITY_INTELLIGENCE_METRICS_PORT=39476
ESHU_COLLECTOR_SBOM_ATTESTATION_METRICS_PORT=39477
EOF

log "3. Minimal services up (postgres, nornicdb, db-migrate, bootstrap-index, projector, resolution-engine, ingester, eshu)"
# db-migrate runs eshu-bootstrap-data-plane automatically, creating the schema
# the tier-1 seed writes into. The ingester parses the K8s manifest into the
# source image reference the image-identity reducer needs.
( cd "${ESHU_SRC}" && ${COMPOSE} up -d --build \
  postgres nornicdb db-migrate workspace-setup bootstrap-index \
  projector resolution-engine ingester eshu )

log "4. Apply the tier-1 seed via psql (no real registry, no real OSV)"
${COMPOSE} exec -T -e PGPASSWORD="${PG_PASSWORD}" postgres \
  psql -U eshu -d eshu -v ON_ERROR_STOP=1 -q -f - <"${SEED_SQL}"

log "5. Re-run bootstrap-index to enqueue reducer intents against the seeded scopes"
${COMPOSE} run --rm bootstrap-index >"${RUNDIR}/bootstrap-rerun.log" 2>&1 || {
  echo "bootstrap-index rerun failed; tail:" >&2
  tail -n 40 "${RUNDIR}/bootstrap-rerun.log" >&2
  exit 1
}

log "6. Poll for an impact finding with non-empty image_ref (up to ~9 min)"
found=0
findings_json=""
for i in $(seq 1 18); do
  findings_json=$(curl -s -m 10 -H "Authorization: Bearer ${API_KEY}" \
    "${API}/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50" || echo '{}')
  with_ref=$(echo "${findings_json}" \
    | jq '[.findings[]? | select((.image_ref // "") != "")] | length' 2>/dev/null || echo 0)
  echo "poll ${i}: npm impact findings with image_ref = ${with_ref}"
  if [ "${with_ref:-0}" -ge 1 ] 2>/dev/null; then found=1; break; fi
  sleep 30
done

log "7. Assertions"
if [ "${found}" -ne 1 ]; then
  echo "FAIL: no impact finding carried a non-empty image_ref." >&2
  echo "Last response:" >&2
  echo "${findings_json}" | jq '.' 2>/dev/null >&2 || echo "${findings_json}" >&2
  exit 1
fi

# The image-identity hop: assert image_ref AND subject_digest on the same finding
# equal the seeded image identity. This is the precise sub-hop #3061 adds.
match=$(echo "${findings_json}" | jq -c \
  --arg ref "${IMAGE_REF}" --arg digest "${DIGEST}" \
  '.findings[]? | select(.image_ref == $ref and .subject_digest == $digest)
   | {cve_id, package_id, impact_status, image_ref, subject_digest,
      evidence_path:(.evidence_path // []),
      workloads:(.workload_ids // .workloads // [])}' 2>/dev/null | head -n 1)

if [ -z "${match}" ]; then
  echo "FAIL: no finding matched image_ref=${IMAGE_REF} AND subject_digest=${DIGEST}." >&2
  echo "Findings with an image_ref:" >&2
  echo "${findings_json}" | jq -c '.findings[]? | select((.image_ref // "") != "") | {cve_id, image_ref, subject_digest}' >&2
  exit 1
fi

echo "matched finding: ${match}"

# Evidence path must show the SBOM-component -> attachment -> image-identity hops.
evidence_ok=$(echo "${match}" | jq '[.evidence_path[]? |
  select(. == "sbom.component"
      or . == "reducer_sbom_attestation_attachment"
      or . == "reducer_container_image_identity")] | length' 2>/dev/null || echo 0)
echo "image-path evidence kinds present = ${evidence_ok} (want 3)"
if [ "${evidence_ok:-0}" -lt 3 ] 2>/dev/null; then
  echo "WARN: evidence_path did not surface all three image hops; image_ref + subject_digest still proven."
fi

# Optional: report whether the workload anchor was also included.
workloads=$(echo "${match}" | jq '.workloads | length' 2>/dev/null || echo 0)
echo "workload anchors on the matched finding = ${workloads}"

echo
echo "PASS: seeded registry digest -> SBOM/attestation -> reducer image identity"
echo "      -> impact finding with image_ref=${IMAGE_REF}"
echo "      (subject_digest=${DIGEST}). The OCI image-identity sub-hop is proven."
echo
echo "Honesty: the registry digest, SBOM, attestation, and advisory were SEEDED"
echo "         (no real registry, no real OSV). The image-identity, attachment,"
echo "         and impact findings were reducer-derived at runtime."
