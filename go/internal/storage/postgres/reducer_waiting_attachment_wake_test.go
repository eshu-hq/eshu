// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countingWakeDB records every statement the producer-terminal wake issues,
// with its arguments, and answers each single-row read with zero reopened
// and zero cleared rows.
type countingWakeDB struct {
	queries []string
	args    [][]any
}

func (f *countingWakeDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	f.args = append(f.args, args)
	return &zeroCountWakeRows{}, nil
}

func (f *countingWakeDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.queries = append(f.queries, query)
	return nil, nil
}

// zeroCountWakeRows answers one row of int64 zeros.
type zeroCountWakeRows struct{ done bool }

func (r *zeroCountWakeRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *zeroCountWakeRows) Scan(dest ...any) error {
	for _, d := range dest {
		target, ok := d.(*int64)
		if !ok {
			return fmt.Errorf("zeroCountWakeRows.Scan: dest %T, want *int64", d)
		}
		*target = 0
	}
	return nil
}

func (r *zeroCountWakeRows) Err() error   { return nil }
func (r *zeroCountWakeRows) Close() error { return nil }

// TestSBOMAttestationAttachmentAckWakeStatementCount pins the post-commit
// statement cost on the narrowed producer Ack paths. The attachment and
// impact domains are narrowed but same-scope (#11114), so each acked
// generation issues exactly one statement, the keyed reopen, and no
// cross-scope leg. workload_materialization carries the cross-scope catalog
// class, so its Ack adds the cross-scope leg (deduped per domain across a
// batch) filtered to that class, plus the domain-only #11000 wake when the
// domain is a CompletionEdges producer. A domain with no deferral-map row
// still issues none.
func TestSBOMAttestationAttachmentAckWakeStatementCount(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)
	intent := func(domain reducercontract.Domain, scope string) reducercontract.Intent {
		return reducercontract.Intent{IntentID: "i-" + scope, Domain: domain, ScopeID: scope, GenerationID: "g-" + scope}
	}
	producers := completionEdgeProducerSet()
	domainOnly := func(domain reducercontract.Domain) int {
		if producers[domain] {
			return 1
		}
		return 0
	}
	cases := []struct {
		name    string
		intents []reducercontract.Intent
		want    int
	}{
		{"one attachment ack", []reducercontract.Intent{intent(reducercontract.DomainSBOMAttestationAttachment, "s1")}, 1},
		{"batch of two attachment acks", []reducercontract.Intent{
			intent(reducercontract.DomainSBOMAttestationAttachment, "s1"),
			intent(reducercontract.DomainSBOMAttestationAttachment, "s2"),
		}, 2},
		{"one impact ack", []reducercontract.Intent{intent(reducercontract.DomainSupplyChainImpact, "s1")}, 1 + domainOnly(reducercontract.DomainSupplyChainImpact)},
		{"mixed batch: two workload acks and one impact ack", []reducercontract.Intent{
			intent(reducercontract.DomainWorkloadMaterialization, "s1"),
			intent(reducercontract.DomainWorkloadMaterialization, "s2"),
			intent(reducercontract.DomainSupplyChainImpact, "s3"),
		}, 2 + 1 + domainOnly(reducercontract.DomainWorkloadMaterialization) + 1 + domainOnly(reducercontract.DomainSupplyChainImpact)},
		{"a domain nothing waits on", []reducercontract.Intent{intent(reducercontract.DomainKubernetesCorrelation, "s1")}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := &countingWakeDB{}
			if err := wakeProducerTerminalAfterAck(context.Background(), database, nil, tc.intents, now); err != nil {
				t.Fatalf("wakeProducerTerminalAfterAck() error = %v", err)
			}
			if len(database.queries) != tc.want {
				t.Fatalf("statements = %d, want %d:\n%s", len(database.queries), tc.want, strings.Join(database.queries, "\n---\n"))
			}
			for i, query := range database.queries {
				if query != wakeProducerTerminalCrossScopeQuery {
					continue
				}
				if got := database.args[i][1]; got != string(reducercontract.DomainWorkloadMaterialization) {
					t.Fatalf("cross-scope leg issued for domain %v, want only workload_materialization", got)
				}
				if got := fmt.Sprint(database.args[i][2]); got != fmt.Sprint([]string{servicecatalog.RuntimeNotReadyFailureClass}) {
					t.Fatalf("cross-scope leg classes = %s, want only the catalog runtime class", got)
				}
			}
		})
	}
	if !defaultProducerTerminalWakePlan().narrowed[string(reducercontract.DomainSBOMAttestationAttachment)] {
		t.Fatal("sbom_attestation_attachment is not a narrowed producer domain: its Ack would never wake a parked impact pass")
	}
}
