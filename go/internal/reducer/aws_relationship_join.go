// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Join modes for the AWS relationship edge projection (issue #805). These are
// the closed enum the design doc §5.2 documents and the join_mode metric
// dimension carries. They are bounded and stable so operators can group the
// edge-projection counter by mode.
const (
	// joinModeARN resolves a target whose identity is an ARN (or ARN-shaped
	// resource_id), the common case for IAM/S3/KMS/MQ targets.
	joinModeARN = "arn"
	// joinModeBareID resolves a target whose identity is a bare AWS id such as
	// vpc-…, subnet-…, sg-…, igw-….
	joinModeBareID = "bare_id"
	// joinModeCorrelationAnchor resolves a name-only target (SageMaker
	// endpoint->config, MQ shared-configuration fallback, CloudFormation
	// stack-by-name) via the resource's published correlation anchors.
	joinModeCorrelationAnchor = "correlation_anchor"
	// joinModeUnresolved labels relationship facts whose endpoint could not be
	// resolved to a materialized CloudResource node in this scope generation.
	joinModeUnresolved = "unresolved"
)

// cloudResourceJoinIndex resolves an AWS relationship endpoint identity to the
// uid of a materialized CloudResource node. It is built once per scope
// generation from the aws_resource facts so target resolution is O(1) per edge
// — no per-edge graph round trip and no N+1 Cypher (design §5.1).
//
// All three maps key into the same uid space, so a hit in any map yields a real
// node uid. The index never fabricates a uid from a relationship fact alone:
// because each entry is derived from an aws_resource fact that carried its own
// account_id and region, a cross-account or cross-region ARN target resolves
// only if that account+region resource was scanned in the same scope (the
// trust-boundary rule, design §10.3).
type cloudResourceJoinIndex struct {
	byARN        map[string]string
	byUID        map[string]string
	byResourceID map[string]string
	byAnchor     map[string]string
}

// buildCloudResourceJoinIndex builds the bounded in-memory join index from the
// scope generation's aws_resource fact envelopes.
func buildCloudResourceJoinIndex(envelopes []facts.Envelope) cloudResourceJoinIndex {
	index := cloudResourceJoinIndex{
		byARN:        make(map[string]string, len(envelopes)),
		byUID:        make(map[string]string, len(envelopes)),
		byResourceID: make(map[string]string, len(envelopes)),
		byAnchor:     make(map[string]string, len(envelopes)),
	}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		accountID := payloadString(env.Payload, "account_id")
		region := payloadString(env.Payload, "region")
		resourceType := payloadString(env.Payload, "resource_type")
		resourceID := payloadString(env.Payload, "resource_id")
		arn := payloadString(env.Payload, "arn")
		if resourceID == "" {
			resourceID = arn
		}
		if resourceType == "" || resourceID == "" {
			// Mirrors cloudResourceNodeRow: an incomplete identity is not a
			// materializable node, so it is not a join target either.
			continue
		}

		uid := cloudResourceUID(accountID, region, resourceType, resourceID)
		if arn != "" {
			index.byARN[arn] = uid
			index.byUID[uid] = arn
		}
		index.byResourceID[resourceID] = uid
		for _, anchor := range payloadStrings(env.Payload, "", "correlation_anchors") {
			// First writer wins for an anchor so a later collision cannot
			// silently re-point a name to a different node. ARN and resource_id
			// already cover the precise identities; anchors are the name-only
			// fallback.
			if _, exists := index.byAnchor[anchor]; !exists {
				index.byAnchor[anchor] = uid
			}
		}
	}
	return index
}

func (i cloudResourceJoinIndex) arnForUID(uid string) (string, bool) {
	arn, ok := i.byUID[uid]
	return arn, ok
}

// resolveSource resolves the relationship source endpoint to a uid. The scanner
// sets source_resource_id to the ARN or the bare id consistently, so source
// resolution tries the ARN index first, then the resource-id index.
func (i cloudResourceJoinIndex) resolveSource(sourceARN, sourceResourceID string) (string, bool) {
	if sourceARN != "" {
		if uid, ok := i.byARN[sourceARN]; ok {
			return uid, true
		}
	}
	if sourceResourceID != "" {
		if uid, ok := i.byARN[sourceResourceID]; ok {
			return uid, true
		}
		if uid, ok := i.byResourceID[sourceResourceID]; ok {
			return uid, true
		}
	}
	return "", false
}

// resolveTarget resolves the relationship target endpoint to a uid and reports
// the join mode that matched. Mode selection is data-driven (design §5.2): try
// the ARN index (when target_arn is set or target_resource_id is ARN-shaped),
// then the bare-id index, then the correlation-anchor index. The first hit
// wins.
func (i cloudResourceJoinIndex) resolveTarget(targetARN, targetResourceID string) (string, string, bool) {
	if targetARN != "" {
		if uid, ok := i.byARN[targetARN]; ok {
			return uid, joinModeARN, true
		}
	}
	if targetResourceID != "" {
		if looksLikeARN(targetResourceID) {
			if uid, ok := i.byARN[targetResourceID]; ok {
				return uid, joinModeARN, true
			}
		}
		if uid, ok := i.byResourceID[targetResourceID]; ok {
			if looksLikeARN(targetResourceID) {
				return uid, joinModeARN, true
			}
			return uid, joinModeBareID, true
		}
		if uid, ok := i.byAnchor[targetResourceID]; ok {
			return uid, joinModeCorrelationAnchor, true
		}
	}
	return "", joinModeUnresolved, false
}

