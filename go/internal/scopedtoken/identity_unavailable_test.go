// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/boundederr"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// failingIdentityDB is a db.ExecQueryer whose every read fails with err, the way
// a PostgreSQL pool answers while the server is down or refusing new dials.
type failingIdentityDB struct{ err error }

func (f failingIdentityDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, f.err
}

func (f failingIdentityDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, f.err
}

func newFailingResolver(err error) *PostgresIdentityResolver {
	return NewPostgresIdentityResolver(pgstatus.NewScopedAPITokenStore(failingIdentityDB{err: err}))
}

// TestPostgresIdentityResolverClassifiesStoreOutageAsUnavailable proves a
// transient store failure surfaces as querycontract.ErrIdentityStoreUnavailable
// (#7586), so the auth middleware can answer a retryable 503 instead of a 401,
// while the driver cause stays reachable for operator logs.
func TestPostgresIdentityResolverClassifiesStoreOutageAsUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		class string
	}{
		{"connection lost", boundederr.Wrap(io.ErrUnexpectedEOF), "unavailable"},
		{"statement timeout", boundederr.Wrap(context.DeadlineExceeded), "timeout"},
		{"bad connection surfaced by database/sql", driver.ErrBadConn, "unavailable"},
		{"connection already closed", sql.ErrConnDone, "unavailable"},
		// database/sql returns the context error itself, before the driver and the
		// bounded connector see it, when a pool wait or the request deadline runs
		// out. It is transient and, like a reader timeout (#7523), retryable.
		{"pool wait deadline surfaced raw", context.DeadlineExceeded, "timeout"},
		// PostgreSQL refusing a connection for lack of resources reaches the
		// resolver as a *pgconn.PgError, which boundederr classes as failed (it
		// checks a server error before a connect error). The issue names a
		// connection-limit blip, so the resource-limit class is claimed here.
		{"too many connections", boundederr.Wrap(&pgconn.PgError{Code: "53300"}), "unavailable"},
		{"out of memory on the server", boundederr.Wrap(&pgconn.PgError{Code: "53200"}), "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := newFailingResolver(tt.cause)
			_, ok, err := resolver.ResolveScopedToken(context.Background(), "some-credential")
			if ok {
				t.Fatal("ok = true: a credential the store could not evaluate must never authenticate")
			}
			if !errors.Is(err, querycontract.ErrIdentityStoreUnavailable) {
				t.Fatalf("err = %v, want it to wrap ErrIdentityStoreUnavailable", err)
			}
			if !errors.Is(err, tt.cause) {
				t.Fatalf("err = %v, want the driver cause to stay reachable through errors.Is", err)
			}
		})
	}
}

// TestPostgresIdentityResolverDoesNotClaimOtherFailures keeps the 503 verdict
// narrow: a rejected statement or a caller cancel is not an outage, so it keeps
// its own error and the middleware's bare 401 (the anthropics/claude-code#59467
// fail-safe).
func TestPostgresIdentityResolverDoesNotClaimOtherFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
	}{
		{"statement failed", boundederr.Wrap(errors.New("syntax error"))},
		{"server rejected the statement", boundederr.Wrap(&pgconn.PgError{Code: "42601"})},
		{"integrity violation", boundederr.Wrap(&pgconn.PgError{Code: "23505"})},
		{"caller canceled", boundederr.Wrap(context.Canceled)},
		{"caller canceled before the pool answered", context.Canceled},
		{"unclassified error", errors.New("something else")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := newFailingResolver(tt.cause).ResolveScopedToken(context.Background(), "some-credential")
			if err == nil {
				t.Fatal("err = nil, want the store error to surface")
			}
			if errors.Is(err, querycontract.ErrIdentityStoreUnavailable) {
				t.Fatalf("err = %v, must not be claimed as an identity-store outage", err)
			}
		})
	}
}

