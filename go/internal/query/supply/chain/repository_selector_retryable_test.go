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
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// selectorTestSelector is the raw repository_id the selector tests send. It
// is deliberately not a substring of selectorTestRepositoryID, so a log
// attribute carrying the raw selector is distinguishable from one carrying
// the resolved canonical id.
const (
	selectorTestSelector     = "payments-svc"
	selectorTestRepositoryID = "repository:r_7f3a9c"
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

type selectorTestRoute struct {
	name       string
	target     string
	capability string
	operation  string
	spanName   string
}

func selectorTestRoutes() []selectorTestRoute {
	return []selectorTestRoute{
		{
			name:       "security alert reconciliations list",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=" + selectorTestSelector,
			capability: SecurityAlertReconciliationsCapability,
			operation:  supplyChainSecurityAlertReconciliationOperation,
			spanName:   telemetry.SpanQuerySupplyChainSecurityAlerts,
		},
		{
			name:       "security alert aggregate count",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=" + selectorTestSelector,
			capability: SecurityAlertReconciliationAggregateCapability,
			operation:  supplyChainSecurityAlertAggregateOperation,
			spanName:   telemetry.SpanQuerySecurityAlertReconciliationAggregate,
		},
		{
			name:       "security alert aggregate inventory",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10&repository_id=" + selectorTestSelector,
			capability: SecurityAlertReconciliationAggregateCapability,
			operation:  supplyChainSecurityAlertAggregateOperation,
			spanName:   telemetry.SpanQuerySecurityAlertReconciliationAggregate,
		},
	}
}

type selectorTestRead struct {
	name   string
	stage  string
	repoID string
	build  func(err error) *Handler
}

func selectorTestReads() []selectorTestRead {
	catalog := []querycontract.RepositoryCatalogEntry{{ID: selectorTestRepositoryID, Name: selectorTestSelector}}
	return []selectorTestRead{
		{
			name:   "catalog match read",
			stage:  "repository_catalog_match",
			repoID: "",
			build: func(err error) *Handler {
				return &Handler{
					Content:                 selectorMatchContent{err: err},
					SecurityAlerts:          selectorScopeLookupStore{},
					SecurityAlertAggregates: selectorScopeLookupStore{},
				}
			},
		},
		{
			name:   "provider scope lookup read",
			stage:  "provider_repository_scope_lookup",
			repoID: selectorTestRepositoryID,
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
}

// TestRepositorySelectorReadsAnswerRetryable503 is the #7567 proof for the
// security-alert repository selector. Its catalog read (Content, built on the
// guarded reader in cmd/api) and its provider scope lookup (the security-alert
// stores, built WithReadStore) both run through the guarded PostgreSQL reader,
// so a stale reader or a pool-wait timeout must answer the retryable 503
// backend_unavailable with Retry-After and no stage_failed record. A bare
// reader failure stays a handler-owned 500 with exactly one stage_failed
// record for its stage. No log attribute may carry the raw selector.
func TestRepositorySelectorReadsAnswerRetryable503(t *testing.T) {
	t.Parallel()

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

	for _, route := range selectorTestRoutes() {
		for _, read := range selectorTestReads() {
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
					records := decodeLogRecords(t, &logBuf)
					assertNoRawSelectorLogged(t, records)
					failed := recordsWithEvent(records, "supply_chain_query.stage_failed")
					if verdict.wantStatus == http.StatusInternalServerError {
						if got := rec.Header().Get("Retry-After"); got != "" {
							t.Fatalf("Retry-After = %q on a non-retryable 500, want none", got)
						}
						assertSelectorStageFailed(t, failed, route.operation, read.stage, read.repoID, logBuf.String())
						return
					}
					if len(failed) != 0 {
						t.Fatalf("stage_failed records = %d on a mapped 503, want 0; log=%s", len(failed), logBuf.String())
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

// TestRepositorySelectorHandlerOwned500RecordsSpanError pins the span half of
// failStage on the selector reads: a handler-owned 500 sets the route's
// handler span to Error and records the error as an exception event.
func TestRepositorySelectorHandlerOwned500RecordsSpanError(t *testing.T) {
	// Not parallel: swaps the package-global queryHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("repository-selector-stage-failed-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	bare := fmt.Errorf("read store: %w", db.ErrReaderUnavailable)
	for _, route := range selectorTestRoutes() {
		for _, read := range selectorTestReads() {
			t.Run(route.name+"/"+read.name, func(t *testing.T) {
				spansBefore := len(recorder.Ended())
				rec := serveSiblingRoute(t, read.build(bare), route.target)
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
				}
				var handlerSpan sdktrace.ReadOnlySpan
				for _, span := range recorder.Ended()[spansBefore:] {
					if span.Name() == route.spanName {
						handlerSpan = span
					}
				}
				if handlerSpan == nil {
					t.Fatalf("no ended %s span", route.spanName)
				}
				if handlerSpan.Status().Code != codes.Error {
					t.Fatalf("handler span status = %v, want Error", handlerSpan.Status().Code)
				}
				hasException := false
				for _, event := range handlerSpan.Events() {
					if event.Name == "exception" {
						hasException = true
					}
				}
				if !hasException {
					t.Fatalf("handler span has no recorded exception event; events=%#v", handlerSpan.Events())
				}
			})
		}
	}
}

func assertSelectorStageFailed(t *testing.T, failed []map[string]any, operation, stage, repoID, log string) {
	t.Helper()
	if len(failed) != 1 {
		t.Fatalf("stage_failed records = %d, want 1; log=%s", len(failed), log)
	}
	record := failed[0]
	if record["level"] != "ERROR" {
		t.Fatalf("stage_failed level = %v, want ERROR", record["level"])
	}
	if record["operation"] != operation || record["stage"] != stage || record["repo_id"] != repoID {
		t.Fatalf("stage_failed identity = %#v, want operation=%q stage=%q repo_id=%q", record, operation, stage, repoID)
	}
	if record["error_site"] != "reader_unavailable" {
		t.Fatalf("stage_failed error_site = %v, want reader_unavailable", record["error_site"])
	}
}

// assertNoRawSelectorLogged fails when any string attribute of any record
// carries the raw repository selector, which is unbounded caller input.
func assertNoRawSelectorLogged(t *testing.T, records []map[string]any) {
	t.Helper()
	for _, record := range records {
		for key, value := range record {
			if text, ok := value.(string); ok && strings.Contains(text, selectorTestSelector) {
				t.Fatalf("log attribute %q = %q carries the raw selector %q", key, text, selectorTestSelector)
			}
		}
	}
}
