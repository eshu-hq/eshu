// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type configProbe struct {
	queries int
	execs   int
	begins  int
}

func (p *configProbe) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	p.queries++
	return nil, errors.New("unexpected database query")
}

func (p *configProbe) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	p.execs++
	return nil, errors.New("unexpected database exec")
}

func (p *configProbe) Begin(context.Context) (db.Transaction, error) {
	p.begins++
	return nil, errors.New("unexpected transaction begin")
}

func configuredStore(p *configProbe, callbacks *int) Store {
	return Store{
		Backfill: backfill.Store{DB: p, Beginner: p},
		PhaseCommitted: func(context.Context, db.ExecQueryer, backfill.ScopeGenerationPartition) (bool, error) {
			*callbacks++
			return false, nil
		},
		GateReopen: func(context.Context, string, []WorkItem, map[backfill.ScopeGenerationPartition]struct{}) ([]string, int) {
			*callbacks++
			return nil, 0
		},
		ReopenSucceeded: func(context.Context, db.Transaction, string) (bool, error) {
			*callbacks++
			return false, nil
		},
		Queries:            ProductionQueries,
		CorrelationDomains: []string{"correlation-a"},
	}
}

var (
	configOwed         = []OwedPartition{{ScopeID: "scope-a", GenerationID: "gen-a"}}
	configBenchmarkErr error
	configAffected     = []backfill.ScopeGenerationPartition{{ScopeID: "scope-a", GenerationID: "gen-a"}}
)

func TestTargetedPassRejectsIncompleteStoreBeforeDatabase(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Store)
		want string
	}{
		{"phase callback", func(s *Store) { s.PhaseCommitted = nil }, "partition-scoped maintenance phase callback is required"},
		{"gate callback", func(s *Store) { s.GateReopen = nil }, "partition-scoped maintenance reopen gate callback is required"},
		{"reopen callback", func(s *Store) { s.ReopenSucceeded = nil }, "partition-scoped maintenance reopen callback is required"},
		{"deployment query empty", func(s *Store) { s.Queries.DeploymentMapping = "" }, "partition-scoped maintenance deployment mapping query is required"},
		{"deployment query blank", func(s *Store) { s.Queries.DeploymentMapping = " \t" }, "partition-scoped maintenance deployment mapping query is required"},
		{"code import query", func(s *Store) { s.Queries.CodeImportRepoEdge = " " }, "partition-scoped maintenance code import repo edge query is required"},
		{"correlation query", func(s *Store) { s.Queries.Correlation = "\n" }, "partition-scoped maintenance correlation query is required"},
		{"domains nil", func(s *Store) { s.CorrelationDomains = nil }, "partition-scoped maintenance correlation domains are required"},
		{"domains empty", func(s *Store) { s.CorrelationDomains = []string{} }, "partition-scoped maintenance correlation domains are required"},
		{"domain blank", func(s *Store) { s.CorrelationDomains = []string{"correlation-a", " \t"} }, "partition-scoped maintenance correlation domain 1 is blank"},
		{"domain duplicate", func(s *Store) { s.CorrelationDomains = []string{"correlation-a", "correlation-a"} }, "partition-scoped maintenance correlation domain \"correlation-a\" is duplicated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &configProbe{}
			callbacks := 0
			s := configuredStore(p, &callbacks)
			tc.edit(&s)
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			_, err := s.RunDeferredRelationshipMaintenanceForPartitions(context.Background(), provider.Tracer("config"), nil, configOwed)
			if len(recorder.Ended()) != 0 {
				t.Fatalf("invalid store started %d spans", len(recorder.Ended()))
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if p.queries != 0 || p.execs != 0 || p.begins != 0 || callbacks != 0 {
				t.Fatalf("invalid store touched database/transaction/callback: queries=%d execs=%d begins=%d callbacks=%d", p.queries, p.execs, p.begins, callbacks)
			}
		})
	}
}

func TestTargetedPassPreservesExistingValidationPrecedence(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Store)
		owed []OwedPartition
		want string
	}{
		{"database", func(s *Store) { s.Backfill.DB = nil }, configOwed, "ingestion store db is required"},
		{"beginner", func(s *Store) { s.Backfill.Beginner = nil }, configOwed, "transaction beginner is required for partition-scoped maintenance"},
		{"owed empty", func(*Store) {}, nil, "partition-scoped maintenance requires at least one owed partition"},
		{"owed normalized blank", func(*Store) {}, []OwedPartition{{ScopeID: " ", GenerationID: ""}}, "partition-scoped maintenance requires at least one owed partition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &configProbe{}
			callbacks := 0
			s := configuredStore(p, &callbacks)
			s.PhaseCommitted = nil
			tc.edit(&s)
			_, err := s.RunDeferredRelationshipMaintenanceForPartitions(context.Background(), nil, nil, tc.owed)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if p.queries != 0 || p.execs != 0 || p.begins != 0 || callbacks != 0 {
				t.Fatalf("validation touched database/transaction/callback: queries=%d execs=%d begins=%d callbacks=%d", p.queries, p.execs, p.begins, callbacks)
			}
		})
	}
}

