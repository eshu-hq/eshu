// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// crossplaneSatisfiedByRelType is the canonical relationship type for the
// Crossplane Claim -> XRD classification edge (issue #5347). It is a single
// static token from a closed vocabulary; the edge writer interpolates it into
// the relationship-type position (which cannot be parameterized) and
// validates it against this exact constant.
const crossplaneSatisfiedByRelType = string(edgetype.SatisfiedBy)

// crossplaneSatisfiedByResolutionModeGroupClaimKind is the sole resolution
// mode this domain produces: an exact (group, kind) match between a
// K8sResource candidate and exactly one CrossplaneXRD's
// (spec.group, spec.claimNames.kind). Kept as a named mode (mirroring
// kubernetesRunsImageRelType's joinModeDigest) so the edge counter and
// completion log have a stable dimension even though only one mode exists
// today.
const crossplaneSatisfiedByResolutionModeGroupClaimKind = "group_claim_kind"

// crossplaneEntityTypeK8sResource and crossplaneEntityTypeXRD are the
// content_entity payload entity_type values that identify a Claim candidate
// and an XRD candidate respectively. They are the canonical Neo4j label
// strings (PascalCase) internal/content/shape/materialize.go's
// materializeEntities stamps onto every content entity's entity_type field —
// not the lowercase keys of internal/projector/canonical.go's
// entityTypeLabelMap, which map the OTHER direction (content-store string ->
// label) and are never themselves the stored value for a git-sourced content
// entity. A Claim is never parser-labeled (issue #5347): it is a generic
// K8sResource row whose (group, kind) resolves against exactly one XRD.
const (
	crossplaneEntityTypeK8sResource = "K8sResource"
	crossplaneEntityTypeXRD         = "CrossplaneXRD"
)

// crossplaneSatisfiedByEdgeTally is the bounded, honest accounting surface
// for the SATISFIED_BY edge projection, mirroring
// kubernetesCorrelationEdgeTally. materialized counts edges written, keyed by
// resolution_mode; ambiguousSkipped counts candidates whose (group, kind)
// matched 2+ XRD nodes (never a fabricated pick). A zero-match candidate is
// just a k8s resource — it produces no edge and no tally entry (would be
// per-row noise from every Deployment/Service/etc. in the corpus).
type crossplaneSatisfiedByEdgeTally struct {
	materialized     map[string]int
	ambiguousSkipped int
}

func newCrossplaneSatisfiedByEdgeTally() crossplaneSatisfiedByEdgeTally {
	return crossplaneSatisfiedByEdgeTally{materialized: make(map[string]int)}
}

func (t crossplaneSatisfiedByEdgeTally) totalMaterialized() int {
	total := 0
	for _, count := range t.materialized {
		total += count
	}
	return total
}

// crossplaneClaimCandidate is one K8sResource content_entity row that could
// resolve to a Claim: its canonical node uid plus the (group, kind) join key
// derived from its api_version/kind fields.
type crossplaneClaimCandidate struct {
	uid   string
	group string
	kind  string
}

// crossplaneXRDCandidate is one CrossplaneXRD content_entity row: its
// canonical node uid plus the (spec.group, spec.claimNames.kind) join key a
// Claim candidate resolves against.
type crossplaneXRDCandidate struct {
	uid       string
	group     string
	claimKind string
}

