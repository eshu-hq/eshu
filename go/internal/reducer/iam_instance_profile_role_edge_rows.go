package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
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

func buildIAMRoleJoinIndex(envelopes []facts.Envelope) iamRoleJoinIndex {
	index := iamRoleJoinIndex{byARN: make(map[string]string, len(envelopes))}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		if payloadString(env.Payload, "resource_type") != iamResourceTypeRole {
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
		uid := cloudResourceUID(accountID, region, iamResourceTypeRole, resourceID)
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
) ([]map[string]any, iamInstanceProfileRoleEdgeTally) {
	tally := newIAMInstanceProfileRoleEdgeTally()
	if len(envelopes) == 0 {
		return nil, tally
	}

	index := buildIAMRoleJoinIndex(envelopes)

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
		if payloadString(env.Payload, "resource_type") != iamInstanceProfileRoleResourceTypeInstanceProfile {
			continue
		}

		roleARNs := payloadStrings(env.Payload, "", "role_arns")
		if len(roleARNs) == 0 {
			continue
		}

		profileUID, ok := iamInstanceProfileRoleProfileUID(env)
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
		return nil, tally
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["profile_uid"]) + "->" + anyToString(rows[a]["role_uid"])
		right := anyToString(rows[b]["profile_uid"]) + "->" + anyToString(rows[b]["role_uid"])
		return left < right
	})
	return rows, tally
}

func iamInstanceProfileRoleProfileUID(env facts.Envelope) (string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	resourceID := payloadString(env.Payload, "resource_id")
	arn := payloadString(env.Payload, "arn")
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return "", false
	}
	return cloudResourceUID(accountID, region, iamInstanceProfileRoleResourceTypeInstanceProfile, resourceID), true
}
