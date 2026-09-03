// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

type iamCanPerformResourcePolicyDenyKey struct {
	principalUID string
	resourceUID  string
	action       string
}

type iamCanPerformResourcePolicyAllow struct {
	principalUID string
	resourceUID  string
	action       string
	mode         string
}

func flattenResourcePolicyEnvelopeSets(sets [][]facts.Envelope) []facts.Envelope {
	switch len(sets) {
	case 0:
		return nil
	case 1:
		return sets[0]
	}
	total := 0
	for _, set := range sets {
		total += len(set)
	}
	flattened := make([]facts.Envelope, 0, total)
	for _, set := range sets {
		flattened = append(flattened, set...)
	}
	return flattened
}

func addIAMCanPerformResourcePolicyEdges(
	index cloudjoin.CloudResourceJoinIndex,
	envelopes []facts.Envelope,
	catalog map[string]Action,
	edges map[iampolicy.EdgeKey]*iamCanPerformEdgeAccumulator,
	tally *iamCanPerformTally,
) ([]factdecode.QuarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, nil
	}
	catalogActions := iamCanPerformCatalogActionsFromCatalog(catalog)
	denied := make(map[iamCanPerformResourcePolicyDenyKey]struct{})
	allows := make([]iamCanPerformResourcePolicyAllow, 0)
	var quarantined []factdecode.QuarantinedFact

	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourcePolicyPermissionFactKind || env.IsTombstone {
			continue
		}
		permission, err := schemadecode.DecodeAWSResourcePolicyPermission(env)
		if err != nil {
			q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		effect := permission.Effect
		actions := permission.Actions
		if effect != "Allow" && effect != "Deny" {
			continue
		}
		if !iampolicy.StatementTouchesCatalog(actions, catalogActions) {
			if effect == "Allow" {
				countIAMCanPerformUncatalogued(actions, catalogActions, tally)
			}
			continue
		}
		hasConditions := payloadcore.DerefBool(permission.HasConditions)
		hasNotActions := len(permission.NotActions) > 0
		hasNotResources := len(permission.NotResources) > 0
		if hasConditions || hasNotActions || hasNotResources {
			if hasConditions {
				tally.recordSkip(iamCanPerformSkipConditioned)
			} else {
				tally.recordSkip(iamCanPerformSkipNotActionResource)
			}
			continue
		}
		if effect == "Allow" {
			countIAMCanPerformUncatalogued(actions, catalogActions, tally)
		}

		principalUIDs := resolveIAMCanPerformResourcePolicyPrincipals(index, permission, tally)
		if len(principalUIDs) == 0 {
			continue
		}
		resourceType := permission.ResourceType
		for _, entry := range catalog {
			if resourceType != entry.ExpectedResourceType {
				continue
			}
			if !iampolicy.AllowStatementTouches(actions, entry.Action) {
				continue
			}
			resourceUID, mode, status := resolveIAMCanPerformResourcePolicyTarget(index, permission, entry)
			switch status {
			case iampolicy.TargetResolved:
				for _, principalUID := range principalUIDs {
					key := iamCanPerformResourcePolicyDenyKey{
						principalUID: principalUID,
						resourceUID:  resourceUID,
						action:       entry.Action,
					}
					if effect == "Deny" {
						denied[key] = struct{}{}
						continue
					}
					allows = append(allows, iamCanPerformResourcePolicyAllow{
						principalUID: principalUID,
						resourceUID:  resourceUID,
						action:       entry.Action,
						mode:         mode,
					})
				}
			case iampolicy.TargetAmbiguous:
				tally.skippedAmbiguous++
			default:
				tally.skippedUnresolved++
			}
		}
	}

	for _, allow := range allows {
		key := iamCanPerformResourcePolicyDenyKey{
			principalUID: allow.principalUID,
			resourceUID:  allow.resourceUID,
			action:       allow.action,
		}
		if _, blocked := denied[key]; blocked {
			tally.skippedDeny++
			continue
		}
		if allow.resourceUID == allow.principalUID {
			tally.skippedSelfLoop++
			continue
		}
		addIAMCanPerformEdge(
			edges,
			allow.principalUID,
			allow.resourceUID,
			allow.action,
			allow.mode,
			iamCanPerformGrantSourceResourcePolicy,
			false,
		)
	}
	return quarantined, nil
}

func countIAMCanPerformUncatalogued(actions []string, catalogActions map[string]struct{}, tally *iamCanPerformTally) {
	for _, action := range actions {
		if !iamCanPerformActionIsCatalogued(action, catalogActions) {
			tally.skippedUncatalogued++
		}
	}
}

func resolveIAMCanPerformResourcePolicyPrincipals(
	index cloudjoin.CloudResourceJoinIndex,
	permission iamv1.ResourcePolicyPermission,
	tally *iamCanPerformTally,
) []string {
	if payloadcore.DerefBool(permission.IsPublic) {
		tally.skippedAmbiguous++
		return nil
	}
	principalARNs := permission.PrincipalARNs
	if len(principalARNs) == 0 {
		tally.skippedUnresolved++
		return nil
	}
	seen := make(map[string]struct{}, len(principalARNs))
	out := make([]string, 0, len(principalARNs))
	for _, arn := range principalARNs {
		switch iampolicy.ResourceTypeOfARN(arn) {
		case iampolicy.ResourceTypeRole, iampolicy.ResourceTypeUser:
		default:
			tally.skippedUnresolved++
			continue
		}
		uid, ok := index.ByARN[arn]
		if !ok {
			tally.skippedUnresolved++
			continue
		}
		if _, duplicate := seen[uid]; duplicate {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

func resolveIAMCanPerformResourcePolicyTarget(
	index cloudjoin.CloudResourceJoinIndex,
	permission iamv1.ResourcePolicyPermission,
	entry Action,
) (string, string, iampolicy.TargetStatus) {
	resourceARN := permission.ResourceARN
	if resourceARN == "" {
		return "", "", iampolicy.TargetUnresolved
	}
	if permission.ResourceType != entry.ExpectedResourceType {
		return "", "", iampolicy.TargetUnresolved
	}
	if iamCanPerformResourceTypeOfARN(resourceARN) != entry.ExpectedResourceType {
		return "", "", iampolicy.TargetUnresolved
	}
	if !iamCanPerformResourcePolicyAppliesToAttachedResource(permission, resourceARN, entry.ExpectedResourceType) {
		return "", "", iampolicy.TargetUnresolved
	}
	uid, ok := index.ByARN[resourceARN]
	if !ok {
		return "", "", iampolicy.TargetUnresolved
	}
	return uid, iamCanPerformResolutionExactARN, iampolicy.TargetResolved
}

func iamCanPerformResourcePolicyAppliesToAttachedResource(
	permission iamv1.ResourcePolicyPermission,
	resourceARN string,
	resourceType string,
) bool {
	patterns := permission.Resources
	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		switch {
		case pattern == resourceARN:
			return true
		case pattern == "*" && resourceType == iamCanPerformResourceTypeKMSKey:
			return true
		case strings.HasPrefix(pattern, resourceARN+"/"):
			return true
		case iampolicy.GlobMatch(pattern, resourceARN):
			return true
		}
	}
	return false
}
