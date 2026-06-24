// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestIdentitySubjectSchemaIncludesSAMLAuthnRequestAndReplayLedger(t *testing.T) {
	t.Parallel()

	identity := identitySubjectDefinitionForTest(t)
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS identity_saml_authn_requests",
		"provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE",
		"request_id_hash TEXT NOT NULL",
		"relay_state_hash TEXT NOT NULL",
		"expires_at TIMESTAMPTZ NOT NULL",
		"consumed_at TIMESTAMPTZ NULL",
		"PRIMARY KEY (provider_config_id, request_id_hash)",
		"identity_saml_authn_requests_relay_state_idx",
		"identity_saml_authn_requests_pending_idx",
		"CREATE TABLE IF NOT EXISTS identity_saml_replay_keys",
		"replay_hash TEXT NOT NULL",
		"PRIMARY KEY (provider_config_id, replay_hash)",
		"identity_saml_replay_keys_expiry_idx",
	} {
		if !strings.Contains(identity.SQL, want) {
			t.Fatalf("identity schema SQL missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"relay_state TEXT",
		"saml_response",
		"saml_assertion",
		"raw_assertion",
		"name_id TEXT",
		"email TEXT",
		"certificate TEXT",
	} {
		if strings.Contains(strings.ToLower(identity.SQL), strings.ToLower(forbidden)) {
			t.Fatalf("identity schema SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestSAMLSSOSchemaSQLIncludesHashLedgers(t *testing.T) {
	t.Parallel()

	schema := SAMLSSOSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS identity_saml_authn_requests",
		"request_id_hash TEXT NOT NULL",
		"relay_state_hash TEXT NOT NULL",
		"CREATE TABLE IF NOT EXISTS identity_saml_replay_keys",
		"replay_hash TEXT NOT NULL",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("SAMLSSOSchemaSQL() missing %q", want)
		}
	}
}

func TestSAMLSSOStoreCreateRequestPersistsHashesOnlyAndIgnoresConflicts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewSAMLSSOStore(db)

	err := store.CreateSAMLRequest(context.Background(), SAMLAuthnRequestRecord{
		ProviderConfigID: "provider_config_a",
		RequestIDHash:    "sha256:request",
		RelayStateHash:   "sha256:relay-state",
		IssuedAt:         now,
		ExpiresAt:        now.Add(5 * time.Minute),
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		t.Fatalf("CreateSAMLRequest() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}

	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO identity_saml_authn_requests",
		"relay_state_hash",
		"status",
		"ON CONFLICT (provider_config_id, request_id_hash) DO NOTHING",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("create request query missing %q:\n%s", want, query)
		}
	}
	for _, leaked := range []string{"request-id", "relay-secret", "RelayState"} {
		if fakeExecArgsContain(db.execs[0].args, leaked) {
			t.Fatalf("CreateSAMLRequest() args leaked raw value %q: %#v", leaked, db.execs[0].args)
		}
	}
}

func TestSAMLSSOStoreConsumeRequestAtomicallyConsumesUnexpiredRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 35, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}}}
	store := NewSAMLSSOStore(db)

	consumed, err := store.ConsumeSAMLRequest(
		context.Background(),
		"provider_config_a",
		"sha256:request",
		"sha256:relay-state",
		now,
	)
	if err != nil {
		t.Fatalf("ConsumeSAMLRequest() error = %v", err)
	}
	if !consumed {
		t.Fatal("ConsumeSAMLRequest() consumed = false, want true")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE identity_saml_authn_requests",
		"status = 'consumed'",
		"consumed_at = $4",
		"updated_at = $4",
		"provider_config_id = $1",
		"request_id_hash = $2",
		"relay_state_hash = $3",
		"status = 'pending'",
		"consumed_at IS NULL",
		"expires_at > $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("consume request query missing %q:\n%s", want, query)
		}
	}
}

func TestSAMLSSOStoreConsumeRequestReturnsFalseForExpiredRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 40, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewSAMLSSOStore(db)

	consumed, err := store.ConsumeSAMLRequest(
		context.Background(),
		"provider_config_a",
		"sha256:request",
		"sha256:relay-state",
		now,
	)
	if err != nil {
		t.Fatalf("ConsumeSAMLRequest() error = %v", err)
	}
	if consumed {
		t.Fatal("ConsumeSAMLRequest() consumed = true, want false for expired or already-consumed request")
	}
}

func TestSAMLSSOStoreReserveReplayUsesInsertConflictLedger(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}}}
	store := NewSAMLSSOStore(db)

	reserved, err := store.ReserveSAMLReplay(context.Background(), SAMLReplayKeyRecord{
		ProviderConfigID: "provider_config_a",
		ReplayHash:       "sha256:assertion-or-response",
		ObservedAt:       now,
		ExpiresAt:        now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ReserveSAMLReplay() error = %v", err)
	}
	if !reserved {
		t.Fatal("ReserveSAMLReplay() reserved = false, want true")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}

	query := db.execs[0].query
	for _, want := range []string{
		"INSERT INTO identity_saml_replay_keys",
		"replay_hash",
		"status",
		"ON CONFLICT (provider_config_id, replay_hash) DO NOTHING",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reserve replay query missing %q:\n%s", want, query)
		}
	}
	for _, leaked := range []string{"assertion-id", "response-id", "saml-response"} {
		if fakeExecArgsContain(db.execs[0].args, leaked) {
			t.Fatalf("ReserveSAMLReplay() args leaked raw value %q: %#v", leaked, db.execs[0].args)
		}
	}
}

func TestSAMLSSOStoreReserveReplayReturnsFalseOnDuplicate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 50, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewSAMLSSOStore(db)

	reserved, err := store.ReserveSAMLReplay(context.Background(), SAMLReplayKeyRecord{
		ProviderConfigID: "provider_config_a",
		ReplayHash:       "sha256:assertion-or-response",
		ObservedAt:       now,
		ExpiresAt:        now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ReserveSAMLReplay() error = %v", err)
	}
	if reserved {
		t.Fatal("ReserveSAMLReplay() reserved = true, want false for duplicate replay hash")
	}
}

func identitySubjectDefinitionForTest(t *testing.T) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == "identity_subjects" {
			return def
		}
	}
	t.Fatal("identity_subjects definition missing")
	return Definition{}
}
