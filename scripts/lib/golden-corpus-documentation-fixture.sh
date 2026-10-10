#!/usr/bin/env bash
#
# The verifier owns finding/packet payloads. The separate section row keeps
# list_documentation_facts coverage for a collector-style read model; it is
# explicitly not evidence that the verifier emits sections.
# shellcheck disable=SC2154

golden_documentation_scope_id="documentation-source:golden-verifier"

seed_golden_documentation_fixture() {
	local output="${log_dir}/documentation-verifier.json"
	"${bin_dir}/eshu-eshu" docs verify \
		"${repo_root}/testdata/golden/documentation-positive.md" \
		--limit 1 --image-truth local --persist \
		--scope "${golden_documentation_scope_id}" --repo orders-api --json \
		>"${output}" || die "documentation verifier failed"
	jq -e '
		.data.summary.documents_scanned == 1 and
		.data.summary.claims_checked == 1 and
		.data.summary.valid == 1 and
		.data.summary.evidence_packets == 1 and
		.data.persistence.persisted == true and
		.data.persistence.skipped == false and
		.data.findings[0].claim_text == "GET /api/v0/documentation/findings" and
		.data.findings[0].claim_byte_offset == 6 and
		.data.findings[0].claim_byte_length == 34 and
		.data.evidence_packets[0].payload.unified_evidence.provenance.source == "documentation_verifier" and
		.data.evidence_packets[0].payload.unified_evidence.citation.byte_offset == 6 and
		.data.evidence_packets[0].payload.unified_evidence.citation.byte_length == 34
	' "${output}" >/dev/null || die "documentation verifier did not produce the positive claim and citation"

	golden_documentation_generation_id="$(jq -er '.data.persistence.generation_id' "${output}")"
	golden_documentation_finding_id="$(jq -er '.data.findings[0].finding_id' "${output}")"
	golden_documentation_packet_id="$(jq -er '.data.evidence_packets[0].packet_id' "${output}")"
	golden_documentation_packet_version="$(jq -er '.data.evidence_packets[0].packet_version' "${output}")"
	golden_documentation_source_id="$(jq -er '.data.findings[0].source_id' "${output}")"
	golden_documentation_document_id="$(jq -er '.data.findings[0].document_id' "${output}")"
	golden_documentation_section_id="$(jq -er '.data.findings[0].section_id' "${output}")"
	golden_documentation_uri="$(jq -er '.data.evidence_packets[0].payload.document.canonical_uri' "${output}")"
	[[ "${golden_documentation_generation_id}" =~ ^docs-verify-generation:[a-f0-9]+$ ]] || die "invalid documentation generation identity"
	[[ "${golden_documentation_finding_id}" =~ ^finding:[a-f0-9]+$ ]] || die "invalid documentation finding identity"
	[[ "${golden_documentation_packet_id}" =~ ^doc-packet:[a-f0-9]+$ ]] || die "invalid documentation packet identity"
	[[ "${golden_documentation_source_id}" =~ ^doc-source:[a-f0-9]+$ ]] || die "invalid documentation source identity"
	[[ "${golden_documentation_document_id}" =~ ^doc:[a-f0-9]+$ ]] || die "invalid documentation document identity"
	[[ "${golden_documentation_section_id}" =~ ^line:[a-f0-9]+$ ]] || die "invalid documentation section identity"

	local persisted
	persisted="$(pg "
SELECT count(*)
FROM fact_records
WHERE scope_id = '${golden_documentation_scope_id}'
  AND generation_id = '${golden_documentation_generation_id}'
  AND fact_kind IN ('documentation_finding', 'documentation_evidence_packet')
  AND is_tombstone = FALSE;")" || die "cannot read persisted documentation facts"
	[[ "${persisted}" == "2" ]] || die "verifier persisted ${persisted} documentation facts, want 2"
}

golden_documentation_verify_active() {
	local state
	for _ in $(seq 1 30); do
		state="$(pg "
SELECT generation.status || '|' || (scope.active_generation_id = generation.generation_id)::text || '|' ||
  (SELECT count(*) FROM fact_records AS fact
   WHERE fact.scope_id = scope.scope_id AND fact.generation_id = generation.generation_id
     AND fact.fact_kind = 'documentation_evidence_packet'
     AND fact.payload->'unified_evidence'->'provenance'->>'source' = 'documentation_verifier'
     AND fact.payload->'unified_evidence'->'citation'->>'byte_length' = '34')::text
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.scope_id = scope.scope_id
WHERE scope.scope_id = '${golden_documentation_scope_id}'
  AND generation.generation_id = '${golden_documentation_generation_id}';")" || die "cannot read documentation activation"
		[[ "${state}" == "active|true|1" ]] && return 0
		sleep 1
	done
	die "documentation producer generation is not active with persisted provenance: ${state:-missing}"
}

