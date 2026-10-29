// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PublicAdvisoryGenerationAuthority is a protected, producer-bound capability.
// Stage, RecordAcceptedPublicAdvisoryFacts, and RecordComplete run in the fact
// transaction. PublishComplete runs
// only after that transaction commits, using a separate short transaction.
// It must accept only the same live claim and its complete staged receipt; a
// missing receipt returns false, not an authority grant from existing facts.
type PublicAdvisoryGenerationAuthority interface {
	StagePublicAdvisoryGeneration(context.Context, db.ExecQueryer,
		workflow.ClaimMutation, string, string) (string, error)
	RecordCompletePublicAdvisoryGeneration(context.Context, db.ExecQueryer,
		string, string, string) error
	RecordAcceptedPublicAdvisoryFacts(context.Context, db.ExecQueryer,
		string, []facts.Envelope) error
	PublishCompletePublicAdvisoryGeneration(context.Context,
		workflow.ClaimMutation, string, string) (bool, error)
}

// CommitClaimedPublicAdvisoryGeneration stages a typed advisory snapshot before
// claim or fact locks, records the complete persisted fact set in that fact
// transaction, then publishes through a protected short transaction. A retry
// of a finalized generation can publish only its existing complete receipt
// under the same current claim; it cannot stage old generic facts retroactively.
// The caller must supply an authority implementation bound to a dedicated
// advisory producer login. Ordinary collector commits never call this path.
func (s IngestionStore) CommitClaimedPublicAdvisoryGeneration(
	ctx context.Context, mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope, generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope, factStreamErr func() error,
	authority PublicAdvisoryGenerationAuthority,
) error {
	if factStream == nil {
		return errors.New("public advisory fact stream is required")
	}
	if authority == nil || scopeValue.ScopeKind != scope.KindVulnerabilityIntelligence ||
		scopeValue.CollectorKind != scope.CollectorVulnerabilityIntelligence ||
		scopeValue.SourceSystem != string(scope.CollectorVulnerabilityIntelligence) ||
		generation.ScopeID != scopeValue.ScopeID {
		return errors.Join(errors.New("public advisory producer boundary invalid"),
			drainFactsAndCheckStream(factStream, factStreamErr))
	}
	if err := validateClaimMutation(mutation); err != nil {
		return errors.Join(err, drainFactsAndCheckStream(factStream, factStreamErr))
	}
	// A public approval requires an actual generation commit. In particular, a
	// freshness shortcut must not become a new public authorization receipt.
	generation.FreshnessHint = ""
	var receiptID string
	beforeClaim := func(ctx context.Context, tx db.ExecQueryer) error {
		var err error
		receiptID, err = authority.StagePublicAdvisoryGeneration(ctx, tx, mutation,
			scopeValue.ScopeID, generation.GenerationID)
		if err != nil {
			return fmt.Errorf("stage public advisory generation: %w", err)
		}
		if receiptID == "" {
			return errors.New("public advisory stage returned no receipt")
		}
		return nil
	}
	stateWriter := func(ctx context.Context, executor collector.CommitExecutor) error {
		tx, ok := executor.(db.ExecQueryer)
		if !ok || receiptID == "" {
			return errors.New("public advisory completion requires staged transaction")
		}
		if err := authority.RecordCompletePublicAdvisoryGeneration(ctx, tx,
			receiptID, scopeValue.ScopeID, generation.GenerationID); err != nil {
			return fmt.Errorf("record complete public advisory generation: %w", err)
		}
		return nil
	}
	onAcceptedBatch := func(ctx context.Context, tx db.ExecQueryer, batch []facts.Envelope) error {
		if receiptID == "" {
			return errors.New("public advisory batch has no staged receipt")
		}
		return authority.RecordAcceptedPublicAdvisoryFacts(ctx, tx, receiptID, batch)
	}
	if err := s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation,
		factStream, factStreamErr, stateWriter, beforeClaim, onAcceptedBatch); err != nil {
		return err
	}
	// A finalized replay bypasses beforeClaim/stateWriter. The protected
	// publication capability may recover only an already complete staged
	// receipt for this exact still-current claim and generation.
	published, err := publishPublicAdvisoryWithRetry(ctx, authority, mutation,
		scopeValue.ScopeID, generation.GenerationID)
	if err != nil {
		return err
	}
	if !published {
		return publicAdvisoryTerminalError{reason: "receipt_incomplete"}
	}
	return nil
}
