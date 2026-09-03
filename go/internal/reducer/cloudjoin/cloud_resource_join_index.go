// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudjoin

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// CloudResourceJoinIndex resolves an AWS relationship endpoint identity to the
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
type CloudResourceJoinIndex struct {
	ByARN        map[string]string
	ByUID        map[string]string
	ByResourceID map[string]string
	ByAnchor     map[string]string
}

// BuildCloudResourceJoinIndex builds the bounded in-memory join index from the
// scope generation's aws_resource fact envelopes. It decodes each aws_resource
// payload through the factschema seam (schemadecode.DecodeAWSResource) — the single decode
// site for this kind. A payload missing a required identity field (account_id,
// region, resource_type, resource_id) is QUARANTINED per-fact via
// factdecode.PartitionDecodeFailures: that one fact is skipped and returned in the
// quarantined slice (so the handler dead-letters it visibly), while every valid
// resource is still indexed. A non-decode error is returned fatally. This
// per-fact isolation means one malformed resource fact never drops the whole
// scope's join index (which would stall every edge domain gating on the
// canonical-nodes-committed readiness phase).
//
// arn is optional (a resource may be identified only by a bare resource_id), so
// resource_id falls back to the ARN the same way it did before typing; the
// typed struct's ResourceID already carries the emitter's arn-or-resource_id
// default, with ARN holding the raw value when present.
func BuildCloudResourceJoinIndex(envelopes []facts.Envelope) (CloudResourceJoinIndex, []factdecode.QuarantinedFact, error) {
	index := CloudResourceJoinIndex{
		ByARN:        make(map[string]string, len(envelopes)),
		ByUID:        make(map[string]string, len(envelopes)),
		ByResourceID: make(map[string]string, len(envelopes)),
		ByAnchor:     make(map[string]string, len(envelopes)),
	}
	var quarantined []factdecode.QuarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resource, err := schemadecode.DecodeAWSResource(env)
		if err != nil {
			q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return CloudResourceJoinIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		arn := ""
		if resource.ARN != nil {
			arn = *resource.ARN
		}
		resourceID := resource.ResourceID
		if resourceID == "" {
			resourceID = arn
		}
		if resource.ResourceType == "" || resourceID == "" {
			// Mirrors cloudResourceNodeRow: an incomplete identity is not a
			// materializable node, so it is not a join target either. This is a
			// present-but-empty value (a valid decode), distinct from an absent
			// required key, which quarantines above.
			continue
		}

		uid := CloudResourceUID(resource.AccountID, resource.Region, resource.ResourceType, resourceID)
		if arn != "" {
			index.ByARN[arn] = uid
			index.ByUID[uid] = arn
		}
		index.ByResourceID[resourceID] = uid
		// payloadcore.UniqueSortedStrings preserves the pre-typing byte-identical resolution:
		// the old payloadStrings(env.Payload, "", "correlation_anchors") trimmed
		// and dropped empty anchors, so an untrimmed or empty anchor never became
		// a lookup key. The typed decode returns the anchors raw.
		for _, anchor := range payloadcore.UniqueSortedStrings(resource.CorrelationAnchors) {
			// First writer wins for an anchor so a later collision cannot
			// silently re-point a name to a different node. ARN and resource_id
			// already cover the precise identities; anchors are the name-only
			// fallback.
			if _, exists := index.ByAnchor[anchor]; !exists {
				index.ByAnchor[anchor] = uid
			}
		}
	}
	return index, quarantined, nil
}

// CloudResourceUID computes the stable CloudResource node identity. The identity
// inputs match the aws_resource fact's StableFactKey inputs so the AWS
// relationship edge projection (issue #805) can recompute the same uid from a
// relationship fact's resolved target identity.
func CloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return payloadcore.CloudResourceUID(accountID, region, resourceType, resourceID)
}

// ARNForUID reverses the uid index back to the ARN the scanned node was keyed
// on. It reports false for a uid that was indexed without an ARN (a resource
// identified only by a bare resource id), so a caller can tell "no ARN" from
// the empty string.
func (i CloudResourceJoinIndex) ARNForUID(uid string) (string, bool) {
	arn, ok := i.ByUID[uid]
	return arn, ok
}
