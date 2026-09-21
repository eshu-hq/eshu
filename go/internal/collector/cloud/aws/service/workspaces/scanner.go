// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workspaces

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon WorkSpaces metadata-only facts for one claimed account
// and region. It never reads desktop session contents, user credentials,
// registration codes, or connection state, and never mutates a WorkSpace or any
// WorkSpaces resource. It reports WorkSpaces, registered directories,
// account-owned bundles, and IP access control groups plus the workspace-in-
// directory, workspace-uses-bundle, workspace-uses-KMS-key, and the directory's
// DS-directory, subnet, security-group, IAM-role, and IP-group relationships.
type Scanner struct {
	// Client is the metadata-only WorkSpaces snapshot source.
	Client Client
}

// Scan observes WorkSpaces, directories, bundles, and IP access control groups
// plus their direct dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("workspaces scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceWorkSpaces:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceWorkSpaces
	default:
		return nil, fmt.Errorf("workspaces scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot WorkSpaces: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, directory := range snapshot.Directories {
		next, err := directoryEnvelopes(boundary, directory)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, bundle := range snapshot.Bundles {
		envelope, err := aws.NewResourceEnvelope(bundleObservation(boundary, bundle))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, group := range snapshot.IPGroups {
		envelope, err := aws.NewResourceEnvelope(ipGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, workspace := range snapshot.Workspaces {
		next, err := workspaceEnvelopes(boundary, workspace)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func workspaceEnvelopes(boundary aws.Boundary, workspace Workspace) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(workspaceObservation(boundary, workspace))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*aws.RelationshipObservation{
		workspaceInDirectoryRelationship(boundary, workspace),
		workspaceUsesBundleRelationship(boundary, workspace),
		workspaceUsesKMSKeyRelationship(boundary, workspace),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func directoryEnvelopes(boundary aws.Boundary, directory Directory) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(directoryObservation(boundary, directory))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range directoryRelationships(boundary, directory) {
		envelope, err := aws.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func workspaceObservation(boundary aws.Boundary, workspace Workspace) aws.ResourceObservation {
	resourceID := workspaceResourceID(boundary, workspace)
	id := strings.TrimSpace(workspace.ID)
	name := strings.TrimSpace(workspace.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeWorkSpacesWorkspace,
		Name:         firstNonEmptyValue(name, id),
		State:        strings.TrimSpace(workspace.State),
		Tags:         cloneStringMap(workspace.Tags),
		Attributes: map[string]any{
			"workspace_id":                   id,
			"workspace_name":                 name,
			"directory_id":                   strings.TrimSpace(workspace.DirectoryID),
			"bundle_id":                      strings.TrimSpace(workspace.BundleID),
			"computer_name":                  strings.TrimSpace(workspace.ComputerName),
			"user_name":                      strings.TrimSpace(workspace.UserName),
			"volume_encryption_key":          strings.TrimSpace(workspace.VolumeEncryptionKey),
			"root_volume_encryption_enabled": workspace.RootVolumeEncryptionEnabled,
			"user_volume_encryption_enabled": workspace.UserVolumeEncryptionEnabled,
		},
		CorrelationAnchors: []string{resourceID, id},
		SourceRecordID:     resourceID,
	}
}

func directoryObservation(boundary aws.Boundary, directory Directory) aws.ResourceObservation {
	resourceID := directoryResourceID(boundary, directory)
	id := strings.TrimSpace(directory.ID)
	name := strings.TrimSpace(directory.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeWorkSpacesDirectory,
		Name:         firstNonEmptyValue(name, id),
		State:        strings.TrimSpace(directory.State),
		Tags:         cloneStringMap(directory.Tags),
		Attributes: map[string]any{
			"directory_id":                id,
			"directory_name":              name,
			"alias":                       strings.TrimSpace(directory.Alias),
			"directory_type":              strings.TrimSpace(directory.DirectoryType),
			"tenancy":                     strings.TrimSpace(directory.Tenancy),
			"iam_role_id":                 strings.TrimSpace(directory.IamRoleID),
			"workspace_security_group_id": strings.TrimSpace(directory.WorkspaceSecurityGroupID),
			"subnet_ids":                  cloneStrings(directory.SubnetIDs),
			"ip_group_ids":                cloneStrings(directory.IPGroupIDs),
		},
		CorrelationAnchors: []string{resourceID, id, strings.TrimSpace(directory.Alias)},
		SourceRecordID:     resourceID,
	}
}

func bundleObservation(boundary aws.Boundary, bundle Bundle) aws.ResourceObservation {
	resourceID := bundleResourceID(boundary, bundle)
	id := strings.TrimSpace(bundle.ID)
	name := strings.TrimSpace(bundle.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeWorkSpacesBundle,
		Name:         firstNonEmptyValue(name, id),
		State:        strings.TrimSpace(bundle.State),
		Tags:         cloneStringMap(bundle.Tags),
		Attributes: map[string]any{
			"bundle_id":            id,
			"bundle_name":          name,
			"description":          strings.TrimSpace(bundle.Description),
			"owner":                strings.TrimSpace(bundle.Owner),
			"bundle_type":          strings.TrimSpace(bundle.BundleType),
			"compute_type":         strings.TrimSpace(bundle.ComputeType),
			"root_volume_size_gib": strings.TrimSpace(bundle.RootVolumeSizeGib),
			"user_volume_size_gib": strings.TrimSpace(bundle.UserVolumeSizeGib),
			"image_id":             strings.TrimSpace(bundle.ImageID),
			"creation_time":        timeOrNil(bundle.CreationTime),
			"last_updated_time":    timeOrNil(bundle.LastUpdatedTime),
		},
		CorrelationAnchors: []string{resourceID, id},
		SourceRecordID:     resourceID,
	}
}

func ipGroupObservation(boundary aws.Boundary, group IPGroup) aws.ResourceObservation {
	resourceID := ipGroupResourceID(boundary, group)
	id := strings.TrimSpace(group.ID)
	name := strings.TrimSpace(group.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeWorkSpacesIPGroup,
		Name:         firstNonEmptyValue(name, id),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"group_id":    id,
			"group_name":  name,
			"description": strings.TrimSpace(group.Description),
			"rules":       ruleAttributes(group.Rules),
		},
		CorrelationAnchors: []string{resourceID, id},
		SourceRecordID:     resourceID,
	}
}

// firstNonEmptyValue returns the first trimmed non-empty value, or "" when none.
func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
