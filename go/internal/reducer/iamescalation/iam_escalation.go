// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamescalation

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// Skip / deferral reason labels for the escalation skipped counter. They are the
// bounded skip_reason metric dimension members for
// eshu_dp_iam_escalation_skipped_total and the completion-log breakdown. Each
// names a distinct conservative refusal so an operator can tell why escalation
// edges are missing (catalog doc §6).
const (
	iamEscalationSkipAmbiguous         = "skipped_ambiguous"
	iamEscalationSkipUnresolved        = "skipped_unresolved"
	iamEscalationSkipDeny              = "skipped_deny"
	iamEscalationSkipConditioned       = "skipped_conditioned"
	iamEscalationSkipNotActionResource = "skipped_not_action_resource"
	iamEscalationSkipIncomplete        = "skipped_incomplete"
	iamEscalationDeferredCanAssume     = "deferred_can_assume"
)

// iamEscalationTally is the honest accounting surface for primitives that did not
// become an edge. Each counter names a conservative refusal reason; a primitive
// is never dropped silently.
type iamEscalationTally struct {
	skippedAmbiguous         int
	skippedUnresolved        int
	skippedDeny              int
	skippedConditioned       int
	skippedNotActionResource int
	skippedIncomplete        int
	deferredCanAssume        int
}

// total returns the count of primitive evaluations that produced no edge.
func (t iamEscalationTally) total() int {
	return t.skippedAmbiguous + t.skippedUnresolved + t.skippedDeny +
		t.skippedConditioned + t.skippedNotActionResource + t.skippedIncomplete +
		t.deferredCanAssume
}

// IAMEscalationResult is the bounded, deterministic output of one generation's
// escalation extraction: the CAN_ESCALATE_TO edge rows to upsert and the skip
// tally. Edge rows are deduplicated by (principal_uid, target_uid) — when two
// primitives reach the same target they merge into one row's sorted primitives
// list — and sorted for byte-stable batched writes.
type IAMEscalationResult struct {
	Edges []map[string]any
	Tally iamEscalationTally
	// Quarantined carries the facts skipped as input_invalid during decode (a
	// missing required identity field), so the handler emits a visible per-fact
	// dead-letter while the valid facts still project.
	Quarantined []factdecode.QuarantinedFact
}

// ExtractIAMEscalationEdges resolves each IAM principal's privilege-escalation
// primitives against an in-memory CloudResource join index built from the scope
// generation's aws_resource facts. It promotes a primitive to a CAN_ESCALATE_TO
// edge only when the principal holds the COMPLETE primitive (all actions present,
// Allow, unconditioned, no NotAction/NotResource, not Deny-touched) AND the
// target resolves to EXACTLY ONE scanned IAM CloudResource node. Wildcard / many
// / zero / Deny / conditioned all degrade to a counted skip, never an edge, and
// it never fabricates a node (graceful degradation). sts:AssumeRole is recognized
// and deferred to the CAN_ASSUME edge (no edge emitted here).
//
// Returned edge rows are deduplicated by (principal_uid, target_uid) with merged
// primitives and sorted deterministically so the batched write is stable across
// retries and reprojections (idempotent).
func ExtractIAMEscalationEdges(
	resourceEnvelopes []facts.Envelope,
	permissionEnvelopes []facts.Envelope,
) (IAMEscalationResult, error) {
	result := IAMEscalationResult{}
	if len(permissionEnvelopes) == 0 {
		return result, nil
	}

	index, resourceQuarantined, err := cloudjoin.BuildCloudResourceJoinIndex(resourceEnvelopes)
	if err != nil {
		return IAMEscalationResult{}, err
	}
	result.Quarantined = append(result.Quarantined, resourceQuarantined...)
	principals, permissionQuarantined, err := groupIAMPermissionsByPrincipal(index, permissionEnvelopes, &result.Tally)
	if err != nil {
		return IAMEscalationResult{}, err
	}
	result.Quarantined = append(result.Quarantined, permissionQuarantined...)

	// edge identity -> merged primitive token set, so two primitives reaching the
	// same target converge on one idempotent edge with a sorted primitives list.
	primitivesByEdge := make(map[iampolicy.EdgeKey]map[string]struct{})

	for _, principal := range principals {
		grant := buildIAMPrincipalGrant(principal.Permissions, &result.Tally)

		for _, primitive := range iamEscalationCatalog {
			switch grantArmStatus(grant, primitive) {
			case iamPrimitiveDenied:
				result.Tally.skippedDeny++
				continue
			case iamPrimitiveIncomplete:
				result.Tally.skippedIncomplete++
				continue
			case iamPrimitiveArmed:
				// Falls through to target resolution below.
			}
			targetUID, status := resolveIAMEscalationTarget(index, grant, primitive)
			switch status {
			case iampolicy.TargetResolved:
				if targetUID == principal.PrincipalUID {
					// A self-escalation (e.g. CreateAccessKey on self) carries no
					// escalation truth; drop without counting it as a skip.
					continue
				}
				key := iampolicy.EdgeKey{PrincipalUID: principal.PrincipalUID, TargetUID: targetUID}
				if primitivesByEdge[key] == nil {
					primitivesByEdge[key] = make(map[string]struct{})
				}
				primitivesByEdge[key][primitive.Token] = struct{}{}
			case iampolicy.TargetAmbiguous:
				result.Tally.skippedAmbiguous++
			default:
				result.Tally.skippedUnresolved++
			}
		}
	}

	result.Edges = buildIAMEscalationEdgeRows(primitivesByEdge)
	return result, nil
}

