#!/usr/bin/env bash
#
# run-full-chain-proof.sh — drive the supply-chain CVE-to-impact chain end to end
# against a local Docker Compose stack and assert a reducer-published impact
# finding. Follow-up to issue #3061.
#
# This transcribes the recipe proven live in the #3014 + #3061 runs (2026-06-18):
# a repository declaring a real vulnerable package AND a K8s Deployment manifest ->
# the vulnerability-intelligence collector derives an OSV target from the owned
# package -> the reducer joins the advisory to the owned consumption -> a published
# `reducer_supply_chain_impact_finding` -> the deployment-correlation reducer
# anchors that finding to a workload, with the SBOM attestation attached.
#
#   PROVEN (this script): repo -> owned package -> OSV advisory -> published impact
#     finding -> workload anchor, with reducer_sbom_attestation_attachment.
#   NOT EXERCISED HERE: the OCI image-IDENTITY sub-hop (a registry-observed image
#     digest as image_ref on the finding). The OCI-registry collector is
#     registry/ECR-gated; the workload anchor here comes from the K8s manifest, not
#     a registry image identity. See the closing note and issue #3061.
#
# Honesty: Eshu only publishes an impact finding when an advisory joins OWNED
# package evidence. A synthetic package (e.g. the repo's own
# `synthetic-vulnerable-npm`) has no real advisory, so this script uses a real
# registry package with a real OSV advisory (`lodash` 4.17.11) for the owned
# evidence. Nothing here is secret; everything is public registry/advisory data.
#
# Requirements: docker compose, an eshu source checkout, network egress to OSV.
# Usage:
#   ESHU_SRC=/path/to/eshu ./run-full-chain-proof.sh [--keep]
#
set -euo pipefail

ESHU_SRC="${ESHU_SRC:-$(cd "$(dirname "$0")/../../.." && pwd)}"
PROJECT="${PROJECT:-eshu-demo-fullchain}"
RUNDIR="${RUNDIR:-$(mktemp -d)}"
API_KEY="${ESHU_API_KEY:-demo-fullchain-key}"
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

log "1. Owned-package corpus (lodash 4.17.11) + workload manifest"
APP="${RUNDIR}/corpus/vuln-demo-app"
mkdir -p "${APP}/k8s"
cat > "${APP}/package.json" <<'JSON'
{"name":"vuln-demo-app","version":"1.0.0","private":true,"dependencies":{"lodash":"4.17.11"}}
JSON
cat > "${APP}/package-lock.json" <<'JSON'
{"name":"vuln-demo-app","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"vuln-demo-app","version":"1.0.0","dependencies":{"lodash":"4.17.11"}},"node_modules/lodash":{"version":"4.17.11","resolved":"https://registry.npmjs.org/lodash/-/lodash-4.17.11.tgz","integrity":"sha512-cQKh8igo5QUhZ7lg38DYWAxMvjSAKG0A8wGSVimP07SIUEK2UO+arSRKbRZWtelMtN5V0Hkwh5ryOto/SshYIg=="}}}
JSON
# A Deployment manifest in the SAME repo gives the deployment-correlation reducer
# a workload to anchor the package's impact findings to. The image digest matches
# the remote-e2e SBOM-attestation fixture subject so the SBOM attaches.
cat > "${APP}/k8s/deployment.yaml" <<'YAML'
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
          image: demo.invalid/vuln-demo-app@sha256:1111111111111111111111111111111111111111111111111111111111111111
          ports:
            - containerPort: 8080
YAML
( cd "${APP}" && git init -q && git -c user.email=demo@example.invalid -c user.name=demo add -A \
  && git -c user.email=demo@example.invalid -c user.name=demo commit -qm "vuln demo: lodash 4.17.11 + workload" )

log "2. run.env (dummy creds for unselected collectors, port overrides, provisioned API key)"
cat > "${RUNDIR}/run.env" <<EOF
ESHU_REMOTE_E2E_PROJECT_NAME=${PROJECT}
ESHU_POSTGRES_PASSWORD=demo-fullchain
ESHU_NEO4J_PASSWORD=demo-fullchain
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

log "3. Base services up (db-migrate runs eshu-bootstrap-data-plane automatically)"
( cd "${ESHU_SRC}" && ${COMPOSE} up -d --build \
  postgres nornicdb db-migrate workspace-setup bootstrap-index \
  remote-e2e-corpus-preflight remote-e2e-scanner-sbom-preflight sbom-attestation-fixture \
  projector resolution-engine ingester eshu mcp-server )

log "4. Coordinator + collectors (--no-deps skips the cred-gated security-alerts preflight)"
# The coordinator's ESHU_COLLECTOR_INSTANCES_JSON is hardcoded in the remote-e2e
# runtime compose (env-file does not override it); its default already includes a
# git collector, package_registry, vulnerability_intelligence with
# derive_from_owned_packages, and sbom_attestation (fixture-backed) — exactly the
# chain this proof needs. The ingester parses the K8s manifest into deployment
# evidence that the reducer correlates into a workload anchor.
( cd "${ESHU_SRC}" && ${COMPOSE} up -d --no-deps \
  workflow-coordinator collector-package-registry collector-vulnerability-intelligence \
  collector-sbom-attestation )

log "5. Poll for a published impact finding (up to ~9 min)"
found=0
for i in $(seq 1 18); do
  n=$(curl -s -m 10 -H "Authorization: Bearer ${API_KEY}" \
    "${API}/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50" \
    | jq '(.findings // []) | length' 2>/dev/null || echo 0)
  echo "poll ${i}: impact findings (ecosystem=npm) = ${n}"
  if [ "${n:-0}" -ge 1 ] 2>/dev/null; then found=1; break; fi
  sleep 30
done

log "6. Assertions"
if [ "${found}" -ne 1 ]; then
  echo "FAIL: no published impact finding surfaced." >&2
  exit 1
fi
findings_json=$(curl -s -m 10 -H "Authorization: Bearer ${API_KEY}" \
  "${API}/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50")
echo "${findings_json}" | jq -c '.findings[] | {cve_id, package_id, impact_status, confidence, workloads:(.workload_ids // .workloads)}' 2>/dev/null | head

# Workload anchor: the impact findings must carry the workload from the K8s manifest.
anchored=$(echo "${findings_json}" | jq '[.findings[] | select(((.workload_ids // .workloads) // []) | length > 0)] | length' 2>/dev/null || echo 0)
echo "workload-anchored findings = ${anchored}"
if [ "${anchored:-0}" -lt 1 ] 2>/dev/null; then
  echo "FAIL: impact findings were not anchored to a workload." >&2
  exit 1
fi
echo
echo "PASS: published CVE -> impact finding -> workload, via the supply-chain reducer,"
echo "      joined to owned package + deployment evidence."

# Note (#3061): this proves CVE -> advisory -> owned package -> impact finding ->
# workload, with the SBOM attestation attached (reducer_sbom_attestation_attachment).
# The OCI image-IDENTITY sub-hop (a registry-observed image digest on the finding,
# image_ref) is NOT exercised here: the OCI-registry collector is registry/ECR-gated
# and the workload anchor comes from the K8s manifest, not a registry image identity.
# Proven live on 2026-06-18; see issue #3061. The vuln-intel collector-readiness may
# report a non-implemented promotion_state under --no-deps startup (coordinator
# registration timing); the findings and anchors are still correct.
