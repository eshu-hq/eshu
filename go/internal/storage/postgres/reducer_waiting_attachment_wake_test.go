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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countingWakeDB records every statement the producer-terminal wake issues and
// answers each single-row read with zero reopened and zero cleared rows.
type countingWakeDB struct {
	queries []string
}

func (f *countingWakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
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

// TestSBOMAttestationAttachmentAckWakeStatementCount pins the #11105 cost on
// the attachment Ack path: enrolling sbom_attestation_attachment as a narrowed
// producer domain adds exactly two post-commit statements per acked
// attachment generation (the narrowed reopen and the cross-scope domain leg,
// the latter deduped per domain across a batch), where the domain added none
// before. A domain with no deferral-map row still issues none.
func TestSBOMAttestationAttachmentAckWakeStatementCount(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)
	intent := func(domain reducercontract.Domain, scope string) reducercontract.Intent {
		return reducercontract.Intent{IntentID: "i-" + scope, Domain: domain, ScopeID: scope, GenerationID: "g-" + scope}
	}
	cases := []struct {
		name    string
		intents []reducercontract.Intent
		want    int
	}{
		{"one attachment ack", []reducercontract.Intent{intent(reducercontract.DomainSBOMAttestationAttachment, "s1")}, 2},
		{"batch of two attachment acks", []reducercontract.Intent{
			intent(reducercontract.DomainSBOMAttestationAttachment, "s1"),
			intent(reducercontract.DomainSBOMAttestationAttachment, "s2"),
		}, 3},
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
		})
	}
	if !waitingProducerDomains()[string(reducercontract.DomainSBOMAttestationAttachment)] {
		t.Fatal("sbom_attestation_attachment is not a narrowed producer domain: its Ack would never wake a parked impact pass")
	}
}
