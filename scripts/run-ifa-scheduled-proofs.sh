#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# Run the seven Ifá repository-dependency and one reducer retry live proofs.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
verify_reviewed_source() {
  [[ -z "${GOFLAGS:-}" && -z "${GOWORK:-}" ]] || return 1
  local effective_config proof_status
  effective_config="$(go env GOFLAGS GOWORK)"
  [[ -z "${effective_config}" ]] || return 1
  proof_status="$(git -C "${repo_root}" status --porcelain --untracked-files=all)"
  [[ -z "${proof_status}" ]]
}
verify_reviewed_source
image="ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
primary="eshu-ifa-scheduled-primary-$$"
secondary="eshu-ifa-scheduled-mismatch-$$"
events_dir="${RUNNER_TEMP:-/tmp}/ifa-scheduled"
mkdir -p "${events_dir}"
cleanup() {
  docker rm -f "${primary}" "${secondary}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for entry in "${primary}:17474:17687" "${secondary}:18474:18687"; do
  IFS=: read -r name http bolt <<< "${entry}"
  docker run -d --name "${name}" \
    -p "127.0.0.1:${http}:7474" -p "127.0.0.1:${bolt}:7687" \
    -e NORNICDB_NO_AUTH=true -e NORNICDB_DATA_DIR=/data \
    -e NORNICDB_HTTP_PORT=7474 -e NORNICDB_BOLT_PORT=7687 \
    -e NORNICDB_ASYNC_WRITES_ENABLED=false \
    -e NORNICDB_HEIMDALL_ENABLED=false \
    -e NORNICDB_EMBEDDING_ENABLED=false \
    -e NORNICDB_SEARCH_BM25_ENABLED=false \
    -e NORNICDB_SEARCH_VECTOR_ENABLED=false "${image}" >/dev/null
done
for http in 17474 18474; do
  ready=false
  for attempt in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${http}/health" >/dev/null; then
      ready=true
      break
    fi
    sleep 2
  done
  [[ "${ready}" == true ]] || { docker logs "${primary}"; docker logs "${secondary}"; exit 1; }
done

export ESHU_GRAPH_BACKEND=nornicdb
export ESHU_REPLAY_TIER_LIVE=1
export ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1
export ESHU_REPO_DEPENDENCY_PROOF_HTTP_URL=http://127.0.0.1:17474
export ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL=http://127.0.0.1:18474
export ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN='postgres://eshu:change-me@127.0.0.1:15432/eshu_ifa_proof?sslmode=disable'
export ESHU_NEO4J_URI=bolt://127.0.0.1:17687
export NEO4J_URI="${ESHU_NEO4J_URI}"
export ESHU_NEO4J_DATABASE=nornic
export NEO4J_DATABASE="${ESHU_NEO4J_DATABASE}"
export ESHU_NEO4J_USERNAME=neo4j
export NEO4J_USERNAME="${ESHU_NEO4J_USERNAME}"
export ESHU_NEO4J_PASSWORD=change-me
export NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}"

python3 scripts/lib/ifa_scheduled_manifest.py --primary "${primary}" --mismatch "${secondary}" --postgres "${POSTGRES_SERVICE_ID:?}" --output "${events_dir}/manifest.json"

cd go
verify_reviewed_source
go test -json -race -p=1 -count=1 -timeout=6m -tags ifarepodependencyproof ./internal/replay/offlinetier -run '^(TestRepoDependencyIfaCleanupPreservesUnrelatedArtifact|TestRepoDependencyIfaConcurrencyLive|TestRepoDependencyIfaProcessDeathLive|TestRepoDependencyIfaProofCleansAmbiguousCommittedLock|TestRepoDependencyIfaProofRejectsMismatchedHTTPBackend|TestRepoDependencyIfaProofRejectsNonDisposableDatabase|TestRepoDependencyIfaQuarantineLive|TestRepoDependencyProofSchemasShareTrigramLive)$' | tee "${events_dir}/repo.jsonl"
python3 ../scripts/lib/ifa_scheduled_events.py --events "${events_dir}/repo.jsonl" --package github.com/eshu-hq/eshu/go/internal/replay/offlinetier
verify_reviewed_source
go test -json -race -p=1 -count=1 -timeout=6m -tags ifafaultinjection ./cmd/reducer -run '^TestReducerGroupedRetrySeamLiveNornicDB$' | tee "${events_dir}/reducer.jsonl"
python3 ../scripts/lib/ifa_scheduled_events.py --events "${events_dir}/reducer.jsonl" --package github.com/eshu-hq/eshu/go/cmd/reducer
verify_reviewed_source
python3 ../scripts/lib/ifa_scheduled_bundle.py --directory "${events_dir}"
verify_reviewed_source
