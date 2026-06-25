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

// SAMLSSOStore persists hash-only SAML login request and replay ledgers.
type SAMLSSOStore struct {
	db ExecQueryer
}

// SAMLAuthnRequestRecord is the durable hash-only state for one AuthnRequest.
// ReturnToPath is the sanitized post-login redirect path stored at login
// initiation (empty string when none was provided by the caller).
type SAMLAuthnRequestRecord struct {
	ProviderConfigID string
	RequestIDHash    string
	RelayStateHash   string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SAMLReplayKeyRecord records a hash-only SAML assertion or response replay key.
type SAMLReplayKeyRecord struct {
	ProviderConfigID string
	ReplayHash       string
	ObservedAt       time.Time
	ExpiresAt        time.Time
}

// NewSAMLSSOStore constructs a Postgres-backed SAML SSO ledger store.
func NewSAMLSSOStore(db ExecQueryer) *SAMLSSOStore {
	return &SAMLSSOStore{db: db}
}

// SAMLSSOSchemaSQL returns the SAML SSO request and replay ledger DDL.
func SAMLSSOSchemaSQL() string {
	return samlSSOSchemaSQL
}

// CreateSAMLRequest records one pending AuthnRequest using digest keys only.
func (s *SAMLSSOStore) CreateSAMLRequest(ctx context.Context, record SAMLAuthnRequestRecord) error {
	if s.db == nil {
		return errors.New("saml sso store database is required")
	}
	record = normalizeSAMLAuthnRequestRecord(record)
	if err := validateSAMLAuthnRequestRecord(record); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		createSAMLAuthnRequestQuery,
		record.ProviderConfigID,
		record.RequestIDHash,
		record.RelayStateHash,
		record.ReturnToPath,
		record.IssuedAt,
		record.ExpiresAt,
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create saml authn request: %w", err)
	}
	return nil
}

// ConsumeSAMLRequest atomically consumes one pending, unexpired AuthnRequest
// and returns the sanitized return_to path stored at login initiation (empty
// string when none was stored), whether the request was found and consumed,
// and any error.
func (s *SAMLSSOStore) ConsumeSAMLRequest(
	ctx context.Context,
	providerConfigID string,
	requestIDHash string,
	relayStateHash string,
	now time.Time,
) (string, bool, error) {
	if s.db == nil {
		return "", false, errors.New("saml sso store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	requestIDHash = strings.TrimSpace(requestIDHash)
	relayStateHash = strings.TrimSpace(relayStateHash)
	if providerConfigID == "" {
		return "", false, errors.New("saml provider config id is required")
	}
	if requestIDHash == "" {
		return "", false, errors.New("saml request id hash is required")
	}
	if relayStateHash == "" {
		return "", false, errors.New("saml relay state hash is required")
	}
	if now.IsZero() {
		return "", false, errors.New("saml request consume time is required")
	}
	rows, err := s.db.QueryContext(
		ctx,
		consumeSAMLAuthnRequestQuery,
		providerConfigID,
		requestIDHash,
		relayStateHash,
		now,
	)
	if err != nil {
		return "", false, fmt.Errorf("consume saml authn request: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("consume saml authn request: %w", err)
		}
		return "", false, nil
	}
	var returnToPath string
	if err := rows.Scan(&returnToPath); err != nil {
		return "", false, fmt.Errorf("consume saml authn request: %w", err)
	}
	return returnToPath, true, rows.Err()
}

// ReserveSAMLReplay records a replay hash and reports false on duplicates.
func (s *SAMLSSOStore) ReserveSAMLReplay(ctx context.Context, record SAMLReplayKeyRecord) (bool, error) {
	if s.db == nil {
		return false, errors.New("saml sso store database is required")
	}
	record = normalizeSAMLReplayKeyRecord(record)
	if err := validateSAMLReplayKeyRecord(record); err != nil {
		return false, err
	}
	result, err := s.db.ExecContext(
		ctx,
		reserveSAMLReplayKeyQuery,
		record.ProviderConfigID,
		record.ReplayHash,
		record.ObservedAt,
		record.ExpiresAt,
	)
	if err != nil {
		return false, fmt.Errorf("reserve saml replay key: %w", err)
	}
	return samlRowsAffected(result)
}

func normalizeSAMLAuthnRequestRecord(record SAMLAuthnRequestRecord) SAMLAuthnRequestRecord {
	record.ProviderConfigID = strings.TrimSpace(record.ProviderConfigID)
	record.RequestIDHash = strings.TrimSpace(record.RequestIDHash)
	record.RelayStateHash = strings.TrimSpace(record.RelayStateHash)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = record.IssuedAt
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	return record
}

func validateSAMLAuthnRequestRecord(record SAMLAuthnRequestRecord) error {
	if record.ProviderConfigID == "" {
		return errors.New("saml provider config id is required")
	}
	if record.RequestIDHash == "" {
		return errors.New("saml request id hash is required")
	}
	if record.RelayStateHash == "" {
		return errors.New("saml relay state hash is required")
	}
	if record.IssuedAt.IsZero() {
		return errors.New("saml request issue time is required")
	}
	if record.ExpiresAt.IsZero() {
		return errors.New("saml request expiry is required")
	}
	if !record.IssuedAt.Before(record.ExpiresAt) {
		return errors.New("saml request expiry must be after issue time")
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("saml request timestamps are required")
	}
	return nil
}

func normalizeSAMLReplayKeyRecord(record SAMLReplayKeyRecord) SAMLReplayKeyRecord {
	record.ProviderConfigID = strings.TrimSpace(record.ProviderConfigID)
	record.ReplayHash = strings.TrimSpace(record.ReplayHash)
	return record
}

func validateSAMLReplayKeyRecord(record SAMLReplayKeyRecord) error {
	if record.ProviderConfigID == "" {
		return errors.New("saml provider config id is required")
	}
	if record.ReplayHash == "" {
		return errors.New("saml replay hash is required")
	}
	if record.ObservedAt.IsZero() {
		return errors.New("saml replay observed time is required")
	}
	if record.ExpiresAt.IsZero() {
		return errors.New("saml replay expiry is required")
	}
	if !record.ObservedAt.Before(record.ExpiresAt) {
		return errors.New("saml replay expiry must be after observed time")
	}
	return nil
}

func samlRowsAffected(result interface {
	RowsAffected() (int64, error)
},
) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read saml rows affected: %w", err)
	}
	return affected > 0, nil
}

