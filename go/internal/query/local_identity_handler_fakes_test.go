// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// fakeLocalIdentityStore is the shared LocalIdentityProfileLister test
// double for local_identity_handler_test.go, local_identity_api_tokens_test.go,
// local_identity_totp_test.go, and browser_session_authz_permission_test.go.
// Split into its own file (from local_identity_handler_test.go) to stay
// under the repo's 500-line file cap.
type fakeLocalIdentityStore struct {
	bootstrap       LocalIdentityBootstrapRecord
	attempt         LocalIdentityAuthenticationAttempt
	authResult      LocalIdentityAuthenticationResult
	invitation      LocalIdentityInvitationRecord
	acceptance      LocalIdentityInvitationAcceptance
	passwordReset   LocalIdentityPasswordReset
	mfaReset        LocalIdentityMFAReset
	disable         LocalIdentityDisableUser
	breakGlass      LocalIdentityBreakGlassWindow
	breakGlassAuth  LocalIdentityAuthContext
	breakGlassError error
	createdAPIToken LocalIdentityAPITokenCreate
	revokedAPIToken LocalIdentityAPITokenRevoke
	rotatedAPIToken LocalIdentityAPITokenRotate
	// revokeAPITokenError / rotateAPITokenError let a test drive the
	// self-service not-owned path (issue #5164): the owner-scoped store
	// returns ErrLocalIdentityAPITokenNotFound when the caller does not own
	// the token, which the handler must translate into a 404.
	revokeAPITokenError error
	rotateAPITokenError error
	// apiTokens accumulates every CreateLocalIdentityAPIToken call so
	// ListAPITokensBySubject can prove real create-then-list wiring instead of
	// always returning nil.
	apiTokens      []LocalIdentityAPITokenCreate
	rotation       LocalIdentityPasswordRotation
	rotationResult LocalIdentityAuthenticationResult
	rotationError  error

	resolvedUserID      string
	resolvedUserIDFound bool
	resolvedUserIDError error
	totpBegin           LocalIdentityTOTPEnrollmentBegin
	totpBeginError      error
	totpConfirm         LocalIdentityTOTPEnrollmentConfirm
	totpConfirmError    error
}

func (s *fakeLocalIdentityStore) BootstrapLocalIdentity(
	_ context.Context,
	record LocalIdentityBootstrapRecord,
) error {
	s.bootstrap = record
	return nil
}

func (s *fakeLocalIdentityStore) AuthenticateLocalIdentity(
	_ context.Context,
	attempt LocalIdentityAuthenticationAttempt,
) (LocalIdentityAuthenticationResult, error) {
	s.attempt = attempt
	return s.authResult, nil
}

func (s *fakeLocalIdentityStore) CreateLocalIdentityInvitation(
	_ context.Context,
	record LocalIdentityInvitationRecord,
) error {
	s.invitation = record
	return nil
}

func (s *fakeLocalIdentityStore) AcceptLocalIdentityInvitation(
	_ context.Context,
	acceptance LocalIdentityInvitationAcceptance,
) error {
	s.acceptance = acceptance
	return nil
}

func (s *fakeLocalIdentityStore) ResetLocalIdentityPassword(
	_ context.Context,
	reset LocalIdentityPasswordReset,
) error {
	s.passwordReset = reset
	return nil
}

func (s *fakeLocalIdentityStore) RotateLocalIdentityPassword(
	_ context.Context,
	rotation LocalIdentityPasswordRotation,
) (LocalIdentityAuthenticationResult, error) {
	s.rotation = rotation
	return s.rotationResult, s.rotationError
}

func (s *fakeLocalIdentityStore) ResetLocalIdentityMFA(_ context.Context, reset LocalIdentityMFAReset) error {
	s.mfaReset = reset
	return nil
}

func (s *fakeLocalIdentityStore) DisableLocalIdentityUser(_ context.Context, disable LocalIdentityDisableUser) error {
	s.disable = disable
	return nil
}

func (s *fakeLocalIdentityStore) EnableLocalIdentityBreakGlass(
	_ context.Context,
	window LocalIdentityBreakGlassWindow,
) error {
	s.breakGlass = window
	return nil
}

func (s *fakeLocalIdentityStore) ResolveLocalIdentityBreakGlass(
	_ context.Context,
	_ LocalIdentityBreakGlassAttempt,
) (LocalIdentityAuthContext, error) {
	return s.breakGlassAuth, s.breakGlassError
}

func (s *fakeLocalIdentityStore) CreateLocalIdentityAPIToken(
	_ context.Context,
	token LocalIdentityAPITokenCreate,
) error {
	s.createdAPIToken = token
	s.apiTokens = append(s.apiTokens, token)
	return nil
}

func (s *fakeLocalIdentityStore) RevokeLocalIdentityAPIToken(
	_ context.Context,
	revoke LocalIdentityAPITokenRevoke,
) error {
	s.revokedAPIToken = revoke
	return s.revokeAPITokenError
}

func (s *fakeLocalIdentityStore) RotateLocalIdentityAPIToken(
	_ context.Context,
	rotate LocalIdentityAPITokenRotate,
) error {
	s.rotatedAPIToken = rotate
	return s.rotateAPITokenError
}

// ListAPITokensBySubject returns metadata built from every token this fake has
// recorded via CreateLocalIdentityAPIToken, so tests can prove real
// create-then-list wiring (including DisplayLabel) through one handler mux
// rather than asserting against a hand-built stand-in.
func (s *fakeLocalIdentityStore) ListAPITokensBySubject(
	_ context.Context,
	_ string,
	_ time.Time,
) ([]LocalIdentityAPITokenListItem, error) {
	if len(s.apiTokens) == 0 {
		return nil, nil
	}
	items := make([]LocalIdentityAPITokenListItem, 0, len(s.apiTokens))
	for _, token := range s.apiTokens {
		items = append(items, LocalIdentityAPITokenListItem{
			TokenID:      token.TokenID,
			TokenClass:   token.TokenClass,
			DisplayLabel: token.DisplayLabel,
			IssuedAt:     token.IssuedAt,
			ExpiresAt:    token.ExpiresAt,
		})
	}
	return items, nil
}

func (s *fakeLocalIdentityStore) GetLocalIdentityMFAStatus(
	_ context.Context,
	_ string,
	_ time.Time,
) (LocalIdentityMFAStatus, error) {
	return LocalIdentityMFAStatus{}, nil
}

func (s *fakeLocalIdentityStore) ResolveLocalIdentityUserID(
	_ context.Context,
	_ string,
) (string, bool, error) {
	return s.resolvedUserID, s.resolvedUserIDFound, s.resolvedUserIDError
}

func (s *fakeLocalIdentityStore) BeginLocalIdentityTOTPEnrollment(
	_ context.Context,
	begin LocalIdentityTOTPEnrollmentBegin,
) error {
	s.totpBegin = begin
	return s.totpBeginError
}

func (s *fakeLocalIdentityStore) ConfirmLocalIdentityTOTPEnrollment(
	_ context.Context,
	confirm LocalIdentityTOTPEnrollmentConfirm,
) error {
	s.totpConfirm = confirm
	return s.totpConfirmError
}
