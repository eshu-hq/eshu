#!/usr/bin/env bash
#
# run-full-chain-proof.sh — drive the supply-chain CVE-to-impact chain end to end
# against a local Docker Compose stack and assert a reducer-published impact
# finding. Follow-up to issue #3061.
#
# This transcribes the recipe proven live in the #3014 run (2026-06-18): a
# repository declaring a real vulnerable package -> the vulnerability-intelligence
# collector derives an OSV target from that owned package -> the reducer joins the
# advisory to the owned consumption -> a published `reducer_supply_chain_impact_finding`.
#
#   PROVEN HALF (this script): repo -> owned package -> OSV advisory ->
#     published impact finding (collector-readiness: implemented).
#   NOT YET SCRIPTED: image digest -> SBOM subject attachment -> workload. That
#     half needs the OCI-registry and SBOM-attestation collectors plus a workload
#     correlated to the image digest in `../sbom/app.cdx.json`; see the TODO at
#     the end of this file and the issue.
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

log "1. Owned-package corpus (lodash 4.17.11)"
APP="${RUNDIR}/corpus/vuln-demo-app"
mkdir -p "${APP}"
cat > "${APP}/package.json" <<'JSON'
{"name":"vuln-demo-app","version":"1.0.0","private":true,"dependencies":{"lodash":"4.17.11"}}
JSON
cat > "${APP}/package-lock.json" <<'JSON'
{"name":"vuln-demo-app","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"vuln-demo-app","version":"1.0.0","dependencies":{"lodash":"4.17.11"}},"node_modules/lodash":{"version":"4.17.11","resolved":"https://registry.npmjs.org/lodash/-/lodash-4.17.11.tgz","integrity":"sha512-cQKh8igo5QUhZ7lg38DYWAxMvjSAKG0A8wGSVimP07SIUEK2UO+arSRKbRZWtelMtN5V0Hkwh5ryOto/SshYIg=="}}}
JSON
( cd "${APP}" && git init -q && git -c user.email=demo@example.invalid -c user.name=demo add -A \
  && git -c user.email=demo@example.invalid -c user.name=demo commit -qm "vuln demo: lodash 4.17.11" )

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
EOF

log "3. Base services up (db-migrate runs eshu-bootstrap-data-plane automatically)"
( cd "${ESHU_SRC}" && ${COMPOSE} up -d --build \
  postgres nornicdb db-migrate workspace-setup bootstrap-index \
  remote-e2e-corpus-preflight remote-e2e-scanner-sbom-preflight \
  projector resolution-engine ingester eshu mcp-server )

log "4. Coordinator + collectors (--no-deps skips the cred-gated security-alerts preflight)"
# The coordinator's ESHU_COLLECTOR_INSTANCES_JSON is hardcoded in the remote-e2e
# runtime compose (env-file does not override it); its default already includes a
# git collector, package_registry, and vulnerability_intelligence with
# derive_from_owned_packages, which is exactly the chain this proof needs.
( cd "${ESHU_SRC}" && ${COMPOSE} up -d --no-deps \
  workflow-coordinator collector-package-registry collector-vulnerability-intelligence )

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
state=$(curl -s -m 10 -H "Authorization: Bearer ${API_KEY}" "${API}/api/v0/status/collector-readiness" \
  | jq -r '(.. | objects | select((.collector_kind // .family // "") == "vulnerability_intelligence")
            | (.promotion_state // .state // .readiness_state))' 2>/dev/null | head -1)
echo "vulnerability_intelligence promotion_state = ${state:-unknown}"
curl -s -m 10 -H "Authorization: Bearer ${API_KEY}" \
  "${API}/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50" \
  | jq -c '.findings[] | {cve_id, package_id, impact_status, confidence, observed_version}' 2>/dev/null | head
echo
echo "PASS: published CVE -> impact finding via the supply-chain reducer, joined to owned evidence."

# TODO(#3061): script the image -> SBOM-subject -> workload half. Build the demo
# image (../Dockerfile), push it so it has a digest, start the OCI-registry and
# SBOM-attestation collectors with ../sbom/app.cdx.json (subject digest must match
# the image identity), and correlate a workload that runs the image. The image
# half is not yet proven live and is intentionally not asserted here.
