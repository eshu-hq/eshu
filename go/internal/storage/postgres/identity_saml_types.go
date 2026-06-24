// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "time"

// SAMLExternalSubjectResolutionRequest identifies one hash-only SAML subject
// and its current group-claim digest for durable authorization resolution.
type SAMLExternalSubjectResolutionRequest struct {
	ProviderConfigID      string
	ExternalSubjectIDHash string
	GroupClaimsHash       string
	Now                   time.Time
}

// SAMLExternalSubjectAuthContext is the durable authorization context resolved
// from identity provider, subject, membership, role, and grant state.
type SAMLExternalSubjectAuthContext struct {
	TenantID           string
	WorkspaceID        string
	SubjectIDHash      string
	SubjectClass       string
	PolicyRevisionHash string
	AllScopes          bool
}

// SAMLExternalSubjectResolutionResult distinguishes unmapped subjects from
// known subjects that were denied by current group, membership, role, or grant
// state so callers can fail closed instead of using bootstrap fallbacks.
type SAMLExternalSubjectResolutionResult struct {
	Auth         SAMLExternalSubjectAuthContext
	Resolved     bool
	KnownSubject bool
}
