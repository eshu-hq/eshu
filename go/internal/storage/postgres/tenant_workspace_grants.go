// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultTenantWorkspaceGrantLimit = 100
	maxTenantWorkspaceGrantLimit     = 1000
)

// TenantWorkspaceGrantStore persists hosted tenant, workspace, and grant state.
type TenantWorkspaceGrantStore struct {
	db ExecQueryer
}

// TenantRecord is the durable hosted tenant state row.
type TenantRecord struct {
	TenantID           string
	Status             string
	DisplayHandleHash  string
	PolicyRevisionHash string
	UpdatedAt          time.Time
	TombstonedAt       time.Time
}

// WorkspaceRecord is the durable hosted workspace state row.
type WorkspaceRecord struct {
	TenantID           string
	WorkspaceID        string
	Status             string
	DisplayHandleHash  string
	PolicyRevisionHash string
	UpdatedAt          time.Time
	TombstonedAt       time.Time
}

// TenantScopeGrant authorizes one subject class to use one ingestion scope.
type TenantScopeGrant struct {
	TenantID           string
	WorkspaceID        string
	ScopeID            string
	SubjectClass       string
	GrantSource        string
	PolicyRevisionHash string
	EffectiveAt        time.Time
	ExpiresAt          *time.Time
	TombstonedAt       time.Time
	UpdatedAt          time.Time
}

// TenantRepositoryGrant narrows repository reads to a granted scope boundary.
type TenantRepositoryGrant struct {
	TenantID           string
	WorkspaceID        string
	RepoID             string
	ScopeID            string
	SubjectClass       string
	GrantSource        string
	PolicyRevisionHash string
	EffectiveAt        time.Time
	ExpiresAt          *time.Time
	TombstonedAt       time.Time
	UpdatedAt          time.Time
}

// TenantWorkspaceGrantQuery bounds active grant reads to one tenant/workspace.
type TenantWorkspaceGrantQuery struct {
	TenantID     string
	WorkspaceID  string
	SubjectClass string
	ScopeIDs     []string
	AsOf         time.Time
	Limit        int
}

// NewTenantWorkspaceGrantStore constructs a Postgres tenant grant store.
func NewTenantWorkspaceGrantStore(db ExecQueryer) *TenantWorkspaceGrantStore {
	return &TenantWorkspaceGrantStore{db: db}
}

// TenantWorkspaceGrantSchemaSQL returns hosted tenant/workspace grant DDL.
func TenantWorkspaceGrantSchemaSQL() string {
	return tenantWorkspaceGrantSchemaSQL
}

// EnsureSchema applies the tenant/workspace grant schema.
func (s *TenantWorkspaceGrantStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("tenant workspace grant store database is required")
	}
	if _, err := s.db.ExecContext(ctx, tenantWorkspaceGrantSchemaSQL); err != nil {
		return fmt.Errorf("ensure tenant workspace grant schema: %w", err)
	}
	return nil
}

// UpsertTenant creates or updates one hosted tenant state row.
func (s *TenantWorkspaceGrantStore) UpsertTenant(ctx context.Context, record TenantRecord) error {
	if s.db == nil {
		return errors.New("tenant workspace grant store database is required")
	}
	record = normalizeTenantRecord(record)
	if err := validateTenantRecord(record); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertTenantRecordQuery,
		record.TenantID,
		record.Status,
		record.DisplayHandleHash,
		record.PolicyRevisionHash,
		record.UpdatedAt,
		nullTime(record.TombstonedAt),
	); err != nil {
		return fmt.Errorf("upsert tenant record: %w", err)
	}
	return nil
}

// UpsertWorkspace creates or updates one hosted workspace state row.
func (s *TenantWorkspaceGrantStore) UpsertWorkspace(ctx context.Context, record WorkspaceRecord) error {
	if s.db == nil {
		return errors.New("tenant workspace grant store database is required")
	}
	record = normalizeWorkspaceRecord(record)
	if err := validateWorkspaceRecord(record); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertWorkspaceRecordQuery,
		record.TenantID,
		record.WorkspaceID,
		record.Status,
		record.DisplayHandleHash,
		record.PolicyRevisionHash,
		record.UpdatedAt,
		nullTime(record.TombstonedAt),
	); err != nil {
		return fmt.Errorf("upsert workspace record: %w", err)
	}
	return nil
}

// UpsertScopeGrant creates, refreshes, or tombstones one scope grant.
func (s *TenantWorkspaceGrantStore) UpsertScopeGrant(ctx context.Context, grant TenantScopeGrant) error {
	if s.db == nil {
		return errors.New("tenant workspace grant store database is required")
	}
	grant = normalizeScopeGrant(grant)
	if err := validateScopeGrant(grant); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertTenantScopeGrantQuery,
		grant.TenantID,
		grant.WorkspaceID,
		grant.ScopeID,
		grant.SubjectClass,
		grant.GrantSource,
		grant.PolicyRevisionHash,
		grant.EffectiveAt,
		nullTimePtr(grant.ExpiresAt),
		nullTime(grant.TombstonedAt),
		grant.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upsert tenant scope grant: %w", err)
	}
	return nil
}

