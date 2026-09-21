// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Directory Service metadata facts for one claimed account and
// region. It covers AWS Managed Microsoft AD, Simple AD, and AD Connector
// directories plus their trust relationships, shared-directory invitations, and
// LDAPS settings metadata. It never persists the directory admin password, the
// RADIUS shared secret, or the AD Connector service-account credentials, and
// never calls a mutation API (ResetUserPassword, Create/Delete/Update/...).
type Scanner struct {
	Client Client
}

// Scan observes Directory Service directories through the configured client and,
// for each directory, its trust relationships, shared-directory invitations, and
// LDAPS settings. It emits aws_resource facts for directories, trusts, and shared
// directories plus directory-to-VPC, directory-to-subnet, trust-to-directory,
// shared-directory-to-owner-directory, and shared-directory-to-owner-account
// relationship evidence.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ds scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDirectoryService:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDirectoryService
	default:
		return nil, fmt.Errorf("ds scanner received service_kind %q", boundary.ServiceKind)
	}

	directories, err := s.Client.ListDirectories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Directory Service directories: %w", err)
	}

	directoryIDs := directoryIDSet(directories)

	var envelopes []facts.Envelope
	for _, directory := range directories {
		resource, err := awscloud.NewResourceEnvelope(directoryObservation(boundary, directory))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(directoryRelationships(boundary, directory))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)

		directoryID := strings.TrimSpace(directory.ID)
		if directoryID == "" {
			continue
		}

		trustEnvelopes, err := s.scanTrusts(ctx, boundary, directoryID)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, trustEnvelopes...)

		shareEnvelopes, err := s.scanSharedDirectories(ctx, boundary, directoryID, directoryIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, shareEnvelopes...)
	}
	return envelopes, nil
}

// scanTrusts emits one resource fact and one trust-to-directory edge per trust
// relationship reported for the directory.
func (s Scanner) scanTrusts(ctx context.Context, boundary awscloud.Boundary, directoryID string) ([]facts.Envelope, error) {
	trusts, err := s.Client.ListTrusts(ctx, directoryID)
	if err != nil {
		return nil, fmt.Errorf("list Directory Service trusts for %s: %w", directoryID, err)
	}
	var envelopes []facts.Envelope
	for _, trust := range trusts {
		resource, err := awscloud.NewResourceEnvelope(trustObservation(boundary, trust))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(trustRelationships(boundary, trust))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	return envelopes, nil
}

// scanSharedDirectories emits one resource fact and the owner-directory and
// owner-account edges per shared-directory invitation reported for the directory.
func (s Scanner) scanSharedDirectories(
	ctx context.Context,
	boundary awscloud.Boundary,
	directoryID string,
	directoryIDs map[string]struct{},
) ([]facts.Envelope, error) {
	shares, err := s.Client.ListSharedDirectories(ctx, directoryID)
	if err != nil {
		return nil, fmt.Errorf("list Directory Service shared directories for %s: %w", directoryID, err)
	}
	var envelopes []facts.Envelope
	for _, share := range shares {
		resource, err := awscloud.NewResourceEnvelope(sharedDirectoryObservation(boundary, share))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		rels, err := relationshipEnvelopes(sharedDirectoryRelationships(boundary, share, directoryIDs))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, rels...)
	}
	return envelopes, nil
}

func relationshipEnvelopes(observations []awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func directoryObservation(boundary awscloud.Boundary, directory Directory) awscloud.ResourceObservation {
	id := strings.TrimSpace(directory.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDSDirectory,
		Name:         strings.TrimSpace(directory.Name),
		State:        strings.TrimSpace(directory.Stage),
		Tags:         cloneStringMap(directory.Tags),
		Attributes: map[string]any{
			"directory_id":       id,
			"directory_type":     strings.TrimSpace(directory.Type),
			"edition":            strings.TrimSpace(directory.Edition),
			"size":               strings.TrimSpace(directory.Size),
			"short_name":         strings.TrimSpace(directory.ShortName),
			"description":        strings.TrimSpace(directory.Description),
			"access_url":         strings.TrimSpace(directory.AccessURL),
			"alias":              strings.TrimSpace(directory.Alias),
			"vpc_id":             strings.TrimSpace(directory.VPCID),
			"subnet_ids":         cloneStrings(directory.SubnetIDs),
			"security_group_id":  strings.TrimSpace(directory.SecurityGroupID),
			"availability_zones": cloneStrings(directory.AvailabilityZones),
			"ldaps_statuses":     cloneStrings(directory.LDAPSStatuses),
			"share_method":       strings.TrimSpace(directory.ShareMethod),
			"share_status":       strings.TrimSpace(directory.ShareStatus),
			"sso_enabled":        directory.SsoEnabled,
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(directory.Name), strings.TrimSpace(directory.Alias)},
		SourceRecordID:     id,
	}
}

func trustObservation(boundary awscloud.Boundary, trust Trust) awscloud.ResourceObservation {
	id := strings.TrimSpace(trust.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeDSTrust,
		Name:         strings.TrimSpace(trust.RemoteDomainName),
		State:        strings.TrimSpace(trust.State),
		Attributes: map[string]any{
			"trust_id":           id,
			"directory_id":       strings.TrimSpace(trust.DirectoryID),
			"remote_domain_name": strings.TrimSpace(trust.RemoteDomainName),
			"trust_direction":    strings.TrimSpace(trust.Direction),
			"trust_type":         strings.TrimSpace(trust.Type),
			"selective_auth":     strings.TrimSpace(trust.SelectiveAuth),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(trust.DirectoryID)},
		SourceRecordID:     id,
	}
}

func sharedDirectoryObservation(boundary awscloud.Boundary, share SharedDirectory) awscloud.ResourceObservation {
	resourceID := sharedDirectoryResourceID(share)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDSSharedDirectory,
		State:        strings.TrimSpace(share.ShareStatus),
		Attributes: map[string]any{
			"owner_account_id":    strings.TrimSpace(share.OwnerAccountID),
			"owner_directory_id":  strings.TrimSpace(share.OwnerDirectoryID),
			"shared_account_id":   strings.TrimSpace(share.SharedAccountID),
			"shared_directory_id": strings.TrimSpace(share.SharedDirectoryID),
			"share_method":        strings.TrimSpace(share.ShareMethod),
			"share_status":        strings.TrimSpace(share.ShareStatus),
		},
		CorrelationAnchors: []string{
			strings.TrimSpace(share.OwnerDirectoryID),
			strings.TrimSpace(share.SharedDirectoryID),
		},
		SourceRecordID: resourceID,
	}
}

// sharedDirectoryResourceID builds a stable resource_id for one share invitation
// from the owner directory id and the consumer account id, so two shares of the
// same directory to different consumer accounts stay distinct.
func sharedDirectoryResourceID(share SharedDirectory) string {
	owner := strings.TrimSpace(share.OwnerDirectoryID)
	consumer := strings.TrimSpace(share.SharedAccountID)
	switch {
	case owner != "" && consumer != "":
		return owner + ":" + consumer
	case owner != "":
		return owner
	default:
		return strings.TrimSpace(share.SharedDirectoryID)
	}
}

func directoryIDSet(directories []Directory) map[string]struct{} {
	ids := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		id := strings.TrimSpace(directory.ID)
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	return ids
}
