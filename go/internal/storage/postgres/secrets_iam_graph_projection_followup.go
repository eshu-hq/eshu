// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// graphProjectionFollowUpReason is the audit string stamped onto every
// secrets/IAM graph-projection follow-up intent. It identifies the
// trust-chain handler as the producer for operator log review.
const graphProjectionFollowUpReason = "secrets/IAM trust-chain read models committed"

// SecretsIAMGraphProjectionFollowUpWriter enqueues the secrets/IAM
// graph-projection follow-up intent through the durable reducer queue. It
// implements secretsiam.GraphProjectionFollowUpWriter so the trust-chain
// handler can trigger the projection stage once its read-model writes
// commit. The row's work_item_id is stable per scope generation
// (domain + entity key), so a retried trust-chain execution collapses onto
// the same row through ON CONFLICT DO NOTHING instead of duplicating
// projection work.
type SecretsIAMGraphProjectionFollowUpWriter struct {
	database db.ExecQueryer
	Now      func() time.Time
}

// NewSecretsIAMGraphProjectionFollowUpWriter constructs the follow-up writer
// over the given database handle.
func NewSecretsIAMGraphProjectionFollowUpWriter(database db.ExecQueryer) SecretsIAMGraphProjectionFollowUpWriter {
	return SecretsIAMGraphProjectionFollowUpWriter{database: database}
}

// EnqueueGraphProjectionFollowUp admits one secrets_iam_graph_projection
// reducer intent for the scope generation whose read models just committed.
func (w SecretsIAMGraphProjectionFollowUpWriter) EnqueueGraphProjectionFollowUp(
	ctx context.Context,
	scopeID string,
	generationID string,
	sourceSystem string,
) error {
	if w.database == nil {
		return fmt.Errorf("graph projection follow-up database is required")
	}
	queue := ReducerQueue{database: w.database}
	if w.Now != nil {
		queue.Now = w.Now
	}
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSecretsIAMGraphProjection,
		EntityKey:    "secrets_iam_graph_projection:" + scopeID,
		Reason:       graphProjectionFollowUpReason,
		SourceSystem: sourceSystem,
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		return fmt.Errorf("enqueue secrets/IAM graph projection follow-up: %w", err)
	}
	return nil
}

// graphProjectionFollowUpWriterContract pins the implementation to the
// handler's collaborator interface at compile time.
var _ secretsiam.GraphProjectionFollowUpWriter = SecretsIAMGraphProjectionFollowUpWriter{}
