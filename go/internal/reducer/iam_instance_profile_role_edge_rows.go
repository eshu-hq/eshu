// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

const (
	iamInstanceProfileRoleResourceTypeInstanceProfile = "aws_iam_instance_profile"
	iamInstanceProfileRoleRelationshipType            = string(edgetype.HasRole)
	iamInstanceProfileRoleModeARN                     = "arn"
)

const (
	iamInstanceProfileRoleSkipSourceUnresolved = "source_unresolved"
	iamInstanceProfileRoleSkipTargetUnresolved = "target_unresolved"
)

type iamInstanceProfileRoleEdgeTally struct {
	resolved map[string]int
	skipped  map[string]int
}

func newIAMInstanceProfileRoleEdgeTally() iamInstanceProfileRoleEdgeTally {
	return iamInstanceProfileRoleEdgeTally{
		resolved: make(map[string]int),
		skipped:  make(map[string]int),
	}
}

func (t iamInstanceProfileRoleEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

type iamRoleJoinIndex struct {
	byARN map[string]string
}

func buildIAMRoleJoinIndex(envelopes []facts.Envelope) (iamRoleJoinIndex, error) {
	index := iamRoleJoinIndex{byARN: make(map[string]string, len(envelopes))}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			return iamRoleJoinIndex{}, err
		}
		if resource.ResourceType != iamResourceTypeRole {
			continue
		}
		arn := strings.TrimSpace(derefString(resource.ARN))
		resourceID := resource.ResourceID
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		uid := cloudResourceUID(resource.AccountID, resource.Region, iamResourceTypeRole, resourceID)
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
	return index, nil
}

func (i iamRoleJoinIndex) resolve(arn string) (string, bool) {
	uid, ok := i.byARN[strings.TrimSpace(arn)]
	return uid, ok
}

// ExtractIAMInstanceProfileRoleEdgeRows builds canonical HAS_ROLE edge rows from
// scanned IAM instance-profile aws_resource facts. The profile fact names role
// ARNs through role_arns, while target role nodes come from aws_iam_role
// aws_resource facts in the same generation. Resolution is exact ARN membership
// in a bounded in-memory index; unresolved roles are counted and never
// fabricated.
func ExtractIAMInstanceProfileRoleEdgeRows(
	envelopes []facts.Envelope,
) ([]map[string]any, iamInstanceProfileRoleEdgeTally, error) {
	tally := newIAMInstanceProfileRoleEdgeTally()
	if len(envelopes) == 0 {
		return nil, tally, nil
	}

	index, err := buildIAMRoleJoinIndex(envelopes)
	if err != nil {
		return nil, tally, err
	}

	type edgeKey struct {
		profile string
		role    string
	}
	seen := make(map[edgeKey]struct{}, len(envelopes))
	rows := make([]map[string]any, 0, len(envelopes))

	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			return nil, tally, err
		}
		if resource.ResourceType != iamInstanceProfileRoleResourceTypeInstanceProfile {
			continue
		}

		// role_arns is a service-specific field on the instance-profile
		// aws_resource fact, carried in the decoded struct's Attributes
		// pass-through rather than a named identity field.
		roleARNs := payloadStrings(resource.Attributes, "", "role_arns")
		if len(roleARNs) == 0 {
			continue
		}

		profileUID, ok := iamInstanceProfileRoleProfileUID(resource)
		if !ok {
			tally.skipped[iamInstanceProfileRoleSkipSourceUnresolved]++
			continue
		}

		for _, roleARN := range roleARNs {
			roleUID, roleOK := index.resolve(roleARN)
			if !roleOK {
				tally.skipped[iamInstanceProfileRoleSkipTargetUnresolved]++
				continue
			}
			key := edgeKey{profile: profileUID, role: roleUID}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			tally.resolved[iamInstanceProfileRoleModeARN]++
			rows = append(rows, map[string]any{
				"profile_uid":       profileUID,
				"role_uid":          roleUID,
				"relationship_type": iamInstanceProfileRoleRelationshipType,
				"resolution_mode":   iamInstanceProfileRoleModeARN,
			})
		}
	}

	if len(rows) == 0 {
		return nil, tally, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["profile_uid"]) + "->" + anyToString(rows[a]["role_uid"])
		right := anyToString(rows[b]["profile_uid"]) + "->" + anyToString(rows[b]["role_uid"])
		return left < right
	})
	return rows, tally, nil
}

func iamInstanceProfileRoleProfileUID(resource awsv1.Resource) (string, bool) {
	arn := derefString(resource.ARN)
	resourceID := resource.ResourceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return "", false
	}
	return cloudResourceUID(resource.AccountID, resource.Region, iamInstanceProfileRoleResourceTypeInstanceProfile, resourceID), true
}
