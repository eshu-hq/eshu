// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type producerSettleFakeResult int64

func (r producerSettleFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r producerSettleFakeResult) RowsAffected() (int64, error) { return int64(r), nil }

type producerSettleFakeRows struct {
	rows  [][]any
	index int
}

func (r *producerSettleFakeRows) Next() bool { return r.index < len(r.rows) }

func (r *producerSettleFakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called past end")
	}
	row := r.rows[r.index]
	r.index++
	if len(dest) != len(row) {
		return errors.New("destination count mismatch")
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *sql.NullString:
			*target = row[i].(sql.NullString)
		case *string:
			*target = row[i].(string)
		case *int64:
			*target = row[i].(int64)
		case *bool:
			*target = row[i].(bool)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *producerSettleFakeRows) Err() error   { return nil }
func (r *producerSettleFakeRows) Close() error { return nil }

type producerSettleExecCall struct {
	query string
	args  []any
}

type producerSettleFakeTx struct {
	activeGeneration string
	obligation       activation.ProducerObligation
	dependentsByDom  map[string][]string
	reopenRows       map[string]int64
	deleteErr        error

	execCalls  []producerSettleExecCall
	committed  bool
	rolledBack bool
}

func (tx *producerSettleFakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "AND work.domain = $1"):
		// The config-leg drift listing shares the drift domain but has no
		// staged intersection in this test's world: it always lists empty
		// so the state leg alone drives the reopen counts.
		if strings.Contains(query, "split_part(state_fact.scope_id") {
			return &producerSettleFakeRows{}, nil
		}
		domain, _ := args[0].(string)
		ids := tx.dependentsByDom[domain]
		out := make([][]any, 0, len(ids))
		for _, id := range ids {
			out = append(out, []any{id})
		}
		return &producerSettleFakeRows{rows: out}, nil
	case strings.Contains(query, "FROM ingestion_scopes") && strings.Contains(query, "FOR NO KEY UPDATE"):
		return &producerSettleFakeRows{rows: [][]any{{sql.NullString{String: tx.activeGeneration, Valid: true}}}}, nil
	case strings.Contains(query, "SELECT EXISTS") && strings.Contains(query, "FROM producer_activation_obligations"):
		return &producerSettleFakeRows{rows: [][]any{{true}}}, nil
	case strings.Contains(query, "FROM producer_activation_obligations") && strings.Contains(query, "FOR NO KEY UPDATE"):
		return &producerSettleFakeRows{rows: [][]any{{
			tx.obligation.LeaseToken, tx.obligation.LeaseOwner, string(activation.StateLeased), true,
		}}}, nil
	case strings.Contains(query, "SELECT EXISTS") && strings.Contains(query, "FROM fact_records AS fact"):
		return &producerSettleFakeRows{rows: [][]any{{true}}}, nil
	case strings.Contains(query, "SELECT keys.repo_key, keys.tag, keys.digest"):
		return &producerSettleFakeRows{}, nil
	case strings.Contains(query, "SELECT DISTINCT state_fact.payload->'attributes'->>'arn'"):
		return &producerSettleFakeRows{rows: [][]any{{sql.NullString{String: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", Valid: true}}}}, nil
	case strings.Contains(query, "backend.value->>'backend_kind'"):
		// Config-kinds probe (#11392): this test stages no config linkage,
		// so the owed generation carries no backend kinds.
		return &producerSettleFakeRows{}, nil
	default:
		return nil, errors.New("unexpected query: " + query)
	}
}

func (tx *producerSettleFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.execCalls = append(tx.execCalls, producerSettleExecCall{query: query, args: append([]any(nil), args...)})
	switch {
	case strings.Contains(query, "set_config('lock_timeout'"):
		return producerSettleFakeResult(1), nil
	case strings.Contains(query, "UPDATE fact_work_items"):
		id, _ := args[1].(string)
		return producerSettleFakeResult(tx.reopenRows[id]), nil
	case strings.Contains(query, "DELETE FROM aws_drift_reopen_fingerprints"):
		if tx.deleteErr != nil {
			return nil, tx.deleteErr
		}
		return producerSettleFakeResult(1), nil
	case strings.Contains(query, "UPDATE producer_activation_obligations"):
		return producerSettleFakeResult(1), nil
	default:
		return nil, errors.New("unexpected exec: " + query)
	}
}

func (tx *producerSettleFakeTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *producerSettleFakeTx) Rollback() error {
	tx.rolledBack = true
	return nil
}

type producerSettleFakeDB struct {
	tx *producerSettleFakeTx
}

func (f *producerSettleFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("unexpected non-tx query")
}

func (f *producerSettleFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected non-tx exec")
}

func (f *producerSettleFakeDB) Begin(context.Context) (db.Transaction, error) {
	return f.tx, nil
}

func deletedFingerprintWorkItemIDs(calls []producerSettleExecCall) []string {
	var ids []string
	for _, call := range calls {
		if strings.Contains(call.query, "DELETE FROM aws_drift_reopen_fingerprints") && len(call.args) == 1 {
			if id, ok := call.args[0].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// TestSettleClaimedProducerActivationClearsAWSDriftReopenFingerprint pins the
// #7635 + #10878 interaction: reopening a succeeded aws_cloud_runtime_drift
// work item on Terraform state activation must delete its
// aws_drift_reopen_fingerprints row inside the settle transaction so
// AWSDriftIncrementalPlanner cannot skip the reopened run as input_unchanged.
// Non-drift domains and un-reopened drift items must not issue a delete.
func TestSettleClaimedProducerActivationClearsAWSDriftReopenFingerprint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
	work := activation.ProducerObligation{
		ScopeID:      "state_snapshot:s3:prod",
		GenerationID: "gen-state-2",
		LeaseOwner:   "test-owner",
		LeaseToken:   7,
		LeaseUntil:   now.Add(time.Minute),
		CreatedAt:    now,
	}

	t.Run("reopened drift item clears fingerprint; non-drift domain does not", func(t *testing.T) {
		t.Parallel()
		tx := &producerSettleFakeTx{
			activeGeneration: work.GenerationID,
			obligation:       work,
			dependentsByDom: map[string][]string{
				string(reducer.DomainKubernetesCorrelationMaterialization): {"work-k8s-1"},
				string(reducer.DomainAWSCloudRuntimeDrift):                 {"work-aws-drift-1"},
			},
			reopenRows: map[string]int64{
				"work-k8s-1":       1,
				"work-aws-drift-1": 1,
			},
		}
		store := NewIngestionStore(&producerSettleFakeDB{tx: tx})
		store.Now = func() time.Time { return now }

		counts, outcome, err := store.SettleClaimedProducerActivation(t.Context(), work)
		if err != nil {
			t.Fatalf("SettleClaimedProducerActivation() error = %v, want nil", err)
		}
		if outcome != activation.ProducerCompleted {
			t.Fatalf("outcome = %q, want completed", outcome)
		}
		if !tx.committed {
			t.Fatal("tx.committed = false, want true")
		}
		if counts[string(reducer.DomainAWSCloudRuntimeDrift)] != 1 || counts[string(reducer.DomainKubernetesCorrelationMaterialization)] != 1 {
			t.Fatalf("counts = %v, want 1 drift + 1 k8s reopened", counts)
		}
		deleted := deletedFingerprintWorkItemIDs(tx.execCalls)
		if len(deleted) != 1 || deleted[0] != "work-aws-drift-1" {
			t.Fatalf("deleted aws_drift_reopen_fingerprints work_item_ids = %v, want [work-aws-drift-1]", deleted)
		}
	})

	t.Run("not-reopened drift item does not clear fingerprint", func(t *testing.T) {
		t.Parallel()
		tx := &producerSettleFakeTx{
			activeGeneration: work.GenerationID,
			obligation:       work,
			dependentsByDom: map[string][]string{
				string(reducer.DomainAWSCloudRuntimeDrift): {"work-aws-drift-already-open"},
			},
			reopenRows: map[string]int64{
				"work-aws-drift-already-open": 0,
			},
		}
		store := NewIngestionStore(&producerSettleFakeDB{tx: tx})
		store.Now = func() time.Time { return now }

		counts, outcome, err := store.SettleClaimedProducerActivation(t.Context(), work)
		if err != nil {
			t.Fatalf("SettleClaimedProducerActivation() error = %v, want nil", err)
		}
		if outcome != activation.ProducerCompleted {
			t.Fatalf("outcome = %q, want completed", outcome)
		}
		if counts[string(reducer.DomainAWSCloudRuntimeDrift)] != 0 {
			t.Fatalf("counts = %v, want 0 drift reopened", counts)
		}
		if deleted := deletedFingerprintWorkItemIDs(tx.execCalls); len(deleted) != 0 {
			t.Fatalf("deleted aws_drift_reopen_fingerprints work_item_ids = %v, want none when reopened=false", deleted)
		}
	})

	t.Run("delete error rolls back settle transaction", func(t *testing.T) {
		t.Parallel()
		tx := &producerSettleFakeTx{
			activeGeneration: work.GenerationID,
			obligation:       work,
			dependentsByDom: map[string][]string{
				string(reducer.DomainAWSCloudRuntimeDrift): {"work-aws-drift-err"},
			},
			reopenRows: map[string]int64{
				"work-aws-drift-err": 1,
			},
			deleteErr: errors.New("fingerprint delete failed"),
		}
		store := NewIngestionStore(&producerSettleFakeDB{tx: tx})
		store.Now = func() time.Time { return now }

		if _, _, err := store.SettleClaimedProducerActivation(t.Context(), work); err == nil {
			t.Fatal("SettleClaimedProducerActivation() error = nil, want delete error")
		}
		if tx.committed {
			t.Fatal("tx.committed = true, want false on delete error")
		}
		if !tx.rolledBack {
			t.Fatal("tx.rolledBack = false, want true on delete error")
		}
	})
}
