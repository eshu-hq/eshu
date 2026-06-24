// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lakeformation

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Lake Formation metadata-only facts for one claimed account
// and region. It governs the Glue Data Catalog, so it reuses the Glue database
// and table resource_id shapes as its permission edge targets. The scanner never
// grants, revokes, registers, or deregisters anything, and it never persists a
// permission policy body, an LF-Tag value, a condition expression, or principal
// credentials: only grant identities, principal identifiers, and resource ARNs
// are emitted.
type Scanner struct {
	Client Client
}

// Scan observes Lake Formation data-lake settings, registered data locations,
// and principal/resource permission grants through the configured client. It
// emits a settings resource (administrator principal identifiers only), one
// registered-resource resource per location with S3-bucket and IAM-role edges,
// and one permission resource per grant with Glue database/table and IAM-role
// principal edges.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("lakeformation scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceLakeFormation:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceLakeFormation
	default:
		return nil, fmt.Errorf("lakeformation scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	settings, err := s.Client.GetDataLakeSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Lake Formation data-lake settings: %w", err)
	}
	settingsEnvelope, err := awscloud.NewResourceEnvelope(settingsObservation(boundary, settings))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, settingsEnvelope)

	resources, err := s.Client.ListResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lake Formation registered resources: %w", err)
	}
	for _, resource := range resources {
		next, err := registeredResourceEnvelopes(boundary, resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	permissions, err := s.Client.ListPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lake Formation permissions: %w", err)
	}
	for _, permission := range permissions {
		next, err := permissionEnvelopes(boundary, permission)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func registeredResourceEnvelopes(boundary awscloud.Boundary, resource RegisteredResource) ([]facts.Envelope, error) {
	resourceEnvelope, err := awscloud.NewResourceEnvelope(registeredResourceObservation(boundary, resource))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resourceEnvelope}
	for _, relationship := range []*awscloud.RelationshipObservation{
		resourceS3BucketRelationship(boundary, resource),
		resourceRoleRelationship(boundary, resource),
	} {
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

func permissionEnvelopes(boundary awscloud.Boundary, permission Permission) ([]facts.Envelope, error) {
	permissionID := permissionResourceID(boundary, permission)
	resourceEnvelope, err := awscloud.NewResourceEnvelope(permissionObservation(boundary, permissionID, permission))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resourceEnvelope}
	for _, relationship := range permissionRelationships(boundary, permissionID, permission) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func settingsObservation(boundary awscloud.Boundary, settings Settings) awscloud.ResourceObservation {
	resourceID := "lakeformation-settings:" + strings.TrimSpace(boundary.AccountID) + ":" + strings.TrimSpace(boundary.Region)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLakeFormationSettings,
		Name:         resourceID,
		Attributes: map[string]any{
			"data_lake_admins":      cloneStringSlice(settings.Admins),
			"read_only_admins":      cloneStringSlice(settings.ReadOnlyAdmins),
			"data_lake_admin_count": len(cloneStringSlice(settings.Admins)),
			"read_only_admin_count": len(cloneStringSlice(settings.ReadOnlyAdmins)),
		},
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

func registeredResourceObservation(boundary awscloud.Boundary, resource RegisteredResource) awscloud.ResourceObservation {
	resourceARN := strings.TrimSpace(resource.ResourceARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          resourceARN,
		ResourceID:   resourceARN,
		ResourceType: awscloud.ResourceTypeLakeFormationResource,
		Name:         resourceARN,
		State:        strings.TrimSpace(resource.VerificationStatus),
		Attributes: map[string]any{
			"role_arn":                        strings.TrimSpace(resource.RoleARN),
			"hybrid_access_enabled":           resource.HybridAccessEnabled,
			"with_federation":                 resource.WithFederation,
			"with_privileged_access":          resource.WithPrivilegedAccess,
			"verification_status":             strings.TrimSpace(resource.VerificationStatus),
			"expected_resource_owner_account": strings.TrimSpace(resource.ExpectedResourceOwnerAccount),
			"last_modified":                   timeOrNil(resource.LastModified),
		},
		CorrelationAnchors: []string{resourceARN},
		SourceRecordID:     resourceARN,
	}
}

func permissionObservation(boundary awscloud.Boundary, permissionID string, permission Permission) awscloud.ResourceObservation {
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   permissionID,
		ResourceType: awscloud.ResourceTypeLakeFormationPermission,
		Name:         permissionID,
		Attributes: map[string]any{
			"principal_id":         strings.TrimSpace(permission.PrincipalID),
			"resource_kind":        strings.TrimSpace(permission.ResourceKind),
			"database_name":        strings.TrimSpace(permission.DatabaseName),
			"table_name":           strings.TrimSpace(permission.TableName),
			"table_wildcard":       permission.TableWildcard,
			"data_location_arn":    strings.TrimSpace(permission.DataLocationARN),
			"catalog_id":           strings.TrimSpace(permission.CatalogID),
			"privileges":           cloneStringSlice(permission.Privileges),
			"grantable_privileges": cloneStringSlice(permission.GrantablePrivileges),
			"last_updated":         timeOrNil(permission.LastUpdated),
		},
		CorrelationAnchors: []string{permissionID},
		SourceRecordID:     permissionID,
	}
}

// permissionResourceID builds a deterministic, boundary-stable identity for a
// permission grant from its principal and governed resource reference. Lake
// Formation does not assign grants an ARN, so the scanner synthesizes a bare
// identifier (never an ARN) keyed on the grant's natural identity, with the
// grant's (order-independent) privileges as a final disambiguator for grants
// that would otherwise collide, so the identity does not depend on API order.
func permissionResourceID(boundary awscloud.Boundary, permission Permission) string {
	var resourceRef string
	switch strings.TrimSpace(permission.ResourceKind) {
	case "table":
		resourceRef = "table:" + glueTableResourceID(permission.DatabaseName, permission.TableName)
		if permission.TableWildcard {
			resourceRef += ":*"
		}
	case "database":
		resourceRef = "database:" + strings.TrimSpace(permission.DatabaseName)
	case "data_location":
		resourceRef = "data_location:" + strings.TrimSpace(permission.DataLocationARN)
	default:
		resourceRef = strings.TrimSpace(permission.ResourceKind)
	}
	principal := strings.TrimSpace(permission.PrincipalID)
	return fmt.Sprintf(
		"lakeformation-permission:%s:%s:%s:%s:%s",
		strings.TrimSpace(boundary.AccountID),
		strings.TrimSpace(boundary.Region),
		principal,
		resourceRef,
		permissionPrivilegeKey(permission),
	)
}

// permissionPrivilegeKey renders the grant's privileges and grantable privileges
// as an order-independent string, so the synthesized permission identity depends
// only on the grant's natural fields (account, region, principal, resource, and
// privileges) rather than the ListPermissions page order. Two grants that share
// every natural field are the same grant and intentionally collapse to one id.
func permissionPrivilegeKey(permission Permission) string {
	privileges := append([]string(nil), permission.Privileges...)
	grantable := append([]string(nil), permission.GrantablePrivileges...)
	sort.Strings(privileges)
	sort.Strings(grantable)
	return strings.Join(privileges, ",") + "|" + strings.Join(grantable, ",")
}
