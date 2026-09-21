// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
	securityParameters := cloneStringMap(observation.SecurityParameters)
	var securityParametersPtr *map[string]string
	if securityParameters != nil {
		securityParametersPtr = &securityParameters
	}
	payload, err := factschema.EncodeRDSInstancePosture(awsv1.RDSInstancePosture{
		AccountID:                        observation.Boundary.AccountID,
		Region:                           observation.Boundary.Region,
		ServiceKind:                      boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:              boundaryValue(observation.Boundary.CollectorInstanceID),
		ARN:                              stringValuePtr(arn),
		ResourceID:                       stringValuePtr(resourceID),
		ResourceType:                     stringValuePtr(resourceType),
		Identifier:                       stringValuePtr(identifier),
		Engine:                           stringValuePtr(strings.TrimSpace(observation.Engine)),
		PubliclyAccessible:               observation.PubliclyAccessible,
		StorageEncrypted:                 observation.StorageEncrypted,
		KMSKeyID:                         stringValuePtr(strings.TrimSpace(observation.KMSKeyID)),
		IAMDatabaseAuthenticationEnabled: observation.IAMDatabaseAuthenticationEnabled,
		MultiAZ:                          observation.MultiAZ,
		DeletionProtection:               observation.DeletionProtection,
		BackupRetentionPeriod:            observation.BackupRetentionPeriod,
		PerformanceInsightsEnabled:       observation.PerformanceInsightsEnabled,
		PerformanceInsightsRetentionDays: observation.PerformanceInsightsRetentionDays,
		PerformanceInsightsKMSKeyID:      stringValuePtr(strings.TrimSpace(observation.PerformanceInsightsKMSKeyID)),
		CACertificateIdentifier:          stringValuePtr(strings.TrimSpace(observation.CACertificateIdentifier)),
		ParameterGroups:                  cloneStringSlice(observation.ParameterGroups),
		OptionGroups:                     cloneStringSlice(observation.OptionGroups),
		SecurityParameters:               securityParametersPtr,
		CorrelationAnchors:               anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode rds_instance_posture payload: %w", err)
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
