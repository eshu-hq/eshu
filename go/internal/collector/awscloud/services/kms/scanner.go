// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kms

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS KMS metadata facts for one claimed account and region.
// It never invokes cryptographic operations and never persists key policy
// Statement bodies, grant encryption contexts, or key material.
type Scanner struct {
	Client Client
}

// Scan observes KMS keys, aliases, and grants through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("kms scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceKMS:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceKMS
	default:
		return nil, fmt.Errorf("kms scanner received service_kind %q", boundary.ServiceKind)
	}

	keys, err := s.Client.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list KMS keys: %w", err)
	}
	var envelopes []facts.Envelope
	for _, key := range keys {
		keyEnvelopes, err := keyEnvelopes(boundary, key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, keyEnvelopes...)
	}
	return envelopes, nil
}

func keyEnvelopes(boundary awscloud.Boundary, key Key) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(keyObservation(boundary, key))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range resourcePolicyPermissionObservations(boundary, key) {
		permission, err := awscloud.NewResourcePolicyPermissionEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, permission)
	}
	for _, alias := range key.Aliases {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			aliasObservation(boundary, alias),
			aliasRelationship(boundary, key, alias),
		)
		if err != nil {
			return nil, err
		}
	}
	for _, grant := range key.Grants {
		envelopes, err = appendResourceAndRelationship(
			envelopes,
			grantObservation(boundary, key, grant),
			grantOnKeyRelationship(boundary, key, grant),
		)
		if err != nil {
			return nil, err
		}
		if granteeRel, ok := grantGranteeRelationship(boundary, key, grant); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(granteeRel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func appendResourceAndRelationship(
	envelopes []facts.Envelope,
	resource awscloud.ResourceObservation,
	relationship awscloud.RelationshipObservation,
) ([]facts.Envelope, error) {
	resourceEnvelope, err := awscloud.NewResourceEnvelope(resource)
	if err != nil {
		return nil, err
	}
	relationshipEnvelope, err := awscloud.NewRelationshipEnvelope(relationship)
	if err != nil {
		return nil, err
	}
	return append(envelopes, resourceEnvelope, relationshipEnvelope), nil
}

// resourcePolicyPermissionObservations maps the key's normalized key-policy
// statements into aws_resource_policy_permission observations. The statements
// arrive already derived on the Key model from a transient key-policy parse;
// this package never sees the raw policy document. A key with no readable policy
// carries no statements, so it emits no fact.
func resourcePolicyPermissionObservations(
	boundary awscloud.Boundary,
	key Key,
) []awscloud.ResourcePolicyPermissionObservation {
	if len(key.ResourcePolicyStatements) == 0 {
		return nil
	}
	keyARN := strings.TrimSpace(key.ARN)
	keyID := strings.TrimSpace(key.ID)
	resourceARN := firstNonEmpty(keyARN, keyID)
	observations := make([]awscloud.ResourcePolicyPermissionObservation, 0, len(key.ResourcePolicyStatements))
	for _, statement := range key.ResourcePolicyStatements {
		observations = append(observations, awscloud.ResourcePolicyPermissionObservation{
			Boundary:            boundary,
			ResourceARN:         resourceARN,
			ResourceType:        awscloud.ResourceTypeKMSKey,
			StatementSID:        statement.StatementSID,
			Effect:              statement.Effect,
			Actions:             statement.Actions,
			NotActions:          statement.NotActions,
			Resources:           statement.Resources,
			NotResources:        statement.NotResources,
			ConditionKeys:       statement.ConditionKeys,
			ConditionOperators:  statement.ConditionOperators,
			PrincipalAccountIDs: statement.PrincipalAccountIDs,
			PrincipalARNs:       statement.PrincipalARNs,
			PrincipalTypes:      statement.PrincipalTypes,
			IsPublic:            statement.IsPublic,
			IsCrossAccount:      statement.IsCrossAccount,
			SourceURI:           keyARN,
		})
	}
	return observations
}

func keyObservation(boundary awscloud.Boundary, key Key) awscloud.ResourceObservation {
	keyARN := strings.TrimSpace(key.ARN)
	keyID := strings.TrimSpace(key.ID)
	attributes := map[string]any{
		"key_manager":              strings.TrimSpace(key.KeyManager),
		"key_usage":                strings.TrimSpace(key.KeyUsage),
		"key_spec":                 strings.TrimSpace(key.KeySpec),
		"customer_master_key_spec": strings.TrimSpace(key.CustomerMasterKeySpec),
		"key_state":                strings.TrimSpace(key.KeyState),
		"origin":                   strings.TrimSpace(key.Origin),
		"description":              strings.TrimSpace(key.Description),
		"creation_date":            strings.TrimSpace(key.CreationDate),
		"deletion_date":            strings.TrimSpace(key.DeletionDate),
		"enabled":                  key.Enabled,
		"multi_region":             key.MultiRegion,
		"multi_region_key_type":    strings.TrimSpace(key.MultiRegionKeyType),
		"primary_key_arn":          strings.TrimSpace(key.PrimaryKeyARN),
		"encryption_algorithms":    cloneStrings(key.EncryptionAlgorithms),
		"signing_algorithms":       cloneStrings(key.SigningAlgorithms),
		"mac_algorithms":           cloneStrings(key.MACAlgorithms),
		"key_agreement_algorithms": cloneStrings(key.KeyAgreementAlgorithms),
		"policy_revision_names":    cloneStrings(key.PolicyRevisionNames),
		"alias_count":              len(key.Aliases),
		"grant_count":              len(key.Grants),
		"rotation_status_known":    key.RotationStatusKnown,
	}
	if key.RotationStatusKnown {
		attributes["rotation_enabled"] = key.RotationEnabled
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                keyARN,
		ResourceID:         firstNonEmpty(keyID, keyARN),
		ResourceType:       awscloud.ResourceTypeKMSKey,
		Name:               firstNonEmpty(keyID, keyARN),
		State:              strings.TrimSpace(key.KeyState),
		Tags:               cloneStringMap(key.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{keyARN, keyID},
		SourceRecordID:     firstNonEmpty(keyARN, keyID),
	}
}

func aliasObservation(boundary awscloud.Boundary, alias Alias) awscloud.ResourceObservation {
	aliasARN := strings.TrimSpace(alias.ARN)
	aliasName := strings.TrimSpace(alias.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          aliasARN,
		ResourceID:   firstNonEmpty(aliasARN, aliasName),
		ResourceType: awscloud.ResourceTypeKMSAlias,
		Name:         aliasName,
		Attributes: map[string]any{
			"alias_name":    aliasName,
			"target_key_id": strings.TrimSpace(alias.TargetKeyID),
			"last_updated":  strings.TrimSpace(alias.LastUpdated),
		},
		CorrelationAnchors: []string{aliasARN, aliasName},
		SourceRecordID:     firstNonEmpty(aliasARN, aliasName),
	}
}

func grantObservation(boundary awscloud.Boundary, key Key, grant Grant) awscloud.ResourceObservation {
	grantID := strings.TrimSpace(grant.ID)
	keyID := strings.TrimSpace(key.ID)
	resourceID := grantResourceID(keyID, grantID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeKMSGrant,
		Name:         firstNonEmpty(strings.TrimSpace(grant.Name), grantID),
		Attributes: map[string]any{
			"grant_id":           grantID,
			"grant_name":         strings.TrimSpace(grant.Name),
			"key_id":             keyID,
			"creation_date":      strings.TrimSpace(grant.CreationDate),
			"grantee_principal":  strings.TrimSpace(grant.GranteePrincipal),
			"retiring_principal": strings.TrimSpace(grant.RetiringPrincipal),
			"issuing_account":    strings.TrimSpace(grant.IssuingAccount),
			"operations":         cloneStrings(grant.Operations),
		},
		CorrelationAnchors: []string{grantID, resourceID},
		SourceRecordID:     resourceID,
	}
}

func aliasRelationship(boundary awscloud.Boundary, key Key, alias Alias) awscloud.RelationshipObservation {
	aliasARN := strings.TrimSpace(alias.ARN)
	aliasName := strings.TrimSpace(alias.Name)
	keyARN := strings.TrimSpace(key.ARN)
	keyID := strings.TrimSpace(key.ID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipKMSAliasTargetsKey,
		SourceResourceID: firstNonEmpty(aliasARN, aliasName),
		SourceARN:        aliasARN,
		TargetResourceID: firstNonEmpty(keyID, keyARN),
		TargetARN:        keyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		Attributes: map[string]any{
			"alias_name": aliasName,
		},
		SourceRecordID: firstNonEmpty(aliasARN, aliasName) + "->" + firstNonEmpty(keyID, keyARN),
	}
}

func grantOnKeyRelationship(boundary awscloud.Boundary, key Key, grant Grant) awscloud.RelationshipObservation {
	grantID := strings.TrimSpace(grant.ID)
	keyID := strings.TrimSpace(key.ID)
	keyARN := strings.TrimSpace(key.ARN)
	resourceID := grantResourceID(keyID, grantID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipKMSGrantOnKey,
		SourceResourceID: resourceID,
		TargetResourceID: firstNonEmpty(keyID, keyARN),
		TargetARN:        keyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		Attributes: map[string]any{
			"grant_id": grantID,
		},
		SourceRecordID: resourceID + "->" + firstNonEmpty(keyID, keyARN),
	}
}

func grantGranteeRelationship(boundary awscloud.Boundary, key Key, grant Grant) (awscloud.RelationshipObservation, bool) {
	grantee := strings.TrimSpace(grant.GranteePrincipal)
	if grantee == "" {
		return awscloud.RelationshipObservation{}, false
	}
	// A KMS grantee is either an IAM ARN or an AWS service principal (for
	// example "s3.amazonaws.com"). Mirror the IAM scanner's principal scheme:
	// encode the target identity as "<type>:<value>", only populate target_arn
	// when the value is actually an ARN, and record principal_type so reducers
	// can evaluate trust without re-deriving the kind.
	principalType := principalType(grant.GranteePrincipalType, grantee)
	grantID := strings.TrimSpace(grant.ID)
	keyID := strings.TrimSpace(key.ID)
	resourceID := grantResourceID(keyID, grantID)
	principalID := principalType + ":" + grantee
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipKMSGrantForGrantee,
		SourceResourceID: resourceID,
		TargetResourceID: principalID,
		TargetType:       awscloud.ResourceTypeIAMPrincipal,
		Attributes: map[string]any{
			"grant_id":           grantID,
			"principal_type":     principalType,
			"retiring_principal": strings.TrimSpace(grant.RetiringPrincipal),
		},
		SourceRecordID: resourceID + "->" + principalID,
	}
	if principalType == principalTypeAWS {
		relationship.TargetARN = grantee
	}
	return relationship, true
}

const (
	// principalTypeAWS marks a grantee that is an IAM ARN, matching the IAM
	// trust-policy "AWS" principal kind.
	principalTypeAWS = "AWS"
	// principalTypeService marks a grantee that is an AWS service principal
	// (for example "s3.amazonaws.com"), matching the IAM "Service" kind.
	principalTypeService = "Service"
)

// principalType resolves the grantee principal kind. It honors the
// adapter-supplied classification when present and otherwise infers the kind
// from ARN shape, so a service principal is never mistaken for an ARN.
func principalType(declared string, grantee string) string {
	switch strings.TrimSpace(declared) {
	case principalTypeAWS:
		return principalTypeAWS
	case principalTypeService:
		return principalTypeService
	}
	if strings.HasPrefix(grantee, "arn:") {
		return principalTypeAWS
	}
	return principalTypeService
}

func grantResourceID(keyID string, grantID string) string {
	return strings.Join([]string{strings.TrimSpace(keyID), "grant", strings.TrimSpace(grantID)}, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
