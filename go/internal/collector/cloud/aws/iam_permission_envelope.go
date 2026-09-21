// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// wildcardAction is the IAM action wildcard that grants every action. A
// statement whose normalized action set contains it is flagged is_wildcard_action
// so downstream posture analysis can treat it as a high-signal grant without
// re-parsing the action list.
const wildcardAction = "*"

// NewIAMPermissionEnvelope builds the durable aws_iam_permission fact for one
// normalized IAM policy statement attached to a principal.
//
// The fact is derived and metadata-only: it captures the principal, effect,
// normalized action/resource patterns, and a condition-key summary. It never
// carries the raw policy JSON body or condition values. Action and resource
// lists are trimmed, lowercased, de-duplicated, and sorted so a statement
// observed across generations keeps a stable identity regardless of the source
// document's element ordering or action casing.
func NewIAMPermissionEnvelope(observation IAMPermissionObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	principalARN := strings.TrimSpace(observation.PrincipalARN)
	if principalARN == "" {
		return facts.Envelope{}, fmt.Errorf("aws iam permission observation requires principal_arn")
	}
	effect := normalizeEffect(observation.Effect)
	if effect == "" {
		return facts.Envelope{}, fmt.Errorf("aws iam permission observation requires effect")
	}
	policySource := strings.TrimSpace(observation.PolicySource)
	if policySource == "" {
		return facts.Envelope{}, fmt.Errorf("aws iam permission observation requires policy_source")
	}

	actions := normalizeActionList(observation.Actions)
	notActions := normalizeActionList(observation.NotActions)
	resources := normalizePatternList(observation.Resources)
	notResources := normalizePatternList(observation.NotResources)
	conditionKeys := normalizeKeyList(observation.ConditionKeys)
	conditionOperators := normalizeKeyList(observation.ConditionOperators)
	assumePrincipals := normalizePatternList(observation.AssumePrincipals)

	policyARN := strings.TrimSpace(observation.PolicyARN)
	policyName := strings.TrimSpace(observation.PolicyName)
	statementSID := strings.TrimSpace(observation.StatementSID)

	stableIdentity := map[string]any{
		"account_id":    observation.Boundary.AccountID,
		"actions":       strings.Join(actions, ","),
		"effect":        effect,
		"not_actions":   strings.Join(notActions, ","),
		"not_resources": strings.Join(notResources, ","),
		"policy_arn":    policyARN,
		"policy_name":   policyName,
		"policy_source": policySource,
		"principal_arn": principalARN,
		"region":        observation.Boundary.Region,
		"resources":     strings.Join(resources, ","),
		"statement_sid": statementSID,
	}
	addConditionSummaryIdentity(stableIdentity, conditionKeys, conditionOperators)
	stableKey := facts.StableID(facts.AWSIAMPermissionFactKind, stableIdentity)

	payload, err := factschema.EncodeAWSIAMPermission(iamv1.Permission{
		AccountID:              observation.Boundary.AccountID,
		Region:                 observation.Boundary.Region,
		ServiceKind:            boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:    boundaryValue(observation.Boundary.CollectorInstanceID),
		PrincipalARN:           principalARN,
		PrincipalType:          stringValuePtr(strings.TrimSpace(observation.PrincipalType)),
		PolicySource:           policySource,
		PolicyARN:              stringValuePtr(policyARN),
		PolicyName:             stringValuePtr(policyName),
		StatementSID:           stringValuePtr(statementSID),
		Effect:                 effect,
		Actions:                actions,
		NotActions:             notActions,
		Resources:              resources,
		NotResources:           notResources,
		ConditionKeys:          conditionKeys,
		ConditionOperators:     conditionOperators,
		ConditionOperatorCount: intValuePtr(len(conditionOperators)),
		AssumePrincipals:       assumePrincipals,
		HasConditions:          boolValuePtr(len(conditionKeys) > 0 || len(conditionOperators) > 0),
		IsWildcardAction:       boolValuePtr(containsValue(actions, wildcardAction)),
		IsWildcardResource:     boolValuePtr(containsValue(resources, wildcardAction)),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_iam_permission payload: %w", err)
	}

	return newEnvelope(
		observation.Boundary,
		facts.AWSIAMPermissionFactKind,
		facts.AWSIAMPermissionSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, iamPermissionSourceID(principalARN, policySource, policyARN, policyName, statementSID, effect, actions)),
		observation.SourceURI,
		payload,
	), nil
}

// normalizeEffect canonicalizes an IAM statement effect to "Allow" or "Deny",
// returning "" for any other value so the builder rejects malformed statements.
func normalizeEffect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return "Allow"
	case "deny":
		return "Deny"
	default:
		return ""
	}
}

// normalizeActionList trims, lowercases, de-duplicates, and sorts IAM action
// strings. Actions are case-insensitive AWS API identifiers, so lowercasing
// gives a stable, comparable set across documents that vary the casing.
func normalizeActionList(values []string) []string {
	return normalizeStrings(values, strings.ToLower)
}

// normalizePatternList trims, de-duplicates, and sorts resource/principal ARN
// patterns. ARNs are case-sensitive, so the values are preserved verbatim apart
// from surrounding whitespace.
func normalizePatternList(values []string) []string {
	return normalizeStrings(values, nil)
}

// normalizeKeyList trims, de-duplicates, and sorts condition keys. Keys are
// preserved verbatim (apart from whitespace) so the summary names the exact
// condition keys without leaking their values.
func normalizeKeyList(values []string) []string {
	return normalizeStrings(values, nil)
}

// normalizeStrings trims each value, drops empties, optionally maps the survivor
// (for case folding), de-duplicates, and returns a sorted slice. It returns a
// non-nil empty slice for empty input so payload list fields stay typed.
func normalizeStrings(values []string, mapFn func(string) string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if mapFn != nil {
			trimmed = mapFn(trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

// containsValue reports whether sorted contains target. The lists are small
// (action/resource sets), so a linear scan is the simplest correct check.
func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// iamPermissionSourceID builds a deterministic source record id for a derived
// permission statement so repeated observations of the same statement map to one
// durable fact within a generation.
func iamPermissionSourceID(principalARN, policySource, policyARN, policyName, statementSID, effect string, actions []string) string {
	policyRef := policyARN
	if policyRef == "" {
		policyRef = policyName
	}
	parts := []string{principalARN, policySource, policyRef, statementSID, effect, strings.Join(actions, ",")}
	return strings.Join(parts, "#")
}
