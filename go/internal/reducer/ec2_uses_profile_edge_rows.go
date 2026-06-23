package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// ec2UsesProfileResourceTypeInstanceProfile is the aws_resource resource_type the
// IAM scanner emits for an instance-profile node. It mirrors
// awscloud.ResourceTypeIAMInstanceProfile; the duplication is intentional so the
// reducer does not import the collector package for one string constant.
const ec2UsesProfileResourceTypeInstanceProfile = "aws_iam_instance_profile"

// ec2UsesProfileResourceTypeInstance is the canonical resource-type token an EC2
// instance CloudResource node carries. It mirrors awscloud.ResourceTypeEC2Instance
// and the PR-A node materialization (ec2_instance_node_rows.go), so the edge's
// source uid is byte-identical to the node uid PR-A committed.
const ec2UsesProfileResourceTypeInstance = "aws_ec2_instance"

// ec2UsesProfileRelationshipType is the closed single-member relationship
// vocabulary this slice projects. It is the static token the cypher writer
// interpolates into the relationship-type position after validation.
const ec2UsesProfileRelationshipType = string(edgetype.UsesProfile)

// ec2UsesProfileModeARN is the only resolution mode for the USES_PROFILE edge
// counter: the target instance profile is resolved by exact ARN equality against
// the in-memory join index. An IAM instance-profile ARN is a precise identity,
// so ARN equality is exact, not a heuristic.
const ec2UsesProfileModeARN = "arn"

// Skip reasons for the bounded completion-log tally. Each posture fact that names
// an instance profile but produces no edge is counted under exactly one reason so
// an operator can see why USES_PROFILE edges were lost without a per-edge log
// line. A blank instance_profile_arn (no profile attached) is NOT counted here —
// it is the normal no-edge state, not a lost edge.
const (
	// ec2UsesProfileSkipSourceUnresolved: the posture fact carried neither an
	// instance id nor an arn, so it cannot form the source EC2 instance uid and
	// cannot anchor an edge. Counted once.
	ec2UsesProfileSkipSourceUnresolved = "source_unresolved"
	// ec2UsesProfileSkipTargetUnresolved: instance_profile_arn named a profile
	// that was not scanned as an aws_iam_instance_profile CloudResource node in
	// this scope generation (cross-account, out-of-scope). The trust-boundary rule
	// — no dangling node, no fabrication.
	ec2UsesProfileSkipTargetUnresolved = "target_unresolved"
)

// ec2UsesProfileEdgeTally is the bounded, honest accounting surface for the
// USES_PROFILE projection. The metric counts materialized edges by
// resolution_mode; the completion log keeps the skip-reason breakdown so an
// operator can answer "which instances are losing USES_PROFILE edges, and why?"
// without a per-edge log line.
type ec2UsesProfileEdgeTally struct {
	// resolved counts materialized edges keyed by resolution mode (arn) for the
	// metric and the completion log's resolved field.
	resolved map[string]int
	// skipped counts posture facts that named a profile but produced no edge,
	// keyed by the closed skip-reason set, for the completion log.
	skipped map[string]int
}

func newEC2UsesProfileEdgeTally() ec2UsesProfileEdgeTally {
	return ec2UsesProfileEdgeTally{
		resolved: make(map[string]int),
		skipped:  make(map[string]int),
	}
}

// totalSkipped returns the count of posture facts that named a profile but
// produced no edge because an endpoint was not scanned or had no identity.
func (t ec2UsesProfileEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// ec2InstanceProfileJoinIndex resolves an IAM instance-profile ARN to a scanned
// instance-profile CloudResource node uid. It is built once per scope generation
// from the aws_resource instance-profile facts so resolution is O(1) per posture
// fact — no per-edge graph round trip and no N+1 Cypher.
//
// It indexes only aws_iam_instance_profile resources, because the USES_PROFILE
// target must be an instance-profile node. An ARN absent from the index did not
// scan as a profile node and resolves to no edge — the trust-boundary rule, never
// fabricated. Each entry is derived from an aws_resource fact that carried its own
// account_id/region, so a cross-account profile resolves only if that account's
// profile was scanned in the same scope.
type ec2InstanceProfileJoinIndex struct {
	byARN map[string]string
}

// buildEC2InstanceProfileJoinIndex builds the bounded in-memory index from the
// scope generation's aws_resource fact envelopes, keeping only
// aws_iam_instance_profile resources and keying each by its ARN. The
// instance-profile node uid the aws_resource materialization committed is keyed by
// the profile ARN (resource_id == arn for instance profiles), so the index value
// is exactly that node uid.
func buildEC2InstanceProfileJoinIndex(envelopes []facts.Envelope) ec2InstanceProfileJoinIndex {
	index := ec2InstanceProfileJoinIndex{byARN: make(map[string]string, len(envelopes))}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if payloadString(env.Payload, "resource_type") != ec2UsesProfileResourceTypeInstanceProfile {
			continue
		}
		accountID := payloadString(env.Payload, "account_id")
		region := payloadString(env.Payload, "region")
		arn := strings.TrimSpace(payloadString(env.Payload, "arn"))
		resourceID := payloadString(env.Payload, "resource_id")
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		uid := cloudResourceUID(accountID, region, ec2UsesProfileResourceTypeInstanceProfile, resourceID)
		// First writer wins on collision so a later duplicate cannot re-point an
		// ARN to a different node. The ARN is the precise identity here.
		if arn != "" {
			if _, exists := index.byARN[arn]; !exists {
				index.byARN[arn] = uid
			}
		}
		if resourceID != arn {
			if _, exists := index.byARN[resourceID]; !exists {
				index.byARN[resourceID] = uid
			}
		}
	}
	return index
}