func TestTargetedReopenRejectsIncompleteStoreBeforeBegin(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Store)
		want string
	}{
		{"beginner", func(s *Store) { s.Backfill.Beginner = nil }, "transaction beginner is required for partition-scoped maintenance"},
		{"gate callback", func(s *Store) { s.GateReopen = nil }, "partition-scoped maintenance reopen gate callback is required"},
		{"reopen callback", func(s *Store) { s.ReopenSucceeded = nil }, "partition-scoped maintenance reopen callback is required"},
		{"deployment query", func(s *Store) { s.Queries.DeploymentMapping = " " }, "partition-scoped maintenance deployment mapping query is required"},
		{"code import query", func(s *Store) { s.Queries.CodeImportRepoEdge = "" }, "partition-scoped maintenance code import repo edge query is required"},
		{"correlation query", func(s *Store) { s.Queries.Correlation = "\t" }, "partition-scoped maintenance correlation query is required"},
		{"domains nil", func(s *Store) { s.CorrelationDomains = nil }, "partition-scoped maintenance correlation domains are required"},
		{"domains empty", func(s *Store) { s.CorrelationDomains = []string{} }, "partition-scoped maintenance correlation domains are required"},
		{"domain blank", func(s *Store) { s.CorrelationDomains = []string{""} }, "partition-scoped maintenance correlation domain 0 is blank"},
		{"domain duplicate", func(s *Store) { s.CorrelationDomains = []string{"correlation-a", "correlation-a"} }, "partition-scoped maintenance correlation domain \"correlation-a\" is duplicated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &configProbe{}
			callbacks := 0
			s := configuredStore(p, &callbacks)
			s.Backfill.DB = nil // The standalone reopen entrypoint does not need it.
			tc.edit(&s)
			err := reopenConfigError(s)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if p.queries != 0 || p.execs != 0 || p.begins != 0 || callbacks != 0 {
				t.Fatalf("invalid reopen touched database/transaction/callback: queries=%d execs=%d begins=%d callbacks=%d", p.queries, p.execs, p.begins, callbacks)
			}
		})
	}
}

func reopenConfigError(s Store) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("panic: %v", value)
		}
	}()
	_, err = s.ReopenTargetedMaintenanceWorkItems(context.Background(), nil, configAffected, nil)
	return err
}

func TestTargetedReopenEmptyAffectedIsNoOpBeforeValidation(t *testing.T) {
	result, err := (Store{}).ReopenTargetedMaintenanceWorkItems(context.Background(), nil, nil, nil)
	if err != nil || result == nil || len(result) != 0 {
		t.Fatalf("empty reopen result=%v error=%v", result, err)
	}
}

// BenchmarkTargetedPassConfigValidation measures the incremental validation
// on the original pass-start checks. The four representative domain strings
// match the root list's cardinality and length range without copying its
// membership into this child package.
func BenchmarkTargetedPassConfigValidation(b *testing.B) {
	p := &configProbe{}
	callbacks := 0
	s := configuredStore(p, &callbacks)
	s.CorrelationDomains = []string{
		"relationship_domain_alpha_01",
		"relationship_domain_beta_materialization",
		"relationship_domain_gamma_03",
		"relationship_domain_delta_04",
	}
	b.Run("preexisting_prefix", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			configBenchmarkErr = preexistingPassConfigChecks(s, configOwed)
		}
	})
	b.Run("with_config_validation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			err := preexistingPassConfigChecks(s, configOwed)
			if err == nil {
				err = s.validatePassConfig()
			}
			configBenchmarkErr = err
		}
	})
}

//go:noinline
func preexistingPassConfigChecks(s Store, owed []OwedPartition) error {
	if s.Backfill.DB == nil {
		return fmt.Errorf("ingestion store db is required")
	}
	if s.Backfill.Beginner == nil {
		return fmt.Errorf("transaction beginner is required for partition-scoped maintenance")
	}
	if len(normalizeOwedPartitions(owed)) == 0 {
		return fmt.Errorf("partition-scoped maintenance requires at least one owed partition")
	}
	return nil
}
