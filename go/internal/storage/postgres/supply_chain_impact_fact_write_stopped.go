// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// The finding-fact write stop (#10880 PR-C). When the stop lands, the database
// refuses every INSERT and UPDATE of a reducer_supply_chain_impact_finding row
// in fact_records with SQLSTATE 55000 and the constraint name below. A reducer on
// an image that still writes the kind then fails inside its writer transaction.
// This file turns that refusal into a retryable, non-counting error so the intent
// waits for the new image instead of dead-lettering, and counts each refusal.
// Nothing raises the error until the trigger exists, so classifying it changes no
// behavior of a current path.

// SupplyChainImpactFactWriteStoppedFailureClass classifies a reducer write the
// finding-fact reject trigger refused. It is enrolled in
// nonCountingReducerRetryFailureClasses (reducer_queue_readiness_sql.go): the
// refusal means this pod's image is older than the schema, which a rollout fixes,
// and a counting class would dead-letter every impact intent such a pod claims
// during the roll.
const SupplyChainImpactFactWriteStoppedFailureClass = "supply_chain_impact_fact_write_stopped"

// supplyChainImpactFactWriteRejectConstraint is the CONSTRAINT the reject
// trigger names in its RAISE EXCEPTION. SQLSTATE 55000 alone is not enough:
// object_not_in_prerequisite_state is raised for unrelated reasons.
const supplyChainImpactFactWriteRejectConstraint = "fact_records_reject_supply_chain_impact_finding"

// supplyChainImpactFactWriteRejectSQLState is object_not_in_prerequisite_state.
const supplyChainImpactFactWriteRejectSQLState = "55000"

// SupplyChainImpactFactWriteStoppedError is a finding-fact write the reject
// trigger refused. It is retryable and non-counting. It is not an expected wait,
// but the reducer service still reports it as a deferral: any retryable error
// whose class is in the non-counting set is deferred (Service.isDeferredFailure),
// so the execution status is "deferred", the log line is a WARN, and the counter
// eshu_dp_supply_chain_impact_fact_write_rejected_total is the signal an operator
// watches: a pod that hits it runs an image older than the schema.
type SupplyChainImpactFactWriteStoppedError struct {
	// Err is the underlying database error, kept for errors.As to *pgconn.PgError.
	Err error
}

// Error implements error.
func (e *SupplyChainImpactFactWriteStoppedError) Error() string {
	return fmt.Sprintf("supply chain impact finding facts are no longer written; this reducer image is older than the schema: %v", e.Err)
}

// Unwrap returns the database error.
func (e *SupplyChainImpactFactWriteStoppedError) Unwrap() error { return e.Err }

// Retryable marks the refusal retryable: it clears when the pod runs the new image.
func (*SupplyChainImpactFactWriteStoppedError) Retryable() bool { return true }

// FailureClass names the non-counting class for the queue.
func (*SupplyChainImpactFactWriteStoppedError) FailureClass() string {
	return SupplyChainImpactFactWriteStoppedFailureClass
}

// classifySupplyChainImpactFactWriteStopped wraps err as the stopped-write error
// when it is the reject trigger's refusal: SQLSTATE 55000 and the trigger's
// constraint name together. Any other error, and nil, comes back unchanged.
func classifySupplyChainImpactFactWriteStopped(err error) error {
	var already *SupplyChainImpactFactWriteStoppedError
	var pgErr *pgconn.PgError
	if err == nil || errors.As(err, &already) || !errors.As(err, &pgErr) {
		return err
	}
	if pgErr.Code != supplyChainImpactFactWriteRejectSQLState || pgErr.ConstraintName != supplyChainImpactFactWriteRejectConstraint {
		return err
	}
	return &SupplyChainImpactFactWriteStoppedError{Err: err}
}

// classifyFactWriteStopped classifies a statement error on the writer
// transaction and counts the refusal once. Only the call that creates the
// classification counts: an error that is already classified, or any other
// error, passes through without touching the counter. The counter has no label,
// so no scope, generation or fact id reaches the metric.
func (t supplyChainImpactTx) classifyFactWriteStopped(ctx context.Context, err error) error {
	var stopped *SupplyChainImpactFactWriteStoppedError
	if err == nil || errors.As(err, &stopped) {
		return err
	}
	classified := classifySupplyChainImpactFactWriteStopped(err)
	if errors.As(classified, &stopped) {
		t.instruments.RecordSupplyChainImpactFactWriteRejected(ctx)
	}
	return classified
}
