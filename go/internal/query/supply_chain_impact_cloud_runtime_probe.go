// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: probeSupplyChainCloudRuntimeResources issues one
// indexed, bounded read through the reducer-owned CloudResource owner ledger.
// The probe opens its own "supply_chain.cloud_runtime_probe" child span
// (queryHandlerTracer, shared with handler_tracing.go) carrying the number of
// subject digests probed, how many authorized current resources matched, and
// the final resolved counts — an operator can read that span to see exactly why
// a finding's deployment truth tier was (or was not) promoted to
// runtime_confirmed (#5452).

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"go.opentelemetry.io/otel/attribute"
)

// supplyChainCloudRuntimeProbeMaxDigests bounds the runtime-image probe: at most
// this many distinct subject digests are matched against owner-ledger rows in
// one read, so a large findings page can never issue an unbounded IN-list query.
// A page returning more distinct digests than this is capped; the
// excess simply does not gain a runtime tier (a bounded, documented limit, not a
// silent wrong answer).
const supplyChainCloudRuntimeProbeMaxDigests = 200

// supplyChainCloudRuntimeProbeMaxResults is the owner-ledger row budget for one
// findings page. It is no longer the bound the query applies:
// supplyChainCloudRuntimeProbePerDigestLimit divides this budget across the
// digests on the page, floored at
// supplyChainCloudRuntimeProbePerDigestMinResults, and the query applies that
// per-digest number inside a CROSS JOIN LATERAL -- one bounded, ordered index
// scan per digest (#5789).
//
// Freshness and caller authorization run inside that lateral BEFORE its LIMIT,
// so the bound counts ELIGIBLE rows rather than candidates. The ordering was
// measured, and reversing it returns wrong answers: on a corpus where only the
// last 50 of 50,000 rows were authorized for the caller, bounding candidates
// first and filtering after took 0.449 ms and returned nothing, reporting a
// genuinely running vulnerable image as not running. Eligibility inside the
// bound took 93 ms and returned the 50 rows
// (docs/internal/evidence/5789-per-digest-bound.md).
//
// Work stays bounded at len(digests) x the per-digest limit, at most 2000 rows,
// and the ordered scan keeps that row set reproducible run to run: a security
// evidence field must not vary. A page can still lose breadth -- a digest
// running on more resources than its share reports only the first slice of
// them. It no longer loses the promotion itself, because runtime_confirmed
// needs one current, authorized resource and every requested digest now gets
// its own share.
const supplyChainCloudRuntimeProbeMaxResults = 200

// supplyChainCloudRuntimeProbePerDigestMinResults is the floor under the
// owner-ledger rows considered FOR EACH subject digest. The per-digest bound
// replaces the total-row cap above as the query's actual bound (#5789); this
// constant is the smallest that bound may shrink to, however crowded the page.
//
// A total cap does not share. Measured on a skewed corpus -- one digest running
// on 30,000 resources, twenty others on 100 each -- the 200-row global cap
// returned rows for exactly ONE digest and zero for the other twenty, because
// the ordered scan spent the whole budget before reaching them. Those twenty
// findings silently kept their CI-declared tier: not a truncated answer, a
// missing one, and invisible because a finding with no runtime evidence looks
// identical to a finding whose image runs nowhere.
//
// Ten per digest keeps the promotion decision intact -- runtime_confirmed needs
// only that at least one current, authorized resource runs the digest, so a
// bounded sample answers it -- while total work stays bounded and deterministic
// at len(digests) x this value, itself capped by
// supplyChainCloudRuntimeProbeMaxDigests.
const supplyChainCloudRuntimeProbePerDigestMinResults = 10

// supplyChainCloudRuntimeProbePerDigestLimit returns the per-digest bound for a
// page of digestCount digests: the total budget shared evenly, with a floor.
//
// A flat per-digest number would have fixed starvation while quietly narrowing
// the single-digest case -- one digest used to yield up to
// supplyChainCloudRuntimeProbeMaxResults resource refs as evidence, and a flat
// 10 would have cut that to 10 for every caller, a user-visible reduction this
// issue never asked for.
//
// Sharing the existing total budget keeps both properties:
//
//   - 1 digest   -> 200 per digest: identical to the previous behaviour.
//   - 21 digests -> 10 (the floor): every finding gets evidence.
//   - 200 digests -> 10 (the floor): worst case 200 x 10 = 2000 rows.
//
// The floor is what actually fixes #5789: an even share alone would give a
// 200-digest page one row each, and a page's digest count must not decide
// whether a finding gets any runtime evidence at all. The worst case grows from
// 200 rows to 2000, bounded and stated rather than incidental (Copilot review).
func supplyChainCloudRuntimeProbePerDigestLimit(digestCount int) int {
	if digestCount <= 0 {
		return supplyChainCloudRuntimeProbePerDigestMinResults
	}
	share := supplyChainCloudRuntimeProbeMaxResults / digestCount
	if share < supplyChainCloudRuntimeProbePerDigestMinResults {
		return supplyChainCloudRuntimeProbePerDigestMinResults
	}
	return share
}