// ExtractCrossplaneSatisfiedByEdgeRows builds canonical SATISFIED_BY edge rows
// from content_entity facts: K8sResource rows are Claim candidates, CrossplaneXRD
// rows are the XRDs they resolve against. envelopes is expected to carry both
// the intent's own-scope content_entity facts (Claim candidates, and any
// same-repo XRDs) and the cross-scope active CrossplaneXRD facts the handler
// loads through ListActiveCrossplaneXRDFacts — XRDs commonly live in a
// separate platform repo from the Claims that reference them.
//
// Resolution (issue #5347 Q2): a candidate's (group, kind) must exactly match
// (case-sensitive) exactly one XRD's (group, claim_kind):
//   - zero matches: the row is an ordinary Kubernetes object, not a Claim. No
//     edge, no tally entry (avoids per-row noise from every non-Claim
//     resource in the corpus).
//   - exactly one match: materializes one SATISFIED_BY edge.
//   - two or more matches: ambiguous. No edge is written (never a fabricated
//     representative pick); counted in ambiguousSkipped.
//
// A candidate or XRD with an empty group or claim_kind is excluded from
// matching before the join: an empty group means a core-group (no "/" in
// apiVersion) or malformed value, and an empty claim_kind means a
// cluster-scoped-only or malformed XRD — neither carries enough identity to
// resolve safely, and matching on the empty string would let two unrelated
// core-group/cluster-scoped rows collide.
//
// Rows are deduplicated by (claim_uid, SATISFIED_BY, xrd_uid) and sorted
// deterministically so the batched MERGE write is byte-stable across retries
// and reprojections.
//
// The same CrossplaneXRD node can appear twice in envelopes when it lives in
// the requesting scope: once from the intent's own-scope content_entity load
// and once more from the handler's cross-scope
// crossplaneXRDFactLoader.ListActiveCrossplaneXRDFacts call, which loads
// every scope's active XRDs and does not exclude the requesting scope. xrdUIDs
// dedupes by uid per join key so one physical XRD node reached through both
// paths counts as one candidate, not a fabricated ambiguity (issue #5347).
func ExtractCrossplaneSatisfiedByEdgeRows(
	envelopes []facts.Envelope,
) ([]map[string]any, crossplaneSatisfiedByEdgeTally, error) {
	tally := newCrossplaneSatisfiedByEdgeTally()

	claims := make([]crossplaneClaimCandidate, 0, len(envelopes))
	xrdsByKey := make(map[crossplaneXRDJoinKey][]string)
	xrdUIDsSeenByKey := make(map[crossplaneXRDJoinKey]map[string]struct{})

	for _, envelope := range envelopes {
		if envelope.FactKind != factKindContentEntity || envelope.IsTombstone {
			continue
		}
		entityType := crossplaneContentEntityType(envelope.Payload)
		switch entityType {
		case crossplaneEntityTypeK8sResource:
			if candidate, ok := crossplaneClaimCandidateFromPayload(envelope.Payload); ok {
				claims = append(claims, candidate)
			}
		case crossplaneEntityTypeXRD:
			if xrd, ok := crossplaneXRDCandidateFromPayload(envelope.Payload); ok {
				key := crossplaneXRDJoinKey{group: xrd.group, claimKind: xrd.claimKind}
				seen := xrdUIDsSeenByKey[key]
				if seen == nil {
					seen = make(map[string]struct{})
					xrdUIDsSeenByKey[key] = seen
				}
				if _, dup := seen[xrd.uid]; dup {
					continue
				}
				seen[xrd.uid] = struct{}{}
				xrdsByKey[key] = append(xrdsByKey[key], xrd.uid)
			}
		}
	}

	if len(claims) == 0 || len(xrdsByKey) == 0 {
		return nil, tally, nil
	}

	type edgeKey struct {
		claim string
		xrd   string
	}
	seen := make(map[edgeKey]struct{}, len(claims))
	rows := make([]map[string]any, 0, len(claims))

	for _, claim := range claims {
		key := crossplaneXRDJoinKey{group: claim.group, claimKind: claim.kind}
		matches := xrdsByKey[key]
		switch len(matches) {
		case 0:
			continue
		case 1:
			xrdUID := matches[0]
			edge := edgeKey{claim: claim.uid, xrd: xrdUID}
			if _, dup := seen[edge]; dup {
				continue
			}
			seen[edge] = struct{}{}
			tally.materialized[crossplaneSatisfiedByResolutionModeGroupClaimKind]++
			rows = append(rows, map[string]any{
				"claim_uid":       claim.uid,
				"xrd_uid":         xrdUID,
				"rel_type":        crossplaneSatisfiedByRelType,
				"resolution_mode": crossplaneSatisfiedByResolutionModeGroupClaimKind,
				"claim_group":     claim.group,
				"claim_kind":      claim.kind,
			})
		default:
			tally.ambiguousSkipped++
		}
	}

	if len(rows) == 0 {
		return nil, tally, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["claim_uid"]) + "->" + anyToString(rows[a]["xrd_uid"])
		right := anyToString(rows[b]["claim_uid"]) + "->" + anyToString(rows[b]["xrd_uid"])
		return left < right
	})
	return rows, tally, nil
}

