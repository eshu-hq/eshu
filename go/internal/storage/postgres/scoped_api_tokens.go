// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ScopedAPITokenStore persists hash-only hosted API token registry rows.
type ScopedAPITokenStore struct {
	db ExecQueryer
}

// ScopedAPITokenRecord is a hash-only hosted API token registry row.
type ScopedAPITokenRecord struct {
	TokenHash          string
	TenantID           string
	WorkspaceID        string
	SubjectIDHash      string
	SubjectClass       string
	Status             string
	PolicyRevisionHash string
	IssuedAt           time.Time
	ExpiresAt          *time.Time
	RevokedAt          time.Time
	LastUsedAt         time.Time
	UpdatedAt          time.Time
}

// NewScopedAPITokenStore constructs a Postgres-backed scoped API token store.
func NewScopedAPITokenStore(db ExecQueryer) *ScopedAPITokenStore {
	return &ScopedAPITokenStore{db: db}
}

// ScopedAPITokenSchemaSQL returns the scoped API token registry DDL.
func ScopedAPITokenSchemaSQL() string {
	return scopedAPITokenSchemaSQL
}

func scopedAPITokenBootstrapDefinition() Definition {
	return Definition{
		Name: "scoped_api_tokens",
		Path: "schema/data-plane/postgres/006d_scoped_api_tokens.sql",
		SQL:  scopedAPITokenSchemaSQL,
	}
}

// ScopedAPITokenHash returns the durable hash used to look up a bearer token.
func ScopedAPITokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EnsureSchema applies the scoped API token registry schema.
func (s *ScopedAPITokenStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("scoped API token store database is required")
	}
	if _, err := s.db.ExecContext(ctx, scopedAPITokenSchemaSQL); err != nil {
		return fmt.Errorf("ensure scoped API token schema: %w", err)
	}
	return nil
}

// UpsertToken creates, rotates, revokes, or refreshes one token registry row.
func (s *ScopedAPITokenStore) UpsertToken(ctx context.Context, record ScopedAPITokenRecord) error {
	if s.db == nil {
		return errors.New("scoped API token store database is required")
	}
	record = normalizeScopedAPITokenRecord(record)
	if err := validateScopedAPITokenRecord(record); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertScopedAPITokenQuery,
		record.TokenHash,
		record.TenantID,
		record.WorkspaceID,
		record.SubjectIDHash,
		record.SubjectClass,
		record.Status,
		record.PolicyRevisionHash,
		record.IssuedAt,
		nullTimePtr(record.ExpiresAt),
		nullTime(record.RevokedAt),
		nullTime(record.LastUsedAt),
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upsert scoped API token: %w", err)
	}
	return nil
}

// ResolveTokenHash loads an active token row by hash without accepting raw
// token values.
func (s *ScopedAPITokenStore) ResolveTokenHash(
	ctx context.Context,
	tokenHash string,
	asOf time.Time,
) (ScopedAPITokenRecord, bool, error) {
	if s.db == nil {
		return ScopedAPITokenRecord{}, false, errors.New("scoped API token store database is required")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return ScopedAPITokenRecord{}, false, errors.New("token hash is required")
	}
	if asOf.IsZero() {
		return ScopedAPITokenRecord{}, false, errors.New("token lookup as_of is required")
	}
	rows, err := s.db.QueryContext(ctx, resolveScopedAPITokenQuery, tokenHash, asOf.UTC())
	if err != nil {
		return ScopedAPITokenRecord{}, false, fmt.Errorf("resolve scoped API token: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ScopedAPITokenRecord{}, false, fmt.Errorf("resolve scoped API token: %w", err)
		}
		return ScopedAPITokenRecord{}, false, nil
	}
	record, err := scanScopedAPIToken(rows)
	if err != nil {
		return ScopedAPITokenRecord{}, false, fmt.Errorf("resolve scoped API token: %w", err)
	}
	if err := rows.Err(); err != nil {
		return ScopedAPITokenRecord{}, false, fmt.Errorf("resolve scoped API token: %w", err)
	}
	return record, true, nil
}

// MarkTokenUsed records the last successful authentication timestamp.
func (s *ScopedAPITokenStore) MarkTokenUsed(ctx context.Context, tokenHash string, usedAt time.Time) error {
	if s.db == nil {
		return errors.New("scoped API token store database is required")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return errors.New("token hash is required")
	}
	if usedAt.IsZero() {
		return errors.New("token used_at is required")
	}
	if _, err := s.db.ExecContext(ctx, markScopedAPITokenUsedQuery, tokenHash, usedAt.UTC()); err != nil {
		return fmt.Errorf("mark scoped API token used: %w", err)
	}
	return nil
}

func normalizeScopedAPITokenRecord(record ScopedAPITokenRecord) ScopedAPITokenRecord {
	record.TokenHash = strings.TrimSpace(record.TokenHash)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.SubjectIDHash = strings.TrimSpace(record.SubjectIDHash)
	record.SubjectClass = strings.TrimSpace(record.SubjectClass)
	record.Status = strings.TrimSpace(record.Status)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.IssuedAt = record.IssuedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.RevokedAt = record.RevokedAt.UTC()
	record.LastUsedAt = record.LastUsedAt.UTC()
	if record.ExpiresAt != nil {
		expiresAt := record.ExpiresAt.UTC()
		record.ExpiresAt = &expiresAt
	}
	return record
}

func validateScopedAPITokenRecord(record ScopedAPITokenRecord) error {
	if blank(record.TokenHash) || blank(record.TenantID) || blank(record.WorkspaceID) ||
		blank(record.SubjectIDHash) || blank(record.SubjectClass) || blank(record.Status) ||
		blank(record.PolicyRevisionHash) {
		return errors.New("token hash, tenant, workspace, subject, status, and policy revision are required")
	}
	if record.IssuedAt.IsZero() {
		return errors.New("token issued_at is required")
	}
	if record.UpdatedAt.IsZero() {
		return errors.New("token updated_at is required")
	}
	if record.ExpiresAt != nil && !record.ExpiresAt.After(record.IssuedAt) {
		return errors.New("token expires_at must be after issued_at")
	}
	return nil
}

func scanScopedAPIToken(rows Rows) (ScopedAPITokenRecord, error) {
	var record ScopedAPITokenRecord
	var expiresAt, revokedAt, lastUsedAt sql.NullTime
	if err := rows.Scan(
		&record.TokenHash,
		&record.TenantID,
		&record.WorkspaceID,
		&record.SubjectIDHash,
		&record.SubjectClass,
		&record.Status,
		&record.PolicyRevisionHash,
		&record.IssuedAt,
		&expiresAt,
		&revokedAt,
		&lastUsedAt,
	); err != nil {
		return ScopedAPITokenRecord{}, err
	}
	record.ExpiresAt = timePtrFromNull(expiresAt)
	record.RevokedAt = timeFromNull(revokedAt)
	record.LastUsedAt = timeFromNull(lastUsedAt)
	return record, nil
}

func timeFromNull(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}