// looksLikeARN reports whether an identity string is an AWS ARN. Used only to
// classify the resolution mode for the metric; resolution itself is index
// membership, never string fabrication.
func looksLikeARN(value string) bool {
	return strings.HasPrefix(value, "arn:")
}

// relTypeMode is the bounded composite key for the edge-projection counter
// (eshu_dp_aws_relationship_edges_total): the AWS relationship type crossed with
// the join/resolution mode. Both members are drawn from closed enums — the
// fleet's relationship types and the four join modes — so the metric cardinality
// stays bounded (design §6).
type relTypeMode struct {
	relationshipType string
	mode             string
}

// awsRelationshipEdgeTally is the bounded, honest accounting surface for the
// edge projection (design §6). It serves two consumers with different cardinality
// needs: the metric is keyed by (relationship_type, join_mode); the completion
// log keeps the target_type breakdown so an operator can answer "which AWS
// relationship target types are losing edges, and is it because the target
// service was not scanned yet?" without a per-edge log line.
type awsRelationshipEdgeTally struct {
	// byRelTypeMode counts every relationship fact by (relationship_type,
	// join_mode) for the edge-projection counter. Resolved edges carry their
	// matched mode (arn / bare_id / correlation_anchor); a relationship whose
	// source or target did not resolve carries joinModeUnresolved. This honors
	// the (relationship_type, join_mode) metric contract.
	byRelTypeMode map[relTypeMode]int
	// resolved counts materialized edges keyed by join mode (arn / bare_id /
	// correlation_anchor) for the completion log's resolved_by_mode field.
	resolved map[string]int
	// unresolved counts relationships whose target could not be resolved,
	// keyed by target_type, for the completion log diagnostic.
	unresolved map[string]int
	// unresolvedSource counts relationships whose source could not be resolved,
	// keyed by target_type (the relationship's own classification), for the
	// completion log diagnostic.
	unresolvedSource map[string]int
}

func newAWSRelationshipEdgeTally() awsRelationshipEdgeTally {
	return awsRelationshipEdgeTally{
		byRelTypeMode:    make(map[relTypeMode]int),
		resolved:         make(map[string]int),
		unresolved:       make(map[string]int),
		unresolvedSource: make(map[string]int),
	}
}

// ExtractAWSRelationshipEdgeRows builds canonical AWS relationship edge rows by
// resolving each aws_relationship fact's endpoints against an in-memory index
// built from the scope generation's aws_resource facts. It never fabricates a
// node: an endpoint that is not a materialized CloudResource in this scope is
// counted in the returned tally and produces no row (graceful degradation).
//
// Returned rows are deduplicated by (source_uid, relationship_type,
// target_uid) and sorted deterministically so the batched write is stable
// across retries and reprojections.
func ExtractAWSRelationshipEdgeRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ([]map[string]any, awsRelationshipEdgeTally) {
	tally := newAWSRelationshipEdgeTally()
	if len(relationshipEnvelopes) == 0 {
		return nil, tally
	}

	index := buildCloudResourceJoinIndex(resourceEnvelopes)

	type edgeKey struct {
		source           string
		relationshipType string
		target           string
	}
	seen := make(map[edgeKey]struct{}, len(relationshipEnvelopes))
	rows := make([]map[string]any, 0, len(relationshipEnvelopes))

	for _, env := range relationshipEnvelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relationshipType := payloadString(env.Payload, "relationship_type")
		sourceARN := payloadString(env.Payload, "source_arn")
		sourceResourceID := payloadString(env.Payload, "source_resource_id")
		targetARN := payloadString(env.Payload, "target_arn")
		targetResourceID := payloadString(env.Payload, "target_resource_id")
		targetType := payloadString(env.Payload, "target_type")
		if targetType == "" {
			targetType = "unknown"
		}
		if relationshipType == "" {
			continue
		}

		sourceUID, sourceOK := index.resolveSource(sourceARN, sourceResourceID)
		if !sourceOK {
			tally.unresolvedSource[targetType]++
			tally.byRelTypeMode[relTypeMode{relationshipType, joinModeUnresolved}]++
			continue
		}

		targetUID, mode, targetOK := index.resolveTarget(targetARN, targetResourceID)
		if !targetOK {
			tally.unresolved[targetType]++
			tally.byRelTypeMode[relTypeMode{relationshipType, joinModeUnresolved}]++
			continue
		}

		if sourceUID == targetUID {
			// A self-loop carries no relationship truth; skip without counting
			// it as unresolved (both endpoints did resolve).
			continue
		}

		key := edgeKey{source: sourceUID, relationshipType: relationshipType, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		tally.resolved[mode]++
		tally.byRelTypeMode[relTypeMode{relationshipType, mode}]++
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": relationshipType,
			"target_type":       targetType,
			"resolution_mode":   mode,
		})
	}

	if len(rows) == 0 {
		return nil, tally
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["relationship_type"]) + ":" +
			anyToString(rows[a]["source_uid"]) + "->" +
			anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["relationship_type"]) + ":" +
			anyToString(rows[b]["source_uid"]) + "->" +
			anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally
}