// probeSupplyChainCloudRuntimeResources maps each given finding subject digest
// to observed CloudResource owner-ledger rows whose running_image_digest equals
// that digest AND that are current + authorized —
// the runtime-observed deployment evidence #5452 promotes to the
// runtime_confirmed deployment_truth_tier. The digest is the vulnerable
// artifact's own content-addressed identity, so a match means "this exact
// scanned image is running on that resource", not a shared base-image
// coincidence.
//
// The owner-ledger read combines digest lookup, active-generation freshness,
// and authorization in one query. A stale graph node therefore cannot become
// runtime evidence, and a scoped caller never sees an ARN outside its grants.
//
// It is nil-safe: a nil runtime-digest resolver or empty input
// returns an empty map so tier classification degrades cleanly to
// CI-declared/config. A ledger error is returned to the caller rather
// than swallowed, so a probe failure never silently downgrades a
// runtime_confirmed finding to a false config_only.
func (h *SupplyChainHandler) probeSupplyChainCloudRuntimeResources(
	ctx context.Context,
	access repositoryAccessFilter,
	digests []string,
) (map[string][]string, error) {
	if h == nil || h.CloudResourceInventory == nil {
		return nil, nil
	}
	resolver, ok := h.CloudResourceInventory.(CloudResourceRuntimeDigestResolver)
	if !ok {
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
	span.SetAttributes(
		attribute.Int("eshu.subject_digest_count", len(deduped)),
		attribute.Int("eshu.authorized_current_resource_count", 0),
		attribute.Int("eshu.runtime_confirmed_digest_count", 0),
		attribute.Int("eshu.runtime_resource_count", 0),
	)

	rows, err := resolver.CurrentAuthorizedCloudResourcesByDigest(
		ctx,
		deduped,
		!access.Scoped(),
		access.GrantedRepositoryIDs(),
		access.GrantedScopeIDs(),
	)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	type cloudRuntimeMatch struct{ digest, arn string }
	byUID := make(map[string][]cloudRuntimeMatch, len(rows))
	authorizedCurrentUIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		uid := strings.TrimSpace(row.UID)
		digest := strings.TrimSpace(row.Digest)
		arn := strings.TrimSpace(row.ARN)
		if uid == "" || digest == "" || arn == "" {
			continue
		}
		if _, seen := byUID[uid]; !seen {
			authorizedCurrentUIDs = append(authorizedCurrentUIDs, uid)
		}
		byUID[uid] = append(byUID[uid], cloudRuntimeMatch{digest: digest, arn: arn})
	}
	if len(authorizedCurrentUIDs) == 0 {
		return nil, nil
	}

	byDigest := make(map[string][]string)
	for _, matches := range byUID {
		for _, match := range matches {
			byDigest[match.digest] = append(byDigest[match.digest], match.arn)
		}
	}
	resourceCount := 0
	for digest, refs := range byDigest {
		sorted := sortedUniqueNonEmptyStrings(refs)
		byDigest[digest] = sorted
		resourceCount += len(sorted)
	}
	span.SetAttributes(
		attribute.Int("eshu.authorized_current_resource_count", len(authorizedCurrentUIDs)),
		attribute.Int("eshu.runtime_confirmed_digest_count", len(byDigest)),
		attribute.Int("eshu.runtime_resource_count", resourceCount),
	)
	return byDigest, nil
}

// applySupplyChainCloudRuntimeEvidence probes the current, authorized cloud
// resources running each finding's subject digest and records the matching
// resource refs on the rows in place, so buildSupplyChainImpactFindingResult
// classifies those findings as runtime_confirmed. Rows with no subject digest,
// or whose digest is not observed running on an authorized current cloud
// resource, are left untouched (their tier stays CI-declared or config-only).
//
// Authorization and staleness are enforced inside the probe through the
// owner-ledger read (see probeSupplyChainCloudRuntimeResources):
// a scoped caller receives runtime evidence only for cloud resources it is
// granted, and a stale (retracted-in-a-later-scan) node never contributes. The
// probe error is propagated so the read fails loudly as a bounded internal
// query error rather than serving a false config_only tier for a vulnerability
// that is actually running.
func (h *SupplyChainHandler) applySupplyChainCloudRuntimeEvidence(
	ctx context.Context,
	access repositoryAccessFilter,
	rows []impact.SupplyChainImpactFindingRow,
) error {
	if h == nil || h.CloudResourceInventory == nil || len(rows) == 0 {
		return nil
	}
	digests := make([]string, 0, len(rows))
	for _, row := range rows {
		if digest := strings.TrimSpace(row.SubjectDigest); digest != "" {
			digests = append(digests, digest)
		}
	}
	byDigest, err := h.probeSupplyChainCloudRuntimeResources(ctx, access, digests)
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
