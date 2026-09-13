// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// fakeStore is the shared IdentityProfileLister test double for this
// package's own route-coverage tests (#6642: the family moved here from
// package query, and its route handler tests could not move with it because
// almost every existing test depended on root-private fixtures shared across
// other families -- see the package doc.go's Move evidence). It mirrors the
// shape of root's own fakeLocalIdentityStore (identity_handler_fakes_test.go)
// but is declared fresh here: an unexported test double in a _test.go file is
// not part of the importable package, so it cannot be shared across the
// package boundary either direction.
type fakeStore struct {
	bootstrap       IdentityBootstrapRecord
	attempt         IdentityAuthenticationAttempt
	authResult      IdentityAuthenticationResult
	invitation      IdentityInvitationRecord
	acceptance      IdentityInvitationAcceptance
	passwordReset   IdentityPasswordReset
	mfaReset        IdentityMFAReset
	disable         IdentityDisableUser
	breakGlass      IdentityBreakGlassWindow
	breakGlassAuth  IdentityAuthContext
	breakGlassError error
	createdAPIToken IdentityAPITokenCreate
	revokedAPIToken IdentityAPITokenRevoke
	rotatedAPIToken IdentityAPITokenRotate
	// revokeAPITokenError / rotateAPITokenError let a test drive the
	// self-service not-owned path (issue #5164): the owner-scoped store
	// returns ErrIdentityAPITokenNotFound when the caller does not own the
	// token, which the handler must translate into a 404.
	revokeAPITokenError error
	rotateAPITokenError error
	// apiTokens accumulates every CreateLocalIdentityAPIToken call so
	// ListAPITokensBySubject can prove real create-then-list wiring instead of
	// always returning nil.
	apiTokens      []IdentityAPITokenCreate
	rotation       IdentityPasswordRotation
	rotationResult IdentityAuthenticationResult
	rotationError  error

	resolvedUserID      string
	resolvedUserIDFound bool
	resolvedUserIDError error
	totpBegin           IdentityTOTPEnrollmentBegin
	totpBeginError      error
	totpConfirm         IdentityTOTPEnrollmentConfirm
	totpConfirmError    error
}

func (s *fakeStore) BootstrapLocalIdentity(_ context.Context, record IdentityBootstrapRecord) error {
	s.bootstrap = record
	return nil
}

func (s *fakeStore) AuthenticateLocalIdentity(
	_ context.Context,
	attempt IdentityAuthenticationAttempt,
) (IdentityAuthenticationResult, error) {
	s.attempt = attempt
	return s.authResult, nil
}

func (s *fakeStore) CreateLocalIdentityInvitation(_ context.Context, record IdentityInvitationRecord) error {
	s.invitation = record
	return nil
}

func (s *fakeStore) AcceptLocalIdentityInvitation(_ context.Context, acceptance IdentityInvitationAcceptance) error {
	s.acceptance = acceptance
	return nil
}

func (s *fakeStore) ResetLocalIdentityPassword(_ context.Context, reset IdentityPasswordReset) error {
	s.passwordReset = reset
	return nil
}

func (s *fakeStore) RotateLocalIdentityPassword(
	_ context.Context,
	rotation IdentityPasswordRotation,
) (IdentityAuthenticationResult, error) {
	s.rotation = rotation
	return s.rotationResult, s.rotationError
}

func (s *fakeStore) ResetLocalIdentityMFA(_ context.Context, reset IdentityMFAReset) error {
	s.mfaReset = reset
	return nil
}

func (s *fakeStore) DisableLocalIdentityUser(_ context.Context, disable IdentityDisableUser) error {
	s.disable = disable
	return nil
}

func (s *fakeStore) EnableLocalIdentityBreakGlass(_ context.Context, window IdentityBreakGlassWindow) error {
	s.breakGlass = window
	return nil
}

func (s *fakeStore) ResolveLocalIdentityBreakGlass(
	_ context.Context,
	_ IdentityBreakGlassAttempt,
) (IdentityAuthContext, error) {
	return s.breakGlassAuth, s.breakGlassError
}

func (s *fakeStore) CreateLocalIdentityAPIToken(_ context.Context, token IdentityAPITokenCreate) error {
	s.createdAPIToken = token
	s.apiTokens = append(s.apiTokens, token)
	return nil
}

func (s *fakeStore) RevokeLocalIdentityAPIToken(_ context.Context, revoke IdentityAPITokenRevoke) error {
	s.revokedAPIToken = revoke
	return s.revokeAPITokenError
}

func (s *fakeStore) RotateLocalIdentityAPIToken(_ context.Context, rotate IdentityAPITokenRotate) error {
	s.rotatedAPIToken = rotate
	return s.rotateAPITokenError
}

// ListAPITokensBySubject returns metadata built from every token this fake has
// recorded via CreateLocalIdentityAPIToken, so tests can prove real
// create-then-list wiring (including DisplayLabel) through one handler mux
// rather than asserting against a hand-built stand-in.
func (s *fakeStore) ListAPITokensBySubject(
	_ context.Context,
	_ string,
	_ time.Time,
) ([]IdentityAPITokenListItem, error) {
	if len(s.apiTokens) == 0 {
		return nil, nil
	}
	items := make([]IdentityAPITokenListItem, 0, len(s.apiTokens))
	for _, token := range s.apiTokens {
		items = append(items, IdentityAPITokenListItem{
			TokenID:      token.TokenID,
			TokenClass:   token.TokenClass,
			DisplayLabel: token.DisplayLabel,
			IssuedAt:     token.IssuedAt,
			ExpiresAt:    token.ExpiresAt,
		})
	}
	return items, nil
}

func (s *fakeStore) GetLocalIdentityMFAStatus(_ context.Context, _ string, _ time.Time) (IdentityMFAStatus, error) {
	return IdentityMFAStatus{}, nil
}

func (s *fakeStore) ResolveLocalIdentityUserID(_ context.Context, _ string) (string, bool, error) {
	return s.resolvedUserID, s.resolvedUserIDFound, s.resolvedUserIDError
}

func (s *fakeStore) BeginLocalIdentityTOTPEnrollment(_ context.Context, begin IdentityTOTPEnrollmentBegin) error {
	s.totpBegin = begin
	return s.totpBeginError
}

func (s *fakeStore) ConfirmLocalIdentityTOTPEnrollment(_ context.Context, confirm IdentityTOTPEnrollmentConfirm) error {
	s.totpConfirm = confirm
	return s.totpConfirmError
}

// fakeSessions is a minimal queryauth.BrowserSessionStore double: it records
// the one CreateBrowserSession call a login/break-glass/rotation route makes
// and reports success. RevokeBrowserSession and SwitchBrowserSessionWorkspace
// are not exercised by this family's routes.
type fakeSessions struct {
	created   queryauth.BrowserSessionCreateRecord
	createErr error
}

func (f *fakeSessions) CreateBrowserSession(_ context.Context, record queryauth.BrowserSessionCreateRecord) error {
	f.created = record
	return f.createErr
}

func (f *fakeSessions) RevokeBrowserSession(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (f *fakeSessions) SwitchBrowserSessionWorkspace(
	_ context.Context, _, _, _ string, _ time.Time,
) (queryauth.AuthContext, bool, error) {
	return queryauth.AuthContext{}, false, nil
}

// sequenceSecrets returns a deterministic NewSecret double that hands out
// values in order, mirroring root's own sequenceSecrets
// (browser_session_handler_test.go) which this package cannot reach directly.
func sequenceSecrets(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := values[index]
		index++
		return value, nil
	}
}
