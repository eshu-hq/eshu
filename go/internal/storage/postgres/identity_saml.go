// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const samlExternalSubjectClass = "external_saml"

// ResolveSAMLExternalSubject resolves a SAML subject through durable identity
// provider, membership, role, and grant state using hash-only inputs.
func (s *IdentitySubjectStore) ResolveSAMLExternalSubject(
	ctx context.Context,
	request SAMLExternalSubjectResolutionRequest,
) (SAMLExternalSubjectResolutionResult, error) {
	if s.db == nil {
		return SAMLExternalSubjectResolutionResult{}, errors.New("identity subject store database is required")
	}
	request = normalizeSAMLExternalSubjectResolutionRequest(request)
	if request.ProviderConfigID == "" || request.ExternalSubjectIDHash == "" || request.GroupClaimsHash == "" {
		return SAMLExternalSubjectResolutionResult{}, nil
	}
	if request.Now.IsZero() {
		return SAMLExternalSubjectResolutionResult{}, errors.New("saml external subject resolution time is required")
	}

	auth, ok, err := resolveActiveSAMLExternalSubject(ctx, s.db, request)
	if err != nil {
		return SAMLExternalSubjectResolutionResult{}, err
	}
	if ok {
		return SAMLExternalSubjectResolutionResult{
			Auth:         auth,
			Resolved:     true,
			KnownSubject: true,
		}, nil
	}
	known, err := selectKnownSAMLExternalSubject(ctx, s.db, request)
	if err != nil {
		return SAMLExternalSubjectResolutionResult{}, err
	}
	return SAMLExternalSubjectResolutionResult{KnownSubject: known}, nil
}

// HasActiveSAMLProviderConfig reports whether a SAML provider config is active
// in durable identity state and belongs to an active tenant.
func (s *IdentitySubjectStore) HasActiveSAMLProviderConfig(
	ctx context.Context,
	providerConfigID string,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	if providerConfigID == "" {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx, selectActiveSAMLProviderConfigQuery, providerConfigID)
	if err != nil {
		return false, fmt.Errorf("select active saml provider config: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("select active saml provider config: %w", err)
		}
		return false, nil
	}
	var activeProviderConfigID string
	if err := rows.Scan(&activeProviderConfigID); err != nil {
		return false, fmt.Errorf("select active saml provider config: %w", err)
	}
	return strings.TrimSpace(activeProviderConfigID) == providerConfigID, rows.Err()
}

func normalizeSAMLExternalSubjectResolutionRequest(
	request SAMLExternalSubjectResolutionRequest,
) SAMLExternalSubjectResolutionRequest {
	request.ProviderConfigID = strings.TrimSpace(request.ProviderConfigID)
	request.ExternalSubjectIDHash = strings.TrimSpace(request.ExternalSubjectIDHash)
	request.GroupClaimsHash = strings.TrimSpace(request.GroupClaimsHash)
	return request
}