// resolve looks up an instance-profile ARN and returns the scanned node uid on an
// exact ARN hit.
func (i ec2InstanceProfileJoinIndex) resolve(arn string) (string, bool) {
	uid, ok := i.byARN[strings.TrimSpace(arn)]
	return uid, ok
}

// ExtractEC2UsesProfileEdgeRows builds canonical USES_PROFILE edge rows from the
// scope generation's ec2_instance_posture facts, resolving the source EC2
// instance to the CloudResource node uid PR-A committed (the same
// cloudResourceUID(account, region, "aws_ec2_instance", instance_id) scheme, with
// ARN fallback) and each instance_profile_arn to a scanned IAM instance-profile
// CloudResource node uid through an in-memory ARN join index built from the
// generation's aws_resource facts. It never fabricates a node: a profile whose
// ARN is not scanned in this scope is counted in the returned tally and produces
// no row.
//
// A blank instance_profile_arn (the instance has no attached profile) produces no
// row and is not counted as a skip. A tombstoned instance (a terminated instance
// no longer running, matching PR-A's node rule) produces no row and is not counted
// — there is no node to anchor an edge to.
//
// Returned rows are deduplicated by (source_uid, USES_PROFILE, target_uid) and
// sorted deterministically so the batched write is stable across retries and
// reprojections.
func ExtractEC2UsesProfileEdgeRows(
	resourceEnvelopes []facts.Envelope,
	postureEnvelopes []facts.Envelope,
) ([]map[string]any, ec2UsesProfileEdgeTally) {
	tally := newEC2UsesProfileEdgeTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally
	}

	index := buildEC2InstanceProfileJoinIndex(resourceEnvelopes)

	type edgeKey struct {
		source string
		target string
	}
	seen := make(map[edgeKey]struct{}, len(postureEnvelopes))
	rows := make([]map[string]any, 0, len(postureEnvelopes))

	for _, env := range postureEnvelopes {
		if env.FactKind != facts.EC2InstancePostureFactKind {
			continue
		}
		// A terminated/tombstoned instance no longer runs, so PR-A materialized no
		// node for it; there is nothing to anchor an edge to. Not a lost edge.
		if env.IsTombstone {
			continue
		}

		profileARN := strings.TrimSpace(payloadString(env.Payload, "instance_profile_arn"))
		if profileARN == "" {
			// The instance has no attached profile — the normal no-edge state, not
			// a skip-error.
			continue
		}

		sourceUID, sourceOK := ec2UsesProfileSourceUID(env)
		if !sourceOK {
			// The posture fact carried neither an instance id nor an arn, so it
			// cannot form the source uid. Count it once.
			tally.skipped[ec2UsesProfileSkipSourceUnresolved]++
			continue
		}

		targetUID, targetOK := index.resolve(profileARN)
		if !targetOK {
			// The instance profile was not scanned in this scope (cross-account,
			// out-of-scope). No dangling node, no fabrication.
			tally.skipped[ec2UsesProfileSkipTargetUnresolved]++
			continue
		}

		key := edgeKey{source: sourceUID, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		tally.resolved[ec2UsesProfileModeARN]++
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": ec2UsesProfileRelationshipType,
			"resolution_mode":   ec2UsesProfileModeARN,
		})
	}

	if len(rows) == 0 {
		return nil, tally
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally
}

// ec2UsesProfileSourceUID derives the source EC2 instance CloudResource node uid
// from a posture fact, returning ok=false when the fact carries neither an
// instance id nor an arn. It reuses PR-A's exact identity derivation — the bare
// instance id as resource_id, ARN fallback when the instance id is blank, and the
// canonical cloudResourceUID(account, region, "aws_ec2_instance", resource_id)
// scheme — so the edge's source endpoint resolves to the node PR-A materialized
// rather than a fabricated uid.
func ec2UsesProfileSourceUID(env facts.Envelope) (string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	instanceID := payloadString(env.Payload, "instance_id")
	arn := payloadString(env.Payload, "arn")

	resourceID := instanceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return "", false
	}

	resourceType := payloadString(env.Payload, "resource_type")
	if resourceType == "" {
		resourceType = ec2UsesProfileResourceTypeInstance
	}
	return cloudResourceUID(accountID, region, resourceType, resourceID), true
}
