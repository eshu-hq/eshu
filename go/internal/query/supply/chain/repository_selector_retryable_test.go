// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// selectorMatchContent fails the security-alert selector's catalog read with
// err, or answers entries when err is nil.
type selectorMatchContent struct {
	content.FakePortContentStore
	entries []querycontract.RepositoryCatalogEntry
	err     error
}

func (s selectorMatchContent) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.entries, nil
}

// selectorScopeLookupStore serves the security-alert list and aggregate ports
// and fails the provider repository-scope lookup the selector runs when the
// catalog entry carries no repo_slug or remote_url evidence.
type selectorScopeLookupStore struct {
	errSecurityAlertStore
	errSecurityAlertAggregateStore
	err error
}

func (s selectorScopeLookupStore) SecurityAlertProviderRepositoryScopes(context.Context, string) ([]string, error) {
	return nil, s.err
}

// TestRepositorySelectorReadsAnswerRetryable503 is the #7567 proof for the
// security-alert repository selector. Its catalog read (Content, built on the
// guarded reader in cmd/api) and its provider scope lookup (the security-alert
// stores, built WithReadStore) both run through the guarded PostgreSQL reader,
// so a stale reader or a pool-wait timeout must answer the retryable 503
// backend_unavailable with Retry-After. A bare reader failure stays a 500.
// The selector runs before any stage timer starts, so neither verdict emits a
// supply_chain_query.stage_failed record.
func TestRepositorySelectorReadsAnswerRetryable503(t *testing.T) {
	t.Parallel()

	catalog := []querycontract.RepositoryCatalogEntry{{ID: "repository:r_payments", Name: "payments"}}
	routes := []struct {
		name       string
		target     string
		capability string
	}{
		{
			name:       "security alert reconciliations list",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=payments",
			capability: SecurityAlertReconciliationsCapability,
		},
		{
			name:       "security alert aggregate count",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=payments",
			capability: SecurityAlertReconciliationAggregateCapability,
		},
		{
			name:       "security alert aggregate inventory",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10&repository_id=payments",
			capability: SecurityAlertReconciliationAggregateCapability,
		},
	}
	reads := []struct {
		name  string
		build func(err error) *Handler
	}{
		{
			name: "catalog match read",
			build: func(err error) *Handler {
				return &Handler{
					Content:                 selectorMatchContent{err: err},
					SecurityAlerts:          selectorScopeLookupStore{},
					SecurityAlertAggregates: selectorScopeLookupStore{},
				}
			},
		},
		{
			name: "provider scope lookup read",
			build: func(err error) *Handler {
				store := selectorScopeLookupStore{err: err}
				return &Handler{
					Content:                 selectorMatchContent{entries: catalog},
					SecurityAlerts:          store,
					SecurityAlertAggregates: store,
				}
			},
		},
	}
	verdicts := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"reader stale", fmt.Errorf("read store: %w", db.ErrReaderStale), http.StatusServiceUnavailable},
		{
			"reader pool wait timeout",
			fmt.Errorf("read store: %w", errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded)),
			http.StatusServiceUnavailable,
		},
		{"bare reader unavailable", fmt.Errorf("read store: %w", db.ErrReaderUnavailable), http.StatusInternalServerError},
	}

	for _, route := range routes {
		for _, read := range reads {
			for _, verdict := range verdicts {
				t.Run(route.name+"/"+read.name+"/"+verdict.name, func(t *testing.T) {
					t.Parallel()

					var logBuf bytes.Buffer
					handler := read.build(verdict.err)
					handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

					rec := serveSiblingRoute(t, handler, route.target)

					if rec.Code != verdict.wantStatus {
						t.Fatalf("status = %d, want %d; body = %s", rec.Code, verdict.wantStatus, rec.Body.String())
					}
					if failed := recordsWithEvent(decodeLogRecords(t, &logBuf), "supply_chain_query.stage_failed"); len(failed) != 0 {
						t.Fatalf("stage_failed records = %#v, want none: the selector runs before any stage timer", failed)
					}
					if verdict.wantStatus == http.StatusInternalServerError {
						if got := rec.Header().Get("Retry-After"); got != "" {
							t.Fatalf("Retry-After = %q on a non-retryable 500, want none", got)
						}
						return
					}
					if seconds, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || seconds < 1 {
						t.Fatalf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
					}
					var body struct {
						Error *querycontract.ErrorEnvelope `json:"error"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
						t.Fatalf("body = %s, want an error envelope: %v", rec.Body.String(), err)
					}
					if body.Error.Code != querycontract.ErrorCodeBackendUnavailable {
						t.Fatalf("error code = %q, want %q", body.Error.Code, querycontract.ErrorCodeBackendUnavailable)
					}
					if body.Error.Capability != route.capability {
						t.Fatalf("error capability = %q, want %q", body.Error.Capability, route.capability)
					}
				})
			}
		}
	}
}
