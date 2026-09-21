// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appstream

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon AppStream 2.0 metadata-only facts for one claimed account
// and region. It never reads streaming sessions, user data, or session scripts,
// and never mutates AppStream state. It reports fleets, stacks, image builders,
// and images plus the fleet/image-builder VPC subnet, security group, IAM role,
// and image dependencies, the fleet-to-stack associations, and the stack S3
// bucket dependencies for persistent application settings and storage connectors.
type Scanner struct {
	// Client is the metadata-only AppStream snapshot source.
	Client Client
}

// Scan observes AppStream fleets, stacks, image builders, images, and their
// reported dependency and association metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("appstream scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAppStream:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAppStream
	default:
		return nil, fmt.Errorf("appstream scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AppStream metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}

	fleetIDByName, fleetARNByName := fleetIndex(snapshot.Fleets)
	stackIDByName := stackIndex(snapshot.Stacks)

	for _, fleet := range snapshot.Fleets {
		if err := appendResource(&envelopes, fleetObservation(boundary, fleet)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, fleetRelationships(boundary, fleet)); err != nil {
			return nil, err
		}
	}
	for _, stack := range snapshot.Stacks {
		if err := appendResource(&envelopes, stackObservation(boundary, stack)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, stackS3Relationships(boundary, stack)); err != nil {
			return nil, err
		}
	}
	for _, builder := range snapshot.ImageBuilders {
		if err := appendResource(&envelopes, imageBuilderObservation(boundary, builder)); err != nil {
			return nil, err
		}
		if err := appendRelationships(&envelopes, imageBuilderRelationships(boundary, builder)); err != nil {
			return nil, err
		}
	}
	for _, image := range snapshot.Images {
		if err := appendResource(&envelopes, imageObservation(boundary, image)); err != nil {
			return nil, err
		}
	}
	for _, association := range snapshot.FleetStackAssociations {
		relationship, ok := fleetStackRelationship(boundary, association, fleetIDByName, fleetARNByName, stackIDByName)
		if !ok {
			continue
		}
		if err := appendRelationships(&envelopes, []awscloud.RelationshipObservation{relationship}); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

// fleetIndex maps each fleet name to its published resource_id and ARN so the
// fleet-to-stack association edge keys on the same id the fleet node publishes.
func fleetIndex(fleets []Fleet) (idByName map[string]string, arnByName map[string]string) {
	idByName = make(map[string]string, len(fleets))
	arnByName = make(map[string]string, len(fleets))
	for _, fleet := range fleets {
		name := strings.TrimSpace(fleet.Name)
		if name == "" {
			continue
		}
		idByName[name] = fleetResourceID(fleet)
		arnByName[name] = strings.TrimSpace(fleet.ARN)
	}
	return idByName, arnByName
}

// stackIndex maps each stack name to its published resource_id so the
// fleet-to-stack association edge keys on the same id the stack node publishes.
func stackIndex(stacks []Stack) map[string]string {
	idByName := make(map[string]string, len(stacks))
	for _, stack := range stacks {
		name := strings.TrimSpace(stack.Name)
		if name == "" {
			continue
		}
		idByName[name] = stackResourceID(stack)
	}
	return idByName
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

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationships(envelopes *[]facts.Envelope, observations []awscloud.RelationshipObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func fleetObservation(boundary awscloud.Boundary, fleet Fleet) awscloud.ResourceObservation {
	arn := strings.TrimSpace(fleet.ARN)
	name := strings.TrimSpace(fleet.Name)
	resourceID := fleetResourceID(fleet)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppStreamFleet,
		Name:         name,
		State:        strings.TrimSpace(fleet.State),
		Tags:         cloneStringMap(fleet.Tags),
		Attributes: map[string]any{
			"display_name":                   strings.TrimSpace(fleet.DisplayName),
			"description":                    strings.TrimSpace(fleet.Description),
			"fleet_type":                     strings.TrimSpace(fleet.FleetType),
			"instance_type":                  strings.TrimSpace(fleet.InstanceType),
			"platform":                       strings.TrimSpace(fleet.Platform),
			"stream_view":                    strings.TrimSpace(fleet.StreamView),
			"image_arn":                      strings.TrimSpace(fleet.ImageARN),
			"image_name":                     strings.TrimSpace(fleet.ImageName),
			"iam_role_arn":                   strings.TrimSpace(fleet.IAMRoleARN),
			"enable_default_internet_access": fleet.EnableDefaultInternetAccess,
			"max_concurrent_sessions":        fleet.MaxConcurrentSessions,
			"max_user_duration_in_seconds":   fleet.MaxUserDurationInSeconds,
			"subnet_ids":                     cloneStrings(fleet.SubnetIDs),
			"security_group_ids":             cloneStrings(fleet.SecurityGroupIDs),
			"created_time":                   timeOrNil(fleet.CreatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func stackObservation(boundary awscloud.Boundary, stack Stack) awscloud.ResourceObservation {
	arn := strings.TrimSpace(stack.ARN)
	name := strings.TrimSpace(stack.Name)
	resourceID := stackResourceID(stack)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppStreamStack,
		Name:         name,
		Tags:         cloneStringMap(stack.Tags),
		Attributes: map[string]any{
			"display_name":                   strings.TrimSpace(stack.DisplayName),
			"description":                    strings.TrimSpace(stack.Description),
			"application_settings_enabled":   stack.ApplicationSettingsEnabled,
			"application_settings_s3_bucket": strings.TrimSpace(stack.ApplicationSettingsS3Bucket),
			"storage_connector_buckets":      cloneStrings(stack.StorageConnectorBuckets),
			"created_time":                   timeOrNil(stack.CreatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func imageBuilderObservation(boundary awscloud.Boundary, builder ImageBuilder) awscloud.ResourceObservation {
	arn := strings.TrimSpace(builder.ARN)
	name := strings.TrimSpace(builder.Name)
	resourceID := imageBuilderResourceID(builder)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppStreamImageBuilder,
		Name:         name,
		State:        strings.TrimSpace(builder.State),
		Tags:         cloneStringMap(builder.Tags),
		Attributes: map[string]any{
			"display_name":                   strings.TrimSpace(builder.DisplayName),
			"description":                    strings.TrimSpace(builder.Description),
			"instance_type":                  strings.TrimSpace(builder.InstanceType),
			"platform":                       strings.TrimSpace(builder.Platform),
			"image_arn":                      strings.TrimSpace(builder.ImageARN),
			"iam_role_arn":                   strings.TrimSpace(builder.IAMRoleARN),
			"enable_default_internet_access": builder.EnableDefaultInternetAccess,
			"subnet_ids":                     cloneStrings(builder.SubnetIDs),
			"security_group_ids":             cloneStrings(builder.SecurityGroupIDs),
			"created_time":                   timeOrNil(builder.CreatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func imageObservation(boundary awscloud.Boundary, image Image) awscloud.ResourceObservation {
	arn := strings.TrimSpace(image.ARN)
	name := strings.TrimSpace(image.Name)
	resourceID := imageResourceID(image)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAppStreamImage,
		Name:         name,
		State:        strings.TrimSpace(image.State),
		Tags:         cloneStringMap(image.Tags),
		Attributes: map[string]any{
			"display_name":   strings.TrimSpace(image.DisplayName),
			"visibility":     strings.TrimSpace(image.Visibility),
			"image_type":     strings.TrimSpace(image.ImageType),
			"platform":       strings.TrimSpace(image.Platform),
			"base_image_arn": strings.TrimSpace(image.BaseImageARN),
			"created_time":   timeOrNil(image.CreatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