func resolveActiveSAMLExternalSubject(
	ctx context.Context,
	db Queryer,
	request SAMLExternalSubjectResolutionRequest,
) (SAMLExternalSubjectAuthContext, bool, error) {
	rows, err := db.QueryContext(
		ctx,
		resolveSAMLExternalSubjectQuery,
		request.ProviderConfigID,
		request.ExternalSubjectIDHash,
		request.GroupClaimsHash,
		request.Now,
	)
	if err != nil {
		return SAMLExternalSubjectAuthContext{}, false, fmt.Errorf("resolve saml external subject: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return SAMLExternalSubjectAuthContext{}, false, fmt.Errorf("resolve saml external subject: %w", err)
		}
		return SAMLExternalSubjectAuthContext{}, false, nil
	}
	var auth SAMLExternalSubjectAuthContext
	var userID string
	var hasAdminRole bool
	if err := rows.Scan(
		&auth.TenantID,
		&auth.WorkspaceID,
		&auth.SubjectIDHash,
		&auth.PolicyRevisionHash,
		&userID,
		&hasAdminRole,
	); err != nil {
		return SAMLExternalSubjectAuthContext{}, false, fmt.Errorf("resolve saml external subject: %w", err)
	}
	if err := rows.Err(); err != nil {
		return SAMLExternalSubjectAuthContext{}, false, fmt.Errorf("resolve saml external subject: %w", err)
	}
	auth.SubjectClass = samlExternalSubjectClass
	// Admin sessions (owner/tenant_admin role) stay fail-open exactly as
	// local and OIDC sessions do: no enforcement snapshot is attached.
	if hasAdminRole {
		auth.AllScopes = true
		return auth, true, nil
	}
	// Non-admin sessions must carry a permission-catalog snapshot so the
	// catalog enforces them identically to a scoped token for the same roles.
	roles, err := resolveSAMLExternalSubjectRoles(ctx, db, auth.TenantID, auth.WorkspaceID, userID, request.Now)
	if err != nil {
		// Fails closed (no session issued). Log distinctly so an operator can
		// tell a permission-catalog resolution outage from any other login 500.
		slog.ErrorContext(ctx, "saml session role resolution failed; login denied",
			"subject_class", samlExternalSubjectClass, "tenant_id", auth.TenantID, "error", err)
		return SAMLExternalSubjectAuthContext{}, false, err
	}
	features, dataClasses, err := resolvePermissionGrantsForRoles(ctx, db, auth.TenantID, roles, request.Now)
	if err != nil {
		slog.ErrorContext(ctx, "saml session permission grant resolution failed; login denied",
			"subject_class", samlExternalSubjectClass, "tenant_id", auth.TenantID, "role_count", len(roles), "error", err)
		return SAMLExternalSubjectAuthContext{}, false, err
	}
	// RoleIDs may include roles whose grants are entirely expired; the
	// authoritative enforcement surface is AllowedPermissionFeatures and
	// AllowedPermissionDataClasses, not RoleIDs.
	auth.RoleIDs = roles
	auth.PermissionCatalogEnforced = true
	auth.AllowedPermissionFeatures = features
	auth.AllowedPermissionDataClasses = dataClasses
	return auth, true, nil
}

// resolveSAMLExternalSubjectRoles returns the active membership role IDs for
// one SAML-mapped user within a tenant/workspace as of the given time. It
// reuses the same query as local identity resolution so a SAML session and a
// scoped token for the same user resolve to the same role set.
func resolveSAMLExternalSubjectRoles(
	ctx context.Context,
	db Queryer,
	tenantID string,
	workspaceID string,
	userID string,
	asOf time.Time,
) ([]string, error) {
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || workspaceID == "" || userID == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(
		ctx,
		resolveLocalIdentityRolesQuery,
		tenantID,
		workspaceID,
		userID,
		asOf.UTC(), // normalize to UTC so the active window matches the resolution query
		maxOIDCGrantLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve saml external subject roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	roles := make([]string, 0)
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, fmt.Errorf("resolve saml external subject roles: %w", err)
		}
		roles = append(roles, roleID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve saml external subject roles: %w", err)
	}
	return cleanBrowserSessionStrings(roles), nil
}

func selectKnownSAMLExternalSubject(
	ctx context.Context,
	db Queryer,
	request SAMLExternalSubjectResolutionRequest,
) (bool, error) {
	rows, err := db.QueryContext(
		ctx,
		selectKnownSAMLExternalSubjectQuery,
		request.ProviderConfigID,
		request.ExternalSubjectIDHash,
	)
	if err != nil {
		return false, fmt.Errorf("select known saml external subject: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("select known saml external subject: %w", err)
		}
		return false, nil
	}
	var externalIdentityID string
	if err := rows.Scan(&externalIdentityID); err != nil {
		return false, fmt.Errorf("select known saml external subject: %w", err)
	}
	return strings.TrimSpace(externalIdentityID) != "", rows.Err()
}
