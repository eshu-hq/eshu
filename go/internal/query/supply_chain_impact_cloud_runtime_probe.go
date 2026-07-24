// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: probeSupplyChainCloudRuntimeResources issues one
// bounded CloudResource graph read through the shared GraphQuery port, so the
// underlying backend read is covered by the port's existing query telemetry.
// That coverage does NOT explain the tier-promotion DECISION this probe drives
// (config_only/provenance_ci_declared -> runtime_confirmed), so the probe opens
// its own "supply_chain.cloud_runtime_probe" child span (queryHandlerTracer,
// shared with handler_tracing.go) carrying the number of subject digests
// probed, how many resolved to a live cloud resource, and the resource count —
// an operator can read that span to see exactly why a finding's deployment
// truth tier was (or was not) promoted to runtime_confirmed (#5452).

package query

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// supplyChainCloudRuntimeProbeMaxDigests bounds the runtime-image probe: at most
// this many distinct subject digests are matched against CloudResource nodes in
// one graph read, so a large findings page can never issue an unbounded IN-list
// graph query. A page returning more distinct digests than this is capped; the
// excess simply does not gain a runtime tier (a bounded, documented limit, not a
// silent wrong answer).
const supplyChainCloudRuntimeProbeMaxDigests = 200

// supplyChainCloudRuntimeProbeMaxResults bounds the total rows the probe's
// CloudResource label read returns, so a single vulnerable image running on an
// unusually large fleet can never return an unbounded ARN set. The probe is a
// bounded label-inventory read (registered as such in
// go/internal/queryplan/testdata/query-source-coverage.yaml), and this LIMIT is
// the max_results bound that classification declares.
//
// The query orders by (running_image_digest, arn) before the LIMIT, so the
// returned ARN set is DETERMINISTIC and reproducible run-to-run — a security
// evidence field must not vary. The remaining bound is honest: when one findings
// page collectively matches more than this many (digest, resource) rows, the
// rows for the highest-sorted digests are truncated, so those findings keep
// their CI-declared/config tier instead of runtime_confirmed. That is a
// deterministic, bounded, scale-gated limitation (a page of many findings each
// running on a large fleet); a per-digest bound that prevents one hot digest
// from starving other findings' runtime evidence is tracked as a follow-up
// (#5789).
const supplyChainCloudRuntimeProbeMaxResults = 200

// probeSupplyChainCloudRuntimeResources maps each given finding subject digest
// to the observed cloud resources (CloudResource graph nodes) whose
// running_image_digest equals that digest — the runtime-observed deployment
// evidence #5452 promotes to the runtime_confirmed deployment_truth_tier. The
// digest is the vulnerable artifact's own content-addressed identity, so a match
// means "this exact scanned image is running on that resource", not a shared
// base-image coincidence.
//
// It is bounded (a deduplicated, capped digest set matched in ONE graph read)
// and nil-safe: a nil GraphQuery port or empty input returns an empty map so
// tier classification degrades cleanly to CI-declared/config. A graph error is
// returned to the caller rather than swallowed, so a probe failure never
// silently downgrades a runtime_confirmed finding to a false config_only.
func (h *SupplyChainHandler) probeSupplyChainCloudRuntimeResources(
	ctx context.Context,
	digests []string,
) (map[string][]string, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	deduped := sortedUniqueNonEmptyStrings(digests)
	if len(deduped) > supplyChainCloudRuntimeProbeMaxDigests {
		deduped = deduped[:supplyChainCloudRuntimeProbeMaxDigests]
	}
	if len(deduped) == 0 {
		return nil, nil
	}

	ctx, span := queryHandlerTracer.Start(ctx, "supply_chain.cloud_runtime_probe")
	defer span.End()
	span.SetAttributes(attribute.Int("eshu.subject_digest_count", len(deduped)))

	const cypher = `
		MATCH (n:CloudResource)
		WHERE n.running_image_digest IN $digests
		  AND coalesce(n.arn, '') <> ''
		RETURN n.running_image_digest AS digest,
		       n.arn AS arn
		ORDER BY n.running_image_digest, n.arn
		LIMIT $limit
	`
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
		"digests": deduped,
		"limit":   supplyChainCloudRuntimeProbeMaxResults,
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	byDigest := make(map[string][]string, len(rows))
	for _, row := range rows {
		digest := strings.TrimSpace(StringVal(row, "digest"))
		arn := strings.TrimSpace(StringVal(row, "arn"))
		if digest == "" || arn == "" {
			continue
		}
		byDigest[digest] = append(byDigest[digest], arn)
	}
	resourceCount := 0
	for digest, refs := range byDigest {
		sorted := sortedUniqueNonEmptyStrings(refs)
		byDigest[digest] = sorted
		resourceCount += len(sorted)
	}
	span.SetAttributes(
		attribute.Int("eshu.runtime_confirmed_digest_count", len(byDigest)),
		attribute.Int("eshu.runtime_resource_count", resourceCount),
	)
	return byDigest, nil
}

// applySupplyChainCloudRuntimeEvidence probes the observed cloud resources
// running each finding's subject digest and records the matching resource refs
// on the rows in place, so buildSupplyChainImpactFindingResult classifies those
// findings as runtime_confirmed. Rows with no subject digest, or whose digest is
// not observed running on any cloud resource, are left untouched (their tier
// stays CI-declared or config-only). The probe error is propagated so the read
// fails loudly (mapped to a bounded, retryable graph-availability error by the
// caller) rather than serving a false config_only tier for a vulnerability that
// is actually running.
//
// Scope authorization: CloudResource graph nodes carry no scope_id (see
// go/internal/storage/cypher/cloud_resource_node_writer.go), so authorization
// for them runs through the Postgres owner ledger — the sibling
// listCloudResources restricts to ListCloudResourceIdentities before hydrating
// the graph. This probe reads the graph directly by digest and cannot apply
// that per-resource authorization, so for a scoped-token caller it would
// otherwise surface ARNs (account_id + region + resource name) of cloud
// resources in scopes the caller is not granted. Until the probe authorizes
// matched resources through the owner ledger, a SCOPED caller gets no
// runtime-observed cloud evidence — the finding keeps its CI-declared/config
// tier rather than leaking cross-scope infrastructure. Unrestricted (all-scope
// / admin) callers, whose grants already span every scope, get the full runtime
// enrichment. Follow-up: owner-ledger authorization so authorized scoped
// callers also receive the runtime tier (#5787).
func (h *SupplyChainHandler) applySupplyChainCloudRuntimeEvidence(
	ctx context.Context,
	access repositoryAccessFilter,
	rows []SupplyChainImpactFindingRow,
) error {
	if h == nil || h.Neo4j == nil || len(rows) == 0 {
		return nil
	}
	if access.scoped() {
		return nil
	}
	digests := make([]string, 0, len(rows))
	for _, row := range rows {
		if digest := strings.TrimSpace(row.SubjectDigest); digest != "" {
			digests = append(digests, digest)
		}
	}
	byDigest, err := h.probeSupplyChainCloudRuntimeResources(ctx, digests)
	if err != nil {
		return err
	}
	if len(byDigest) == 0 {
		return nil
	}
	for i := range rows {
		if refs := byDigest[strings.TrimSpace(rows[i].SubjectDigest)]; len(refs) > 0 {
			rows[i].CloudRuntimeResourceRefs = refs
		}
	}
	return nil
}
