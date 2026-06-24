// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package licensemanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS License Manager metadata-only facts for one claimed account
// and region. It never grants, checks out, or mutates license state and never
// reads entitlement tokens or usage records. It reports license configurations
// plus the configuration-to-EC2-instance association relationship for resolvable
// EC2 instance associations.
type Scanner struct {
	// Client is the metadata-only License Manager snapshot source.
	Client Client
}

// Scan observes License Manager license configurations and their resource
// associations through the configured client and emits resource and
// relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("license manager scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceLicenseManager:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceLicenseManager
	default:
		return nil, fmt.Errorf("license manager scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list License Manager configurations: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, configuration := range snapshot.Configurations {
		next, err := configurationEnvelopes(boundary, configuration)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func configurationEnvelopes(boundary awscloud.Boundary, configuration Configuration) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(configurationObservation(boundary, configuration))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	configurationID := configurationResourceID(configuration)
	for _, association := range configuration.Associations {
		relationship := configurationInstanceRelationship(
			boundary, configurationID, configuration.ARN, association,
		)
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func configurationObservation(
	boundary awscloud.Boundary,
	configuration Configuration,
) awscloud.ResourceObservation {
	configurationARN := strings.TrimSpace(configuration.ARN)
	name := strings.TrimSpace(configuration.Name)
	resourceID := configurationResourceID(configuration)
	attributes := map[string]any{
		"license_configuration_id":  strings.TrimSpace(configuration.ID),
		"license_counting_type":     strings.TrimSpace(configuration.LicenseCountingType),
		"license_count_hard_limit":  configuration.LicenseCountHardLimit,
		"consumed_licenses":         configuration.ConsumedLicenses,
		"license_rule_count":        configuration.LicenseRuleCount,
		"product_information_count": configuration.ProductInformationCount,
		"association_count":         len(configuration.Associations),
		"associated_resource_types": associatedResourceTypes(configuration.Associations),
		"owner_account_id":          strings.TrimSpace(configuration.OwnerAccountID),
		"license_expiry":            timeOrNil(configuration.LicenseExpiry),
	}
	if configuration.LicenseCountConfigured {
		attributes["license_count"] = configuration.LicenseCount
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                configurationARN,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeLicenseManagerConfiguration,
		Name:               name,
		State:              strings.TrimSpace(configuration.Status),
		Tags:               cloneStringMap(configuration.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{configurationARN, strings.TrimSpace(configuration.ID), name},
		SourceRecordID:     resourceID,
	}
}

// associatedResourceTypes returns the sorted, de-duplicated set of
// License-Manager-reported association resource types (EC2_INSTANCE, EC2_HOST,
// EC2_AMI, RDS, SYSTEMS_MANAGER_MANAGED_INSTANCE). It is metadata describing the
// shape of the associations; no entitlement or usage value is recorded.
func associatedResourceTypes(associations []Association) []string {
	if len(associations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(associations))
	var types []string
	for _, association := range associations {
		kind := strings.TrimSpace(association.ResourceType)
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		types = append(types, kind)
	}
	if len(types) == 0 {
		return nil
	}
	sortStrings(types)
	return types
}