// UpsertRepositoryGrant creates, refreshes, or tombstones one repository grant.
func (s *TenantWorkspaceGrantStore) UpsertRepositoryGrant(ctx context.Context, grant TenantRepositoryGrant) error {
	if s.db == nil {
		return errors.New("tenant workspace grant store database is required")
	}
	grant = normalizeRepositoryGrant(grant)
	if err := validateRepositoryGrant(grant); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertTenantRepositoryGrantQuery,
		grant.TenantID,
		grant.WorkspaceID,
		grant.RepoID,
		grant.ScopeID,
		grant.SubjectClass,
		grant.GrantSource,
		grant.PolicyRevisionHash,
		grant.EffectiveAt,
		nullTimePtr(grant.ExpiresAt),
		nullTime(grant.TombstonedAt),
		grant.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upsert tenant repository grant: %w", err)
	}
	return nil
}

// ListScopeGrants returns active scope grants for one tenant/workspace boundary.
func (s *TenantWorkspaceGrantStore) ListScopeGrants(
	ctx context.Context,
	query TenantWorkspaceGrantQuery,
) ([]TenantScopeGrant, error) {
	if s.db == nil {
		return nil, errors.New("tenant workspace grant store database is required")
	}
	query = normalizeGrantQuery(query)
	if err := validateGrantQuery(query); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		listTenantScopeGrantsQuery,
		query.TenantID,
		query.WorkspaceID,
		query.SubjectClass,
		query.AsOf,
		query.ScopeIDs,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant scope grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	grants := make([]TenantScopeGrant, 0)
	for rows.Next() {
		grant, err := scanTenantScopeGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("list tenant scope grants: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tenant scope grants: %w", err)
	}
	return grants, nil
}

// ListRepositoryGrants returns active repository grants for one boundary.
func (s *TenantWorkspaceGrantStore) ListRepositoryGrants(
	ctx context.Context,
	query TenantWorkspaceGrantQuery,
) ([]TenantRepositoryGrant, error) {
	if s.db == nil {
		return nil, errors.New("tenant workspace grant store database is required")
	}
	query = normalizeGrantQuery(query)
	if err := validateGrantQuery(query); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		listTenantRepositoryGrantsQuery,
		query.TenantID,
		query.WorkspaceID,
		query.SubjectClass,
		query.AsOf,
		query.ScopeIDs,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant repository grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	grants := make([]TenantRepositoryGrant, 0)
	for rows.Next() {
		grant, err := scanTenantRepositoryGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("list tenant repository grants: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tenant repository grants: %w", err)
	}
	return grants, nil
}

func normalizeTenantRecord(record TenantRecord) TenantRecord {
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.Status = strings.TrimSpace(record.Status)
	record.DisplayHandleHash = strings.TrimSpace(record.DisplayHandleHash)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.TombstonedAt = record.TombstonedAt.UTC()
	return record
}

func normalizeWorkspaceRecord(record WorkspaceRecord) WorkspaceRecord {
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.Status = strings.TrimSpace(record.Status)
	record.DisplayHandleHash = strings.TrimSpace(record.DisplayHandleHash)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.TombstonedAt = record.TombstonedAt.UTC()
	return record
}

func normalizeScopeGrant(grant TenantScopeGrant) TenantScopeGrant {
	grant.TenantID = strings.TrimSpace(grant.TenantID)
	grant.WorkspaceID = strings.TrimSpace(grant.WorkspaceID)
	grant.ScopeID = strings.TrimSpace(grant.ScopeID)
	grant.SubjectClass = strings.TrimSpace(grant.SubjectClass)
	grant.GrantSource = strings.TrimSpace(grant.GrantSource)
	grant.PolicyRevisionHash = strings.TrimSpace(grant.PolicyRevisionHash)
	grant.EffectiveAt = grant.EffectiveAt.UTC()
	grant.UpdatedAt = grant.UpdatedAt.UTC()
	grant.TombstonedAt = grant.TombstonedAt.UTC()
	if grant.ExpiresAt != nil {
		expiresAt := grant.ExpiresAt.UTC()
		grant.ExpiresAt = &expiresAt
	}
	return grant
}

func normalizeRepositoryGrant(grant TenantRepositoryGrant) TenantRepositoryGrant {
	grant.TenantID = strings.TrimSpace(grant.TenantID)
	grant.WorkspaceID = strings.TrimSpace(grant.WorkspaceID)
	grant.RepoID = strings.TrimSpace(grant.RepoID)
	grant.ScopeID = strings.TrimSpace(grant.ScopeID)
	grant.SubjectClass = strings.TrimSpace(grant.SubjectClass)
	grant.GrantSource = strings.TrimSpace(grant.GrantSource)
	grant.PolicyRevisionHash = strings.TrimSpace(grant.PolicyRevisionHash)
	grant.EffectiveAt = grant.EffectiveAt.UTC()
	grant.UpdatedAt = grant.UpdatedAt.UTC()
	grant.TombstonedAt = grant.TombstonedAt.UTC()
	if grant.ExpiresAt != nil {
		expiresAt := grant.ExpiresAt.UTC()
		grant.ExpiresAt = &expiresAt
	}
	return grant
}