// groupIAMPermissionsByPrincipal buckets permission facts by principal_arn and
// resolves each principal's own CloudResource node uid. A principal that was not
// scanned has no source node to anchor an edge on, so its statements are dropped
// and counted skippedUnresolved (one count per principal). The returned slice is
// sorted by principal uid for deterministic iteration.
func groupIAMPermissionsByPrincipal(
	index cloudjoin.CloudResourceJoinIndex,
	permissionEnvelopes []facts.Envelope,
	tally *iamEscalationTally,
) ([]iampolicy.PrincipalStatements, []factdecode.QuarantinedFact, error) {
	byPrincipalARN := make(map[string][]iampolicy.Statement)
	order := make([]string, 0)
	var quarantined []factdecode.QuarantinedFact
	for _, env := range permissionEnvelopes {
		if env.FactKind != facts.AWSIAMPermissionFactKind || env.IsTombstone {
			continue
		}
		permission, err := schemadecode.DecodeAWSIAMPermission(env)
		if err != nil {
			q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if permission.PrincipalARN == "" {
			continue
		}
		if _, seen := byPrincipalARN[permission.PrincipalARN]; !seen {
			order = append(order, permission.PrincipalARN)
		}
		byPrincipalARN[permission.PrincipalARN] = append(byPrincipalARN[permission.PrincipalARN], iampolicy.Statement{FactID: env.FactID, Permission: permission})
	}

	principals := make([]iampolicy.PrincipalStatements, 0, len(order))
	for _, principalARN := range order {
		uid, ok := index.ByARN[principalARN]
		if !ok {
			// The principal itself was not scanned: no anchor node exists, so none of
			// its primitives can become an edge. Count once and skip the principal.
			tally.skippedUnresolved++
			continue
		}
		principals = append(principals, iampolicy.PrincipalStatements{PrincipalUID: uid, Permissions: byPrincipalARN[principalARN]})
	}
	sort.Slice(principals, func(a, b int) bool {
		return principals[a].PrincipalUID < principals[b].PrincipalUID
	})
	return principals, quarantined, nil
}

// buildIAMEscalationEdgeRows turns the merged per-edge primitive sets into sorted,
// byte-stable edge rows. Each row carries the merged sorted primitives list and a
// primitive_count for cheap operator filtering. Rows are sorted by
// (principal_uid, target_uid) so the batched write is deterministic.
func buildIAMEscalationEdgeRows(primitivesByEdge map[iampolicy.EdgeKey]map[string]struct{}) []map[string]any {
	if len(primitivesByEdge) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(primitivesByEdge))
	for key, tokens := range primitivesByEdge {
		primitives := sortedPrimitiveTokens(tokens)
		rows = append(rows, map[string]any{
			"principal_uid":   key.PrincipalUID,
			"target_uid":      key.TargetUID,
			"primitives":      primitives,
			"primitive_count": len(primitives),
		})
	}
	sort.Slice(rows, func(a, b int) bool {
		left := payloadcore.AnyToString(rows[a]["principal_uid"]) + "->" + payloadcore.AnyToString(rows[a]["target_uid"])
		right := payloadcore.AnyToString(rows[b]["principal_uid"]) + "->" + payloadcore.AnyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows
}
