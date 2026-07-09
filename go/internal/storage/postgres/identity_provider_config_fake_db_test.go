// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// --- fake DB: an in-memory model of identity_provider_configs and
// identity_provider_config_revisions, mutex-serialized per the single-row
// FOR UPDATE conflict domain, so concurrent-writer tests can run under
// `go test -race` and prove the Go-level transaction logic in this package
// serializes correctly (real Postgres row-lock semantics are proven by the
// SQL shape itself — FOR UPDATE — not by this fake).

type fakeProviderConfigRow struct {
	tenantID         string
	providerKind     string
	providerKeyHash  string
	status           string
	activeRevisionID string
	tombstoned       bool
}

type fakeProviderConfigRevisionRow struct {
	status        string
	sealedSecret  string
	configuration string
}

type providerConfigFakeDB struct {
	mu sync.Mutex

	configs   map[string]*fakeProviderConfigRow
	revisions map[string]map[string]*fakeProviderConfigRevisionRow
}

func newProviderConfigFakeDB() *providerConfigFakeDB {
	return &providerConfigFakeDB{
		configs:   make(map[string]*fakeProviderConfigRow),
		revisions: make(map[string]map[string]*fakeProviderConfigRevisionRow),
	}
}

func (db *providerConfigFakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch query {
	case insertProviderConfigRevisionQuery:
		providerConfigID := args[0].(string)
		revisionID := args[1].(string)
		var sealed string
		if args[4] != nil {
			sealed = args[4].(string)
		}
		var configuration string
		if args[5] != nil {
			configuration = args[5].(string)
		}
		if db.revisions[providerConfigID] == nil {
			db.revisions[providerConfigID] = make(map[string]*fakeProviderConfigRevisionRow)
		}
		db.revisions[providerConfigID][revisionID] = &fakeProviderConfigRevisionRow{
			status:        "active",
			sealedSecret:  sealed,
			configuration: configuration,
		}
		return affectedResult{affected: 1}, nil
	case activateProviderConfigActiveRevisionQuery:
		providerConfigID := args[0].(string)
		revisionID := args[2].(string)
		row := db.configs[providerConfigID]
		if row == nil {
			return affectedResult{affected: 0}, nil
		}
		row.activeRevisionID = revisionID
		// Mirrors the real query: changing the active revision always resets
		// status back to draft (see activateProviderConfigActiveRevisionQuery's
		// doc comment) — a fresh Enable + test-connection is required before
		// the provider is trusted again.
		row.status = "draft"
		return affectedResult{affected: 1}, nil
	case supersedeProviderConfigRevisionQuery:
		providerConfigID := args[0].(string)
		revisionID := args[1].(string)
		if rev, ok := db.revisions[providerConfigID][revisionID]; ok {
			rev.status = "superseded"
			return affectedResult{affected: 1}, nil
		}
		return affectedResult{affected: 0}, nil
	case activateProviderConfigRevisionQuery:
		providerConfigID := args[0].(string)
		revisionID := args[1].(string)
		if rev, ok := db.revisions[providerConfigID][revisionID]; ok {
			rev.status = "active"
			return affectedResult{affected: 1}, nil
		}
		return affectedResult{affected: 0}, nil
	default:
		return nil, errUnexpectedProviderConfigQuery(query)
	}
}