func normalizeGrantQuery(query TenantWorkspaceGrantQuery) TenantWorkspaceGrantQuery {
	query.TenantID = strings.TrimSpace(query.TenantID)
	query.WorkspaceID = strings.TrimSpace(query.WorkspaceID)
	query.SubjectClass = strings.TrimSpace(query.SubjectClass)
	query.ScopeIDs = normalizeGrantScopeIDs(query.ScopeIDs)
	query.AsOf = query.AsOf.UTC()
	if query.Limit == 0 {
		query.Limit = defaultTenantWorkspaceGrantLimit
	}
	if query.Limit > maxTenantWorkspaceGrantLimit {
		query.Limit = maxTenantWorkspaceGrantLimit
	}
	return query
}

func validateTenantRecord(record TenantRecord) error {
	if blank(record.TenantID) || blank(record.Status) || blank(record.PolicyRevisionHash) {
		return errors.New("tenant id, status, and policy revision hash are required")
	}
	if record.UpdatedAt.IsZero() {
		return errors.New("tenant updated_at is required")
	}
	return nil
}

func validateWorkspaceRecord(record WorkspaceRecord) error {
	if blank(record.TenantID) || blank(record.WorkspaceID) ||
		blank(record.Status) || blank(record.PolicyRevisionHash) {
		return errors.New("workspace tenant id, workspace id, status, and policy revision hash are required")
	}
	if record.UpdatedAt.IsZero() {
		return errors.New("workspace updated_at is required")
	}
	return nil
}

func validateScopeGrant(grant TenantScopeGrant) error {
	if blank(grant.TenantID) || blank(grant.WorkspaceID) || blank(grant.ScopeID) ||
		blank(grant.SubjectClass) || blank(grant.GrantSource) || blank(grant.PolicyRevisionHash) {
		return errors.New("scope grant tenant, workspace, scope, subject class, source, and policy revision are required")
	}
	return validateGrantTimes(grant.EffectiveAt, grant.ExpiresAt, grant.UpdatedAt)
}

func validateRepositoryGrant(grant TenantRepositoryGrant) error {
	if blank(grant.TenantID) || blank(grant.WorkspaceID) || blank(grant.RepoID) ||
		blank(grant.ScopeID) || blank(grant.SubjectClass) || blank(grant.GrantSource) ||
		blank(grant.PolicyRevisionHash) {
		return errors.New("repository grant tenant, workspace, repo, scope, subject class, source, and policy revision are required")
	}
	return validateGrantTimes(grant.EffectiveAt, grant.ExpiresAt, grant.UpdatedAt)
}

func validateGrantTimes(effectiveAt time.Time, expiresAt *time.Time, updatedAt time.Time) error {
	if effectiveAt.IsZero() {
		return errors.New("grant effective_at is required")
	}
	if updatedAt.IsZero() {
		return errors.New("grant updated_at is required")
	}
	if expiresAt != nil && !expiresAt.After(effectiveAt) {
		return errors.New("grant expires_at must be after effective_at")
	}
	return nil
}

func validateGrantQuery(query TenantWorkspaceGrantQuery) error {
	if blank(query.TenantID) || blank(query.WorkspaceID) || blank(query.SubjectClass) {
		return errors.New("tenant id, workspace id, and subject class are required")
	}
	if query.AsOf.IsZero() {
		return errors.New("grant query as_of is required")
	}
	if query.Limit <= 0 {
		return errors.New("grant query limit must be positive")
	}
	return nil
}

func scanTenantScopeGrant(rows Rows) (TenantScopeGrant, error) {
	var grant TenantScopeGrant
	var expiresAt sql.NullTime
	if err := rows.Scan(
		&grant.TenantID,
		&grant.WorkspaceID,
		&grant.ScopeID,
		&grant.SubjectClass,
		&grant.GrantSource,
		&grant.PolicyRevisionHash,
		&grant.EffectiveAt,
		&expiresAt,
	); err != nil {
		return TenantScopeGrant{}, err
	}
	grant.ExpiresAt = timePtrFromNull(expiresAt)
	return grant, nil
}

func scanTenantRepositoryGrant(rows Rows) (TenantRepositoryGrant, error) {
	var grant TenantRepositoryGrant
	var expiresAt sql.NullTime
	if err := rows.Scan(
		&grant.TenantID,
		&grant.WorkspaceID,
		&grant.RepoID,
		&grant.ScopeID,
		&grant.SubjectClass,
		&grant.GrantSource,
		&grant.PolicyRevisionHash,
		&grant.EffectiveAt,
		&expiresAt,
	); err != nil {
		return TenantRepositoryGrant{}, err
	}
	grant.ExpiresAt = timePtrFromNull(expiresAt)
	return grant, nil
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func nullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}

func nullTimePtr(value *time.Time) sql.NullTime {
	if value == nil || value.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func timePtrFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}
