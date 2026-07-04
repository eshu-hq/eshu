// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// IAM permission fact field constants. PR1 (#1134) emits these payload keys on
// the derived aws_iam_permission fact; this slice consumes only the trust
// subset.
const (
	iamPermissionPolicySourceTrust = "trust"
	iamPermissionEffectAllow       = "Allow"
)

// IAM CloudResource node resource types the assuming-principal classifier
// recognizes. They mirror awscloud.ResourceTypeIAMRole / ResourceTypeIAMUser;
// the duplication is intentional so the reducer does not import the collector
// package for two string constants.
const (
	iamResourceTypeRole = "aws_iam_role"
	iamResourceTypeUser = "aws_iam_user"
)

// iamCanAssumeRelationshipType is the closed single-member relationship
// vocabulary this slice projects. It is the static token the cypher writer
// interpolates into the relationship-type position after validation.
const iamCanAssumeRelationshipType = string(edgetype.CanAssume)

// Assuming-principal kinds for the edge-projection counter principal_kind
// dimension. Bounded and stable so operators can group the counter by the
// resolved node type.
const (
	iamCanAssumePrincipalKindRole = "role"
	iamCanAssumePrincipalKindUser = "user"
)

// Resolution mode for the CAN_ASSUME edge counter. Resolution is ARN-equality
// against the in-memory join index; an unresolved principal carries no edge and
// is counted in the skip tally rather than as a resolution mode.
const (
	iamCanAssumeModeARN = "arn"
)

// Skip reasons for the bounded completion-log tally. Each assume-principal that
// does not materialize an edge is counted under exactly one reason so an
// operator can see why trust edges were lost without a per-edge log line.
const (
	// iamCanAssumeSkipWildcard: the principal is or contains "*" (public /
	// anonymous trust). It names no concrete node.
	iamCanAssumeSkipWildcard = "wildcard"
	// iamCanAssumeSkipServiceOrAccount: the principal is not an ARN — an
	// AWS-service principal (ec2.amazonaws.com), a bare account id, or another
	// non-ARN identifier. It cannot resolve to a scanned role/user node.
	iamCanAssumeSkipServiceOrAccount = "service_or_account"
	// iamCanAssumeSkipExternalUnresolved: the principal is an ARN (including an
	// account-root ARN) that did not resolve to a scanned role/user
	// CloudResource node in this scope generation — external, cross-account
	// unscanned, or a non-principal ARN.
	iamCanAssumeSkipExternalUnresolved = "external_unresolved"
	// iamCanAssumeSkipDeny: the trust statement effect is Deny; it grants no
	// assume-role and must not produce an edge.
	iamCanAssumeSkipDeny = "deny"
	// iamCanAssumeSkipSourceUnresolved: the fact's own principal_arn (the
	// role-with-trust-policy) did not resolve to a scanned role node, so the
	// whole statement is skipped once.
	iamCanAssumeSkipSourceUnresolved = "source_unresolved"
)

// iamCanAssumeEdgeTally is the bounded, honest accounting surface for the
// CAN_ASSUME projection. The metric is keyed by (principal_kind, resolution_mode)
// from the resolved map; the completion log keeps the skip-reason breakdown so
// an operator can answer "which trust principals are losing edges, and why?"
// without a per-edge log line.
type iamCanAssumeEdgeTally struct {
	// resolved counts materialized edges keyed by assuming-principal kind
	// (role / user) for the metric and the completion log's resolved field.
	resolved map[string]int
	// skipped counts assume-principals that produced no edge, keyed by the
	// closed skip-reason set, for the completion log.
	skipped map[string]int
}

func newIAMCanAssumeEdgeTally() iamCanAssumeEdgeTally {
	return iamCanAssumeEdgeTally{
		resolved: make(map[string]int),
		skipped:  make(map[string]int),
	}
}

// totalSkipped returns the count of assume-principals (and Deny / unresolved-
// source statements) that produced no edge.
func (t iamCanAssumeEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// iamRoleUserJoinIndex resolves an IAM principal ARN to a scanned role/user
// CloudResource node uid and its kind. It is built once per scope generation
// from the aws_resource facts so resolution is O(1) per assume-principal — no
// per-edge graph round trip and no N+1 Cypher.
//
// It indexes only IAM role and user resources, because those are the only node
// types a CAN_ASSUME endpoint may be. An ARN absent from the index did not scan
// as a role/user node and resolves to no edge — the trust-boundary rule, never
// fabricated. Each entry is derived from an aws_resource fact that carried its
// own account_id and region, so a cross-account ARN resolves only if that
// account's role/user was scanned in the same scope.
type iamRoleUserJoinIndex struct {
	byARN map[string]iamPrincipalNode
}

// iamPrincipalNode is a resolved IAM node identity: the CloudResource uid and
// the assuming-principal kind for the counter dimension.
type iamPrincipalNode struct {
	uid  string
	kind string
}

// buildIAMRoleUserJoinIndex builds the bounded in-memory index from the scope
// generation's aws_resource fact envelopes, keeping only IAM role and user
// resources.
func buildIAMRoleUserJoinIndex(envelopes []facts.Envelope) (iamRoleUserJoinIndex, []quarantinedFact, error) {
	index := iamRoleUserJoinIndex{byARN: make(map[string]iamPrincipalNode, len(envelopes))}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return iamRoleUserJoinIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		kind := iamPrincipalKindForResourceType(resource.ResourceType)
		if kind == "" {
			continue
		}
		arn := derefString(resource.ARN)
		resourceID := resource.ResourceID
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		uid := cloudResourceUID(resource.AccountID, resource.Region, resource.ResourceType, resourceID)
		node := iamPrincipalNode{uid: uid, kind: kind}
		// First writer wins on collision so a later duplicate cannot re-point an
		// ARN to a different node. The ARN is the precise identity here.
		if arn != "" {
			if _, exists := index.byARN[arn]; !exists {
				index.byARN[arn] = node
			}
		}
		if resourceID != arn {
			if _, exists := index.byARN[resourceID]; !exists {
				index.byARN[resourceID] = node
			}
		}
	}
	return index, quarantined, nil
}

