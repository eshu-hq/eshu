// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	if err := rows.Scan(
		&auth.TenantID,
		&auth.WorkspaceID,
		&auth.SubjectIDHash,
		&auth.PolicyRevisionHash,
	); err != nil {
		return SAMLExternalSubjectAuthContext{}, false, fmt.Errorf("resolve saml external subject: %w", err)
	}
	auth.SubjectClass = samlExternalSubjectClass
	auth.AllScopes = true
	return auth, true, rows.Err()
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
