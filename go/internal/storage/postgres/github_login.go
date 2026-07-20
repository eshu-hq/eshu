// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GitHubLoginStore persists hash-only GitHub OAuth2 login state (issue
// #5166, F-5). Unlike OIDCLoginStore it does not resolve group/team grants
// itself: identity_provider_group_role_mappings has no provider_kind
// column, so team→role grant resolution reuses OIDCLoginStore.
// ResolveGroupRoleGrants unchanged — see cmd/api's github_login.go wiring —
// rather than duplicating that SQL for a second provider kind.
type GitHubLoginStore struct {
	db ExecQueryer
}

// GitHubLoginStateRecord is one server-side GitHub OAuth2 state row. There
// is no NonceHash field: plain OAuth2 has no ID token and no nonce concept.
type GitHubLoginStateRecord struct {
	StateHash        string
	ProviderConfigID string
	ProviderKeyHash  string
	IssuerHash       string
	ClientIDHash     string
	TenantID         string
	WorkspaceID      string
	RedirectURIHash  string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}

// NewGitHubLoginStore constructs a Postgres GitHub login state store.
func NewGitHubLoginStore(db ExecQueryer) *GitHubLoginStore {
	return &GitHubLoginStore{db: db}
}

// GitHubLoginSchemaSQL returns the GitHub login state DDL.
func GitHubLoginSchemaSQL() string {
	return githubLoginSchemaSQL
}

// EnsureSchema applies the GitHub login schema.
func (s *GitHubLoginStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("github login store database is required")
	}
	if _, err := s.db.ExecContext(ctx, githubLoginSchemaSQL); err != nil {
		return fmt.Errorf("ensure github login schema: %w", err)
	}
	return nil
}

// CreateState writes one hash-only GitHub login state row.
func (s *GitHubLoginStore) CreateState(ctx context.Context, record GitHubLoginStateRecord) error {
	if s.db == nil {
		return errors.New("github login store database is required")
	}
	record = normalizeGitHubLoginState(record)
	if err := validateGitHubLoginState(record); err != nil {
		return err
	}
	result, err := s.db.ExecContext(
		ctx,
		createGitHubLoginStateQuery,
		record.StateHash,
		record.ProviderConfigID,
		record.ProviderKeyHash,
		record.IssuerHash,
		record.ClientIDHash,
		record.TenantID,
		record.WorkspaceID,
		record.RedirectURIHash,
		record.ReturnToPath,
		record.IssuedAt,
		record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create github login state: %w", err)
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err == nil && affected == 0 {
			return errors.New("active github provider config is required to create login state")
		}
	}
	return nil
}

// ConsumeState atomically marks an unexpired state row consumed and returns it.
func (s *GitHubLoginStore) ConsumeState(
	ctx context.Context,
	stateHash string,
	consumedAt time.Time,
) (GitHubLoginStateRecord, bool, error) {
	if s.db == nil {
		return GitHubLoginStateRecord{}, false, errors.New("github login store database is required")
	}
	stateHash = strings.TrimSpace(stateHash)
	consumedAt = consumedAt.UTC()
	if stateHash == "" || consumedAt.IsZero() {
		return GitHubLoginStateRecord{}, false, errors.New("github state hash and consumed_at are required")
	}
	rows, err := s.db.QueryContext(ctx, consumeGitHubLoginStateQuery, stateHash, consumedAt)
	if err != nil {
		return GitHubLoginStateRecord{}, false, fmt.Errorf("consume github login state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return GitHubLoginStateRecord{}, false, fmt.Errorf("consume github login state: %w", err)
		}
		return GitHubLoginStateRecord{}, false, nil
	}
	record, err := scanGitHubLoginState(rows)
	if err != nil {
		return GitHubLoginStateRecord{}, false, fmt.Errorf("consume github login state: %w", err)
	}
	if err := rows.Err(); err != nil {
		return GitHubLoginStateRecord{}, false, fmt.Errorf("consume github login state: %w", err)
	}
	return record, true, nil
}

func normalizeGitHubLoginState(record GitHubLoginStateRecord) GitHubLoginStateRecord {
	record.StateHash = strings.TrimSpace(record.StateHash)
	record.ProviderConfigID = strings.TrimSpace(record.ProviderConfigID)
	record.ProviderKeyHash = strings.TrimSpace(record.ProviderKeyHash)
	record.IssuerHash = strings.TrimSpace(record.IssuerHash)
	record.ClientIDHash = strings.TrimSpace(record.ClientIDHash)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.RedirectURIHash = strings.TrimSpace(record.RedirectURIHash)
	record.ReturnToPath = strings.TrimSpace(record.ReturnToPath)
	record.IssuedAt = record.IssuedAt.UTC()
	record.ExpiresAt = record.ExpiresAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func validateGitHubLoginState(record GitHubLoginStateRecord) error {
	if blank(record.StateHash) || blank(record.ProviderConfigID) ||
		blank(record.ProviderKeyHash) || blank(record.IssuerHash) || blank(record.ClientIDHash) ||
		blank(record.TenantID) || blank(record.WorkspaceID) || blank(record.RedirectURIHash) {
		return errors.New("github state hash, provider hashes, tenant, workspace, and redirect uri hash are required")
	}
	if record.IssuedAt.IsZero() || record.ExpiresAt.IsZero() {
		return errors.New("github state issued and expiry timestamps are required")
	}
	if !record.ExpiresAt.After(record.IssuedAt) {
		return errors.New("github state expires_at must be after issued_at")
	}
	return nil
}

func scanGitHubLoginState(rows Rows) (GitHubLoginStateRecord, error) {
	var record GitHubLoginStateRecord
	if err := rows.Scan(
		&record.StateHash,
		&record.ProviderConfigID,
		&record.TenantID,
		&record.WorkspaceID,
		&record.RedirectURIHash,
		&record.ReturnToPath,
		&record.IssuedAt,
		&record.ExpiresAt,
		&record.UpdatedAt,
	); err != nil {
		return GitHubLoginStateRecord{}, err
	}
	return normalizeGitHubLoginState(record), nil
}