// TestPostgresIdentityResolverEmitsAttributableSignals proves an operator at
// 3 AM can tell that authentication failed because the identity store was
// unreachable: one structured log line naming the event and the bounded failure
// class, and one counter increment by that class. Neither carries the
// credential or the driver's error text.
func TestPostgresIdentityResolverEmitsAttributableSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	resolver := newFailingResolver(boundederr.Wrap(io.ErrUnexpectedEOF)).WithTelemetry(logger, instruments)

	const credential = "super-secret-credential"
	if _, _, err := resolver.ResolveScopedToken(context.Background(), credential); err == nil {
		t.Fatal("err = nil, want the outage to surface")
	}

	line := logs.String()
	for _, want := range []string{`"event_name":"auth.identity_store.unavailable"`, `"failure_class":"unavailable"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("log = %s, want it to contain %s", line, want)
		}
	}
	if strings.Contains(line, credential) {
		t.Fatalf("log = %s leaks the presented credential", line)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_auth_identity_store_unavailable_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				class, _ := point.Attributes.Value(telemetry.MetricDimensionFailureClass)
				if class.AsString() != "unavailable" {
					t.Fatalf("failure_class = %q, want %q", class.AsString(), "unavailable")
				}
				total += point.Value
			}
		}
	}
	if total != 1 {
		t.Fatalf("eshu_dp_auth_identity_store_unavailable_total = %d, want 1", total)
	}
}

// TestPostgresIdentityResolverClassifiesTopologyDivergenceSeparately proves a
// writer that was refused for a topology mismatch (a promotion, a restore, or a
// different cluster) reports failure_class=topology and not unavailable (#7586).
// That state is permanent until the process restarts; labeling it unavailable
// would tell an operator to retry shortly forever. The status stays a retryable
// 503 through ErrIdentityStoreUnavailable, matching the checkpoint path.
func TestPostgresIdentityResolverClassifiesTopologyDivergenceSeparately(t *testing.T) {
	t.Parallel()

	// A dial refused by the writer validator reaches the resolver as a connect
	// error (a *pgconn.ConnectError, which cannot be built outside pgconn)
	// carrying db.ErrWrongTopology, which boundederr classifies as unavailable. A
	// net.OpError stands in for it: both are network errors that unwrap to the
	// sentinel, and the shared sentinel must still win.
	cause := boundederr.Wrap(&net.OpError{Op: "dial", Err: db.ErrWrongTopology})
	var bounded *boundederr.Error
	if !errors.As(cause, &bounded) || bounded.Kind() != boundederr.KindUnavailable {
		t.Fatalf("fixture drift: cause = %v, want a KindUnavailable bounded error", cause)
	}

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	resolver := newFailingResolver(cause).WithTelemetry(slog.New(slog.NewJSONHandler(&logs, nil)), instruments)

	_, ok, err := resolver.ResolveScopedToken(context.Background(), "some-credential")
	if ok || !errors.Is(err, querycontract.ErrIdentityStoreUnavailable) {
		t.Fatalf("ok=%v err=%v, want not ok and ErrIdentityStoreUnavailable", ok, err)
	}
	if !errors.Is(err, db.ErrWrongTopology) {
		t.Fatalf("err = %v, want db.ErrWrongTopology reachable for operators", err)
	}
	if !strings.Contains(logs.String(), `"failure_class":"topology"`) {
		t.Fatalf("log = %s, want failure_class topology", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("log = %s, want level ERROR: a topology refusal does not clear on its own", logs.String())
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	seen := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "eshu_dp_auth_identity_store_unavailable_total" {
				for _, point := range sum.DataPoints {
					class, _ := point.Attributes.Value(telemetry.MetricDimensionFailureClass)
					seen[class.AsString()] += point.Value
				}
			}
		}
	}
	if seen["topology"] != 1 || seen["unavailable"] != 0 {
		t.Fatalf("counter by failure_class = %v, want topology=1 unavailable=0", seen)
	}
}