// samlSSOReturnToPathMigrationSQL adds return_to_path to existing deployments.
// It is idempotent (ADD COLUMN IF NOT EXISTS) and safe for zero-downtime apply.
const samlSSOReturnToPathMigrationSQL = `
ALTER TABLE identity_saml_authn_requests
    ADD COLUMN IF NOT EXISTS return_to_path TEXT NULL;
`

const samlSSOSchemaSQL = `
CREATE TABLE IF NOT EXISTS identity_saml_authn_requests (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    request_id_hash TEXT NOT NULL,
    relay_state_hash TEXT NOT NULL,
    return_to_path TEXT NULL,
    status TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_config_id, request_id_hash)
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_saml_authn_requests_relay_state_idx
    ON identity_saml_authn_requests (provider_config_id, relay_state_hash)
    WHERE consumed_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_saml_authn_requests_pending_idx
    ON identity_saml_authn_requests (provider_config_id, expires_at)
    WHERE status = 'pending' AND consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_saml_replay_keys (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    replay_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_config_id, replay_hash)
);

CREATE INDEX IF NOT EXISTS identity_saml_replay_keys_expiry_idx
    ON identity_saml_replay_keys (expires_at);
`

const createSAMLAuthnRequestQuery = `
INSERT INTO identity_saml_authn_requests (
    provider_config_id,
    request_id_hash,
    relay_state_hash,
    return_to_path,
    status,
    issued_at,
    expires_at,
    created_at,
    updated_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    'pending',
    $5,
    $6,
    $7,
    $8
)
ON CONFLICT (provider_config_id, request_id_hash) DO NOTHING
`

// consumeSAMLAuthnRequestQuery atomically marks one pending, unexpired
// AuthnRequest as consumed and returns its return_to_path. The CTE performs
// the UPDATE with RETURNING so the whole operation is one round-trip and
// remains atomic: no row is returned unless the UPDATE matched exactly.
const consumeSAMLAuthnRequestQuery = `
WITH consumed AS (
    UPDATE identity_saml_authn_requests
    SET status = 'consumed',
        consumed_at = $4,
        updated_at = $4
    WHERE provider_config_id = $1
      AND request_id_hash = $2
      AND relay_state_hash = $3
      AND status = 'pending'
      AND consumed_at IS NULL
      AND expires_at > $4
    RETURNING return_to_path
)
SELECT COALESCE(return_to_path, '') FROM consumed
`

const reserveSAMLReplayKeyQuery = `
INSERT INTO identity_saml_replay_keys (
    provider_config_id,
    replay_hash,
    status,
    observed_at,
    expires_at
) VALUES (
    $1,
    $2,
    'observed',
    $3,
    $4
)
ON CONFLICT (provider_config_id, replay_hash) DO NOTHING
`