// crossplaneXRDJoinKey is the (group, claim_kind) identity a Claim candidate
// and an XRD candidate must match exactly (case-sensitive) to resolve.
type crossplaneXRDJoinKey struct {
	group     string
	claimKind string
}

// crossplaneContentEntityType returns the content_entity payload's
// entity_type (falling back to entity_kind, mirroring
// projector.buildContentEntityRecord's own dual-path read): the canonical
// Neo4j label string ("K8sResource" or "CrossplaneXRD") materializeEntities
// stamps for these rows, not the lowercase entityTypeLabelMap key.
func crossplaneContentEntityType(payload map[string]any) string {
	if value := payloadStr(payload, "entity_kind"); value != "" {
		return value
	}
	return payloadStr(payload, "entity_type")
}

// crossplaneEntityMetadataString reads one field from the content_entity
// payload's entity_metadata map — the wrapper
// internal/collector/git_content_fact_envelopes.go's contentEntityFactEnvelope
// nests every extra parser field (api_version, kind, group, claim_kind, ...)
// under, since none of them are in the reserved top-level key set
// (internal/projector/entity_metadata.go entityPayloadReservedKeys).
func crossplaneEntityMetadataString(payload map[string]any, key string) string {
	metadata, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	return payloadStr(metadata, key)
}

// crossplaneClaimCandidateFromPayload builds a Claim candidate from a
// K8sResource content_entity row. The node uid is the row's own entity_id
// (the same value internal/projector/canonical_entity_identity.go's
// canonicalGraphEntityID resolves to for the K8sResource label, since
// K8sResource is not in canonicalNamePathLineEntityLabels and therefore uses
// the incoming id directly). group is derived from api_version's segment
// before the first "/" (Kubernetes group/version convention); a core-group
// apiVersion (no "/", e.g. "v1") or an empty/malformed value yields an empty
// group and is excluded from matching by the caller's key construction (an
// empty group can never equal a non-empty XRD group).
func crossplaneClaimCandidateFromPayload(payload map[string]any) (crossplaneClaimCandidate, bool) {
	uid := payloadStr(payload, "entity_id")
	if uid == "" {
		return crossplaneClaimCandidate{}, false
	}
	apiVersion := crossplaneEntityMetadataString(payload, "api_version")
	kind := crossplaneEntityMetadataString(payload, "kind")
	if kind == "" {
		return crossplaneClaimCandidate{}, false
	}
	group := crossplaneAPIVersionGroup(apiVersion)
	if group == "" {
		return crossplaneClaimCandidate{}, false
	}
	return crossplaneClaimCandidate{uid: uid, group: group, kind: kind}, true
}

// crossplaneXRDCandidateFromPayload builds an XRD candidate from a
// CrossplaneXRD content_entity row. group and claim_kind are read directly
// from the parser's parseCrossplaneXRD row (spec.group, spec.claimNames.kind
// respectively); either being empty excludes the XRD from matching (a
// cluster-scoped-only or malformed XRD carries no safe join identity).
func crossplaneXRDCandidateFromPayload(payload map[string]any) (crossplaneXRDCandidate, bool) {
	uid := payloadStr(payload, "entity_id")
	if uid == "" {
		return crossplaneXRDCandidate{}, false
	}
	group := crossplaneEntityMetadataString(payload, "group")
	claimKind := crossplaneEntityMetadataString(payload, "claim_kind")
	if group == "" || claimKind == "" {
		return crossplaneXRDCandidate{}, false
	}
	return crossplaneXRDCandidate{uid: uid, group: group, claimKind: claimKind}, true
}

// crossplaneAPIVersionGroup splits a Kubernetes apiVersion ("group/version")
// into its group segment. A core-group apiVersion carries no "/" (e.g. "v1")
// and yields an empty group.
func crossplaneAPIVersionGroup(apiVersion string) string {
	idx := strings.IndexByte(apiVersion, '/')
	if idx <= 0 {
		return ""
	}
	return apiVersion[:idx]
}
