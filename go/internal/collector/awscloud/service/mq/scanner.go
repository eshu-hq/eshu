// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon MQ metadata facts for one claimed account and region.
// It covers ActiveMQ and RabbitMQ broker engine types. The scanner never
// mutates brokers, configurations, or users, never reboots brokers, never
// persists broker user passwords, never persists configuration XML bodies, and
// never reads queue or topic message contents.
type Scanner struct {
	Client Client
}

// Scan observes Amazon MQ brokers and broker configurations through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("mq scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMQ:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMQ
	default:
		return nil, fmt.Errorf("mq scanner received service_kind %q", boundary.ServiceKind)
	}

	brokers, err := s.Client.ListBrokers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MQ brokers: %w", err)
	}
	configurations, err := s.Client.ListConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MQ configurations: %w", err)
	}
	configurationARNs := configurationARNByID(configurations)

	var envelopes []facts.Envelope
	for _, broker := range brokers {
		resource, err := awscloud.NewResourceEnvelope(brokerObservation(boundary, broker))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, observation := range brokerRelationships(boundary, broker, configurationARNs) {
			relationship, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}
	for _, configuration := range configurations {
		resource, err := awscloud.NewResourceEnvelope(configurationObservation(boundary, configuration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func brokerObservation(boundary awscloud.Boundary, broker Broker) awscloud.ResourceObservation {
	brokerARN := strings.TrimSpace(broker.ARN)
	attributes := map[string]any{
		"broker_id":                  strings.TrimSpace(broker.ID),
		"engine_type":                strings.TrimSpace(broker.EngineType),
		"engine_version":             strings.TrimSpace(broker.EngineVersion),
		"deployment_mode":            strings.TrimSpace(broker.DeploymentMode),
		"host_instance_type":         strings.TrimSpace(broker.HostInstanceType),
		"storage_type":               strings.TrimSpace(broker.StorageType),
		"authentication_strategy":    strings.TrimSpace(broker.AuthStrategy),
		"publicly_accessible":        broker.PubliclyAccessible,
		"auto_minor_version_upgrade": broker.AutoMinorVersionUpgrade,
		"created":                    timeOrNil(broker.Created),
		"subnet_ids":                 cloneStrings(broker.SubnetIDs),
		"security_group_ids":         cloneStrings(broker.SecurityGroupIDs),
		"encryption":                 encryptionMap(broker.Encryption),
		"logs":                       logsMap(broker.Logs),
		"usernames":                  cloneStrings(broker.Usernames),
	}
	if broker.Configuration != nil {
		attributes["configuration"] = map[string]any{
			"id":       strings.TrimSpace(broker.Configuration.ID),
			"revision": broker.Configuration.Revision,
		}
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                brokerARN,
		ResourceID:         firstNonEmpty(brokerARN, broker.ID, broker.Name),
		ResourceType:       awscloud.ResourceTypeMQBroker,
		Name:               strings.TrimSpace(broker.Name),
		State:              strings.TrimSpace(broker.State),
		Tags:               cloneStringMap(broker.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{brokerARN, strings.TrimSpace(broker.ID), strings.TrimSpace(broker.Name)},
		SourceRecordID:     firstNonEmpty(brokerARN, broker.ID, broker.Name),
	}
}

func configurationObservation(boundary awscloud.Boundary, configuration Configuration) awscloud.ResourceObservation {
	configurationARN := strings.TrimSpace(configuration.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          configurationARN,
		ResourceID:   firstNonEmpty(configurationARN, configuration.ID, configuration.Name),
		ResourceType: awscloud.ResourceTypeMQConfiguration,
		Name:         strings.TrimSpace(configuration.Name),
		Tags:         cloneStringMap(configuration.Tags),
		Attributes: map[string]any{
			"configuration_id":        strings.TrimSpace(configuration.ID),
			"description":             strings.TrimSpace(configuration.Description),
			"engine_type":             strings.TrimSpace(configuration.EngineType),
			"engine_version":          strings.TrimSpace(configuration.EngineVersion),
			"authentication_strategy": strings.TrimSpace(configuration.AuthStrategy),
			"created":                 timeOrNil(configuration.Created),
			"latest_revision":         latestRevisionMap(configuration.LatestRevision),
		},
		CorrelationAnchors: []string{configurationARN, strings.TrimSpace(configuration.ID), strings.TrimSpace(configuration.Name)},
		SourceRecordID:     firstNonEmpty(configurationARN, configuration.ID, configuration.Name),
	}
}

func encryptionMap(encryption Encryption) map[string]any {
	return map[string]any{
		"use_aws_owned_key": encryption.UseAWSOwnedKey,
		"kms_key_id":        strings.TrimSpace(encryption.KMSKeyID),
	}
}

func logsMap(logs Logs) map[string]any {
	return map[string]any{
		"general_enabled":   logs.GeneralEnabled,
		"general_log_group": strings.TrimSpace(logs.GeneralLogGroup),
		"audit_enabled":     logs.AuditEnabled,
		"audit_log_group":   strings.TrimSpace(logs.AuditLogGroup),
	}
}

func latestRevisionMap(revision ConfigurationRevisionSummary) map[string]any {
	return map[string]any{
		"revision":    revision.Revision,
		"created":     timeOrNil(revision.Created),
		"description": strings.TrimSpace(revision.Description),
	}
}
