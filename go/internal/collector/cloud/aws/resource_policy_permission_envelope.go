// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// NewResourcePolicyPermissionEnvelope builds the durable
// aws_resource_policy_permission fact for one normalized statement from a
// resource-based policy attached to an AWS resource (an S3 bucket policy or KMS
// key policy).
//
// The fact is derived and metadata-only and mirrors aws_iam_permission's
// normalization rigor: it captures the attached resource identity, effect,
// normalized action/resource patterns, a condition-key summary, and the derived
// grantee-principal facts (account ids, principal types, public/cross-account).
// It NEVER carries the raw policy JSON body, the statement Sid/body, or any
// condition values. Action and resource lists are trimmed, lowercased
// (actions), de-duplicated, and sorted so a statement observed across
// generations keeps a stable identity regardless of the source document's
// element ordering or action casing. A resource with no attached policy emits no
// fact (the caller skips it), so this builder is never invoked for the empty
// case.
func NewResourcePolicyPermissionEnvelope(observation ResourcePolicyPermissionObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	resourceARN := strings.TrimSpace(observation.ResourceARN)
	if resourceARN == "" {
		return facts.Envelope{}, fmt.Errorf("aws resource policy permission observation requires resource_arn")
	}
	resourceType := strings.TrimSpace(observation.ResourceType)
	if resourceType == "" {
		return facts.Envelope{}, fmt.Errorf("aws resource policy permission observation requires resource_type")
	}
	effect := normalizeEffect(observation.Effect)
	if effect == "" {
		return facts.Envelope{}, fmt.Errorf("aws resource policy permission observation requires effect")
	}

	actions := normalizeActionList(observation.Actions)
	notActions := normalizeActionList(observation.NotActions)
	resources := normalizePatternList(observation.Resources)
	notResources := normalizePatternList(observation.NotResources)
	conditionKeys := normalizeKeyList(observation.ConditionKeys)
	conditionOperators := normalizeKeyList(observation.ConditionOperators)
	principalAccountIDs := normalizeKeyList(observation.PrincipalAccountIDs)
	principalARNs := normalizePatternList(observation.PrincipalARNs)
	principalTypes := normalizeKeyList(observation.PrincipalTypes)

	statementSID := strings.TrimSpace(observation.StatementSID)

	stableIdentity := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"actions":               strings.Join(actions, ","),
		"effect":                effect,
		"not_actions":           strings.Join(notActions, ","),
		"not_resources":         strings.Join(notResources, ","),
		"policy_source":         ResourcePolicySourceResource,
		"principal_account_ids": strings.Join(principalAccountIDs, ","),
		"principal_arns":        strings.Join(principalARNs, ","),
		"region":                observation.Boundary.Region,
		"resource_arn":          resourceARN,
		"resource_type":         resourceType,
		"resources":             strings.Join(resources, ","),
		"statement_sid":         statementSID,
	}
	addConditionSummaryIdentity(stableIdentity, conditionKeys, conditionOperators)
	stableKey := facts.StableID(facts.AWSResourcePolicyPermissionFactKind, stableIdentity)

	payload, err := factschema.EncodeAWSResourcePolicyPermission(iamv1.ResourcePolicyPermission{
		AccountID:              observation.Boundary.AccountID,
		Region:                 observation.Boundary.Region,
		ServiceKind:            boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:    boundaryValue(observation.Boundary.CollectorInstanceID),
		ResourceARN:            resourceARN,
		ResourceType:           resourceType,
		PolicySource:           stringValuePtr(ResourcePolicySourceResource),
		Effect:                 effect,
		Actions:                actions,
		NotActions:             notActions,
		Resources:              resources,
		NotResources:           notResources,
		ConditionKeys:          conditionKeys,
		ConditionOperators:     conditionOperators,
		ConditionOperatorCount: intValuePtr(len(conditionOperators)),
		PrincipalAccountIDs:    principalAccountIDs,
		PrincipalARNs:          principalARNs,
		PrincipalTypes:         principalTypes,
		HasConditions:          boolValuePtr(len(conditionKeys) > 0 || len(conditionOperators) > 0),
		IsWildcardAction:       boolValuePtr(containsValue(actions, wildcardAction)),
		IsWildcardResource:     boolValuePtr(containsValue(resources, wildcardAction)),
		IsPublic:               boolValuePtr(observation.IsPublic),
		IsCrossAccount:         boolValuePtr(observation.IsCrossAccount),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_resource_policy_permission payload: %w", err)
	}

	return newEnvelope(
		observation.Boundary,
		facts.AWSResourcePolicyPermissionFactKind,
		facts.AWSResourcePolicyPermissionSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, resourcePolicyPermissionSourceID(resourceARN, resourceType, statementSID, effect, actions)),
		observation.SourceURI,
		payload,
	), nil
}

// resourcePolicyPermissionSourceID builds a deterministic source record id for a
// derived resource-policy statement so repeated observations of the same
// statement map to one durable fact within a generation. The statement Sid is
// part of the id (statements without a Sid fall back to the effect + action
// set) but is never written into the persisted payload.
func resourcePolicyPermissionSourceID(resourceARN, resourceType, statementSID, effect string, actions []string) string {
	parts := []string{resourceARN, resourceType, statementSID, effect, strings.Join(actions, ",")}
	return strings.Join(parts, "#")
}