# This row is deliberately read-model-only coverage; the producer proof above
# never uses it as a finding or evidence packet.
seed_golden_documentation_section_read_model() {
	local seeded
	seeded="$(pg "
WITH source_scope AS (
  SELECT scope.scope_id, scope.active_generation_id AS generation_id
  FROM ingestion_scopes AS scope
  JOIN scope_generations AS generation ON generation.generation_id = scope.active_generation_id
  WHERE scope.scope_kind = 'repository'
    AND COALESCE(scope.payload->>'name', scope.payload->>'repo_name', scope.payload->>'repo_slug') = 'orders-api'
    AND generation.status = 'active'
  LIMIT 1
), seeded AS (
  INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, observed_at, ingested_at,
    is_tombstone, payload
  )
  SELECT
    'golden-documentation-section', source_scope.scope_id, source_scope.generation_id,
    'documentation_section', 'golden-documentation-section', '1.1.0',
    'documentation', 0, 'derived', 'golden_fixture',
    'golden-documentation-section', NOW(), NOW(), FALSE,
    jsonb_build_object(
      'document_id', 'documentation-document:runtime-readiness',
      'revision_id', 'golden-v1',
      'section_id', 'documentation-section:runtime-readiness',
      'section_anchor', 'runtime-readiness',
      'heading_text', 'Runtime readiness',
      'ordinal_path', jsonb_build_array(1),
      'content', 'The synthetic orders-api fixture documents its runtime readiness contract.',
      'content_format', 'markdown',
      'contains_warnings', false
    )
  FROM source_scope
  ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload
  RETURNING fact_id
)
SELECT count(*) FROM seeded;
")" || die "failed to insert documentation section read-model fixture"
	[[ "${seeded}" == "1" ]] || die "documentation section read-model seed returned ${seeded:-no rows}, want 1"
}

golden_documentation_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary
	jq -e '.query_shapes.mcp.get_documentation_evidence_packet.arguments.finding_id == "__runtime_documentation_finding_id__"' \
		"${input_snapshot}" >/dev/null || die "documentation runtime sentinels are missing"
	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "cannot create documentation snapshot temporary file"
	jq \
		--arg finding "${golden_documentation_finding_id}" \
		--arg packet "${golden_documentation_packet_id}" \
		--arg version "${golden_documentation_packet_version}" \
		--arg source "${golden_documentation_source_id}" \
		--arg document "${golden_documentation_document_id}" \
		--arg section "${golden_documentation_section_id}" \
		--arg uri "${golden_documentation_uri}" \
		'walk(if . == "__runtime_documentation_finding_id__" then $finding
		      elif . == "__runtime_documentation_packet_id__" then $packet
		      elif . == "__runtime_documentation_packet_version__" then $version
		      elif . == "__runtime_documentation_source_id__" then $source
		      elif . == "__runtime_documentation_document_id__" then $document
		      elif . == "__runtime_documentation_section_id__" then $section
		      elif . == "__runtime_documentation_uri__" then $uri
		      else . end)' "${input_snapshot}" >"${temporary}" || die "cannot compose documentation snapshot"
	mv "${temporary}" "${output_snapshot}" || die "cannot install documentation snapshot"
}

golden_documentation_run_proof() {
	local projector_pid reducer_pid runtime_snapshot
	# Producer verification and queue convergence are assertion work, outside
	# the API/MCP startup interval measured by phase_graph_query.
	phase_graph_query_excluded_starts+=("$(date +%s)")
	seed_golden_documentation_fixture
	start_bg projector-documentation projector_pid "${bin_dir}/eshu-projector"
	start_bg reducer-documentation reducer_pid "${bin_dir}/eshu-reducer"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		tail -30 "${log_dir}/reducer-documentation.log" || true
		tail -30 "${log_dir}/projector-documentation.log" || true
		die "documentation producer drain failed"
	fi
	golden_documentation_verify_active
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		die "documentation producer post-activation drain failed"
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	seed_golden_documentation_section_read_model
	runtime_snapshot="${log_dir}/e2e-20repo-documentation-runtime-snapshot.json"
	golden_documentation_compose_snapshot "${golden_query_runtime_snapshot}" "${runtime_snapshot}"
	golden_query_runtime_snapshot="${runtime_snapshot}"
	phase_graph_query_excluded_ends+=("$(date +%s)")
}
