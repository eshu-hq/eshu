// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewRDSInstancePostureEnvelope builds the durable rds_instance_posture fact
// for one RDS DB instance or Aurora DB cluster. The fact captures derived
// security and operations posture (public exposure, encryption and KMS key,
// IAM database authentication, backup/multi-AZ/deletion-protection, Performance
// Insights configuration, parameter/option-group identity, a curated set of
// security-relevant non-default parameters, and the CA certificate identifier)
// as metadata-only control-plane evidence. It never carries database contents,
// master usernames, connection secrets, snapshot payloads, log bodies, or
// Performance Insights samples. It emits no graph edges; reducers own posture
// projection.
func NewRDSInstancePostureEnvelope(observation RDSPostureObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	arn := strings.TrimSpace(observation.ARN)
	resourceID := strings.TrimSpace(observation.ResourceID)
	if arn == "" && resourceID == "" {
		return facts.Envelope{}, fmt.Errorf("rds posture observation requires arn or resource_id")
	}
	if resourceID == "" {
		resourceID = arn
	}
	resourceType := strings.TrimSpace(observation.ResourceType)
	if resourceType == "" {
		return facts.Envelope{}, fmt.Errorf("rds posture observation requires resource_type")
	}
	identifier := strings.TrimSpace(observation.Identifier)
	stableKey := facts.StableID(facts.RDSInstancePostureFactKind, map[string]any{
		"account_id":    observation.Boundary.AccountID,
		"region":        observation.Boundary.Region,
		"resource_id":   resourceID,
		"resource_type": resourceType,
	})
	anchors := normalizedAnchors(nil, arn, resourceID, identifier)
	payload := map[string]any{
		"account_id":                          observation.Boundary.AccountID,
		"region":                              observation.Boundary.Region,
		"service_kind":                        observation.Boundary.ServiceKind,
		"collector_instance_id":               observation.Boundary.CollectorInstanceID,
		"arn":                                 arn,
		"resource_id":                         resourceID,
		"resource_type":                       resourceType,
		"identifier":                          identifier,
		"engine":                              strings.TrimSpace(observation.Engine),
		"publicly_accessible":                 observation.PubliclyAccessible,
		"storage_encrypted":                   observation.StorageEncrypted,
		"kms_key_id":                          strings.TrimSpace(observation.KMSKeyID),
		"iam_database_authentication_enabled": observation.IAMDatabaseAuthenticationEnabled,
		"multi_az":                            observation.MultiAZ,
		"deletion_protection":                 observation.DeletionProtection,
		"backup_retention_period":             observation.BackupRetentionPeriod,
		"performance_insights_enabled":        observation.PerformanceInsightsEnabled,
		"performance_insights_retention_days": observation.PerformanceInsightsRetentionDays,
		"performance_insights_kms_key_id":     strings.TrimSpace(observation.PerformanceInsightsKMSKeyID),
		"ca_certificate_identifier":           strings.TrimSpace(observation.CACertificateIdentifier),
		"parameter_groups":                    cloneStringSlice(observation.ParameterGroups),
		"option_groups":                       cloneStringSlice(observation.OptionGroups),
		"security_parameters":                 cloneStringMap(observation.SecurityParameters),
		"correlation_anchors":                 anchors,
	}
	return newEnvelope(
		observation.Boundary,
		facts.RDSInstancePostureFactKind,
		facts.RDSPostureSchemaVersionV1,
		stableKey,
		sourceRecordID(observation.SourceRecordID, resourceType+":"+resourceID),
		observation.SourceURI,
		payload,
	), nil
}
