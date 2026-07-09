// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
)

// ResolveBootstrapCredentialOwner resolves the owning user_id and
// subject_id_hash for the bootstrap credential row at (tenantID,
// workspaceID). It is the read-only counterpart ResetBootstrapCredential
// already uses internally (selectBootstrapCredentialSubject then
// selectBootstrapCredentialOwnerUserID), exported for the first-run setup
// wizard (#4965): once a caller has opened the sealed envelope and verified
// the submitted plaintext, it still needs the owning identity to rotate the
// password and MFA factors via ResetLocalIdentityPassword/ResetLocalIdentityMFA.
// Returns ErrBootstrapCredentialNotFound when no credential row exists for
// the tenant/workspace, matching ResetBootstrapCredential's failure mode.
func (s *IdentitySubjectStore) ResolveBootstrapCredentialOwner(
	ctx context.Context,
	tenantID, workspaceID string,
) (userID string, subjectIDHash string, err error) {
	if s.db == nil {
		return "", "", errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	if tenantID == "" || workspaceID == "" {
		return "", "", errors.New("resolve bootstrap credential owner requires tenant_id and workspace_id")
	}
	subjectIDHash, err = selectBootstrapCredentialSubject(ctx, s.db, tenantID, workspaceID)
	if err != nil {
		return "", "", err
	}
	userID, err = selectBootstrapCredentialOwnerUserID(ctx, s.db, subjectIDHash)
	if err != nil {
		return "", "", err
	}
	return userID, subjectIDHash, nil
}
