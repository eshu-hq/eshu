// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package xray

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// encryptionConfigKMSRelationship emits a single edge from the X-Ray
// account-region encryption configuration to the KMS key that encrypts X-Ray
// data. It is emitted only when the encryption type is KMS and AWS reports a
// key reference, so an account using X-Ray default encryption produces no edge.
//
// The target keys whichever KMS family the reference names. X-Ray accepts a key
// id, key ARN, alias name, or alias ARN and reports it verbatim; the KMS scanner
// publishes keys as aws_kms_key keyed by firstNonEmpty(keyID, keyARN) and aliases
// as aws_kms_alias keyed by firstNonEmpty(aliasARN, aliasName). An alias-shaped
// reference therefore targets aws_kms_alias and a key-shaped reference targets
// aws_kms_key, with the reported reference used as the join key directly so the
// edge joins the right node family instead of dangling against the other.
// target_arn is set only when the reference is ARN-shaped, so an ARN-keyed
// target is never keyed by a bare id/name and a bare-id/name reference is never
// given a fabricated ARN.
func encryptionConfigKMSRelationship(
	boundary awscloud.Boundary,
	config EncryptionConfig,
) (awscloud.RelationshipObservation, bool) {
	if !strings.EqualFold(strings.TrimSpace(config.Type), encryptionTypeKMS) {
		return awscloud.RelationshipObservation{}, false
	}
	keyRef := strings.TrimSpace(config.KeyID)
	if keyRef == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetType := awscloud.ResourceTypeKMSKey
	if isKMSAliasReference(keyRef) {
		targetType = awscloud.ResourceTypeKMSAlias
	}
	source := encryptionConfigResourceID(boundary)
	relationship := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipXRayEncryptionConfigUsesKMSKey,
		SourceResourceID: source,
		TargetResourceID: keyRef,
		TargetType:       targetType,
		Attributes: map[string]any{
			"encryption_status": strings.TrimSpace(config.Status),
			"key_reference":     keyRef,
		},
		SourceRecordID: source + "->" + keyRef,
	}
	if isARN(keyRef) {
		relationship.TargetARN = keyRef
	}
	return relationship, true
}

// samplingRuleServiceRelationship emits a single labeled correlation-anchor
// edge from a sampling rule to the service identity it matches by service name
// and service type. It is emitted only when the rule names a concrete service
// (a wildcard-only "*"/"*" match anchors nothing and is skipped). The target is
// the synthetic aws_xray_service_correlation anchor, which reducers resolve to
// the real service node by name during materialization; the scanner never
// fabricates an ARN for the matched service.
func samplingRuleServiceRelationship(
	boundary awscloud.Boundary,
	rule SamplingRule,
) (awscloud.RelationshipObservation, bool) {
	source := firstNonEmpty(rule.ARN, rule.Name)
	if source == "" {
		return awscloud.RelationshipObservation{}, false
	}
	serviceName := strings.TrimSpace(rule.ServiceName)
	serviceType := strings.TrimSpace(rule.ServiceType)
	if isWildcard(serviceName) && isWildcard(serviceType) {
		return awscloud.RelationshipObservation{}, false
	}
	anchor := serviceCorrelationID(serviceName, serviceType)
	if anchor == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipXRaySamplingRuleMatchesService,
		SourceResourceID: source,
		SourceARN:        strings.TrimSpace(rule.ARN),
		TargetResourceID: anchor,
		TargetType:       awscloud.ResourceTypeXRayServiceCorrelation,
		Attributes: map[string]any{
			"service_name": serviceName,
			"service_type": serviceType,
			"rule_name":    strings.TrimSpace(rule.Name),
		},
		SourceRecordID: source + "->" + anchor,
	}, true
}

// isWildcard reports whether a sampling-rule match field is the X-Ray
// match-all wildcard ("*") or empty. A rule that matches every service by
// wildcard does not name a service to correlate, so it anchors nothing.
func isWildcard(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == "*"
}
