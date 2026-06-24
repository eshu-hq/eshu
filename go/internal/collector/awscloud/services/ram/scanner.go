// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ram

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Resource Access Manager metadata facts for one claimed
// account and region. It observes the resource shares the account owns, the
// resources they share, the principals they target, and the managed permissions
// they use. It never calls a mutation API and never persists a permission
// policy document body.
type Scanner struct {
	Client Client
}

// Scan observes RAM resource shares, shared resources, principals, and managed
// permissions through the configured client. It emits one resource fact per
// share and per permission, plus share-to-resource, share-to-principal, and
// share-to-permission relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ram scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceRAM:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceRAM
	default:
		return nil, fmt.Errorf("ram scanner received service_kind %q", boundary.ServiceKind)
	}

	shares, err := s.Client.ListResourceShares(ctx)
	if err != nil {
		return nil, fmt.Errorf("list RAM resource shares: %w", err)
	}
	var envelopes []facts.Envelope
	seenPermission := make(map[string]struct{})
	for _, share := range shares {
		observation, ok := shareObservation(boundary, share)
		if !ok {
			// A share with neither an ARN nor a name has no stable identity, so
			// skip it rather than failing the whole scan on one malformed record.
			continue
		}
		shareEnvelope, err := awscloud.NewResourceEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, shareEnvelope)

		for _, resource := range share.Resources {
			relationship, ok := shareResourceRelationship(boundary, share, resource)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}

		for _, principal := range share.Principals {
			relationship, ok := sharePrincipalRelationship(boundary, share, principal)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}

		for _, permission := range share.Permissions {
			permissionARN := strings.TrimSpace(permission.ARN)
			if permissionARN != "" {
				if _, ok := seenPermission[permissionARN]; !ok {
					seenPermission[permissionARN] = struct{}{}
					permissionEnvelope, err := awscloud.NewResourceEnvelope(permissionObservation(boundary, permission))
					if err != nil {
						return nil, err
					}
					envelopes = append(envelopes, permissionEnvelope)
				}
			}
			relationship, ok := sharePermissionRelationship(boundary, share, permission)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

// shareObservation builds the resource observation for one RAM resource share.
// It keys the resource on the share ARN, falling back to the share name when
// RAM reports a blank ARN, so one malformed share record cannot fail the whole
// scan. It returns false when the share has neither an ARN nor a name, because
// such a record has no stable identity to project.
func shareObservation(boundary awscloud.Boundary, share ResourceShare) (awscloud.ResourceObservation, bool) {
	shareARN := strings.TrimSpace(share.ARN)
	shareName := strings.TrimSpace(share.Name)
	shareID := shareJoinID(share)
	if shareID == "" {
		return awscloud.ResourceObservation{}, false
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          shareARN,
		ResourceID:   shareID,
		ResourceType: awscloud.ResourceTypeRAMResourceShare,
		Name:         shareName,
		State:        strings.TrimSpace(share.Status),
		Tags:         cloneStringMap(share.Tags),
		Attributes: map[string]any{
			"allow_external_principals": share.AllowExternalPrincipals,
			"owning_account_id":         strings.TrimSpace(share.OwningAccountID),
			"feature_set":               strings.TrimSpace(share.FeatureSet),
			"status":                    strings.TrimSpace(share.Status),
			"status_message":            strings.TrimSpace(share.StatusMessage),
			"creation_time":             timeOrNil(share.CreationTime),
			"last_updated_time":         timeOrNil(share.LastUpdatedTime),
			"resource_count":            len(share.Resources),
			"principal_count":           len(share.Principals),
			"permission_count":          len(share.Permissions),
		},
		CorrelationAnchors: []string{shareARN, shareName},
		SourceRecordID:     shareID,
	}, true
}

func permissionObservation(boundary awscloud.Boundary, permission Permission) awscloud.ResourceObservation {
	permissionARN := strings.TrimSpace(permission.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          permissionARN,
		ResourceID:   permissionARN,
		ResourceType: awscloud.ResourceTypeRAMPermission,
		Name:         strings.TrimSpace(permission.Name),
		State:        strings.TrimSpace(permission.Status),
		Attributes: map[string]any{
			"version":         strings.TrimSpace(permission.Version),
			"permission_type": strings.TrimSpace(permission.PermissionType),
			"resource_type":   strings.TrimSpace(permission.ResourceType),
			"status":          strings.TrimSpace(permission.Status),
			"default_version": permission.DefaultVersion,
		},
		CorrelationAnchors: []string{permissionARN, permission.Name},
		SourceRecordID:     permissionARN,
	}
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