// resolve looks up an IAM principal ARN. It returns the resolved node and true
// only on an exact ARN hit against a scanned role/user node.
func (i iamRoleUserJoinIndex) resolve(arn string) (iamPrincipalNode, bool) {
	node, ok := i.byARN[arn]
	return node, ok
}

// iamPrincipalKindForResourceType maps an aws_resource resource_type to the
// bounded assuming-principal kind, or "" for a non-principal resource.
func iamPrincipalKindForResourceType(resourceType string) string {
	switch resourceType {
	case iamResourceTypeRole:
		return iamCanAssumePrincipalKindRole
	case iamResourceTypeUser:
		return iamCanAssumePrincipalKindUser
	default:
		return ""
	}
}

// ExtractIAMCanAssumeEdgeRows builds canonical CAN_ASSUME edge rows from the
// scope generation's aws_iam_permission trust statements, resolving both the
// role-with-trust-policy and each assume-principal against an in-memory index
// built from the generation's aws_resource facts. It never fabricates a node:
// an assume-principal that is a wildcard, an AWS-service principal, a bare
// account id, an account-root ARN, or any ARN not scanned as a role/user node
// in this scope is counted in the returned tally and produces no row.
//
// Only policy_source=trust, effect=Allow statements are considered. A Deny
// trust statement, or one whose own principal_arn did not resolve to a scanned
// role node, materializes no edge and is counted. A self-assume (the role
// trusting itself) is skipped without counting (both endpoints resolved).
//
// Returned rows are deduplicated by (principal_uid, CAN_ASSUME, role_uid) and
// sorted deterministically so the batched write is stable across retries and
// reprojections.
func ExtractIAMCanAssumeEdgeRows(
	resourceEnvelopes []facts.Envelope,
	permissionEnvelopes []facts.Envelope,
) ([]map[string]any, iamCanAssumeEdgeTally, []quarantinedFact, error) {
	tally := newIAMCanAssumeEdgeTally()
	if len(permissionEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildIAMRoleUserJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}

	type edgeKey struct {
		principal string
		role      string
	}
	seen := make(map[edgeKey]struct{}, len(permissionEnvelopes))
	rows := make([]map[string]any, 0, len(permissionEnvelopes))

	for _, env := range permissionEnvelopes {
		if env.FactKind != facts.AWSIAMPermissionFactKind {
			continue
		}
		permission, err := decodeAWSIAMPermission(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if permission.PolicySource != iamPermissionPolicySourceTrust {
			continue
		}
		if permission.Effect != iamPermissionEffectAllow {
			tally.skipped[iamCanAssumeSkipDeny]++
			continue
		}

		roleNode, roleOK := index.resolve(permission.PrincipalARN)
		if !roleOK {
			// The role-with-trust-policy was not scanned as a node, so the whole
			// statement cannot anchor an edge. Count it once.
			tally.skipped[iamCanAssumeSkipSourceUnresolved]++
			continue
		}

		for _, principal := range permission.AssumePrincipals {
			reason, principalNode, ok := classifyAssumePrincipal(index, principal)
			if !ok {
				tally.skipped[reason]++
				continue
			}
			if principalNode.uid == roleNode.uid {
				// Self-assume carries no trust truth; skip without counting it as
				// a lost edge (both endpoints resolved).
				continue
			}
			key := edgeKey{principal: principalNode.uid, role: roleNode.uid}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			tally.resolved[principalNode.kind]++
			rows = append(rows, map[string]any{
				"principal_uid":     principalNode.uid,
				"role_uid":          roleNode.uid,
				"relationship_type": iamCanAssumeRelationshipType,
				"principal_kind":    principalNode.kind,
				"resolution_mode":   iamCanAssumeModeARN,
			})
		}
	}

	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["principal_uid"]) + "->" + anyToString(rows[a]["role_uid"])
		right := anyToString(rows[b]["principal_uid"]) + "->" + anyToString(rows[b]["role_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}

// classifyAssumePrincipal screens one assume-principal identifier and resolves
// it. It returns ok=true with the resolved node only on an exact ARN hit
// against a scanned role/user node; otherwise it returns ok=false with the
// bounded skip reason. Resolution is index membership, never string
// fabrication.
func classifyAssumePrincipal(index iamRoleUserJoinIndex, principal string) (string, iamPrincipalNode, bool) {
	principal = strings.TrimSpace(principal)
	if principal == "" || strings.Contains(principal, "*") {
		return iamCanAssumeSkipWildcard, iamPrincipalNode{}, false
	}
	if !strings.HasPrefix(principal, "arn:") {
		// AWS-service principal (ec2.amazonaws.com), bare account id, canonical
		// user id, or any other non-ARN identifier — names no role/user node.
		return iamCanAssumeSkipServiceOrAccount, iamPrincipalNode{}, false
	}
	if node, ok := index.resolve(principal); ok {
		return iamCanAssumeModeARN, node, true
	}
	// An ARN (including account-root and non-principal ARNs) that did not scan
	// as a role/user node in this scope.
	return iamCanAssumeSkipExternalUnresolved, iamPrincipalNode{}, false
}