func (db *providerConfigFakeDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch query {
	case insertProviderConfigQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		providerKind := args[2].(string)
		providerKeyHash := args[3].(string)
		for id, row := range db.configs {
			if !row.tombstoned && row.tenantID == tenantID && row.providerKind == providerKind && row.providerKeyHash == providerKeyHash {
				_ = id
				return &scalarRows{}, nil // ON CONFLICT DO NOTHING -> zero rows returned
			}
		}
		db.configs[providerConfigID] = &fakeProviderConfigRow{
			tenantID:        tenantID,
			providerKind:    providerKind,
			providerKeyHash: providerKeyHash,
			status:          "draft",
		}
		return &scalarRows{data: [][]any{{providerConfigID}}}, nil
	case selectProviderConfigForUpdateQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		row := db.configs[providerConfigID]
		if row == nil || row.tombstoned || row.tenantID != tenantID {
			return &scalarRows{}, nil
		}
		var activeRevisionID any
		if row.activeRevisionID != "" {
			activeRevisionID = row.activeRevisionID
		}
		return &scalarRows{data: [][]any{{providerConfigID, row.providerKind, row.status, activeRevisionID}}}, nil
	case selectProviderConfigRevisionExistsQuery:
		providerConfigID := args[0].(string)
		revisionID := args[1].(string)
		if _, ok := db.revisions[providerConfigID][revisionID]; ok {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case selectProviderConfigDetailQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		row := db.configs[providerConfigID]
		if row == nil || row.tombstoned || row.tenantID != tenantID {
			return &scalarRows{}, nil
		}
		var sealed, configuration any
		if rev, ok := db.revisions[providerConfigID][row.activeRevisionID]; ok {
			if rev.sealedSecret != "" {
				sealed = rev.sealedSecret
			}
			if rev.configuration != "" {
				configuration = rev.configuration
			}
		}
		var activeRevisionID any
		if row.activeRevisionID != "" {
			activeRevisionID = row.activeRevisionID
		}
		now := time.Now().UTC()
		return &scalarRows{data: [][]any{{
			providerConfigID, tenantID, row.providerKind, row.status, activeRevisionID, now, now, sealed, configuration,
		}}}, nil
	case setProviderConfigStatusQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		targetStatus := args[2].(string)
		row := db.configs[providerConfigID]
		if row == nil || row.tombstoned || row.tenantID != tenantID {
			return &scalarRows{}, nil
		}
		row.status = targetStatus
		return &scalarRows{data: [][]any{{row.status}}}, nil
	case selectProviderConfigsQuery:
		tenantID := args[0].(string)
		now := time.Now().UTC()
		var data [][]any
		for providerConfigID, row := range db.configs {
			if row.tombstoned || row.tenantID != tenantID {
				continue
			}
			var sealed, configuration any
			if rev, ok := db.revisions[providerConfigID][row.activeRevisionID]; ok {
				if rev.sealedSecret != "" {
					sealed = rev.sealedSecret
				}
				if rev.configuration != "" {
					configuration = rev.configuration
				}
			}
			var activeRevisionID any
			if row.activeRevisionID != "" {
				activeRevisionID = row.activeRevisionID
			}
			data = append(data, []any{providerConfigID, tenantID, row.providerKind, row.status, activeRevisionID, now, now, sealed, configuration})
		}
		return &scalarRows{data: data}, nil
	case selectProviderConfigRevisionsQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		row := db.configs[providerConfigID]
		if row == nil || row.tombstoned || row.tenantID != tenantID {
			return &scalarRows{}, nil
		}
		now := time.Now().UTC()
		var data [][]any
		for revisionID, rev := range db.revisions[providerConfigID] {
			data = append(data, []any{revisionID, rev.status, rev.sealedSecret != "", now, now, nil})
		}
		return &scalarRows{data: data}, nil
	case selectProviderConfigConnectionTestMaterialQuery:
		providerConfigID := args[0].(string)
		tenantID := args[1].(string)
		row := db.configs[providerConfigID]
		if row == nil || row.tombstoned || row.tenantID != tenantID || row.activeRevisionID == "" {
			return &scalarRows{}, nil
		}
		rev, ok := db.revisions[providerConfigID][row.activeRevisionID]
		if !ok {
			return &scalarRows{}, nil
		}
		var configuration any
		if rev.configuration != "" {
			configuration = rev.configuration
		}
		return &scalarRows{data: [][]any{{row.providerKind, row.activeRevisionID, rev.sealedSecret, configuration}}}, nil
	default:
		return nil, errUnexpectedProviderConfigQuery(query)
	}
}

func (db *providerConfigFakeDB) Begin(context.Context) (Transaction, error) {
	db.mu.Lock()
	return &providerConfigFakeTx{db: db}, nil
}

type providerConfigFakeTx struct {
	db *providerConfigFakeDB
}

func (tx *providerConfigFakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *providerConfigFakeTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx *providerConfigFakeTx) Commit() error   { tx.db.mu.Unlock(); return nil }
func (tx *providerConfigFakeTx) Rollback() error { tx.db.mu.Unlock(); return nil }

type unexpectedProviderConfigQueryError string

func (e unexpectedProviderConfigQueryError) Error() string { return "unexpected query: " + string(e) }

func errUnexpectedProviderConfigQuery(query string) error {
	return unexpectedProviderConfigQueryError(query)
}

// testKeyring builds a deterministic 32-byte-key Keyring for tests. Shared
// across every provider-config test file in this package.
func testKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	kr, err := secretcrypto.NewKeyring("k1", map[secretcrypto.KeyID][]byte{"k1": key})
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}
