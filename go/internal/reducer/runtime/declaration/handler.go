// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// EvidenceSource is the evidence_source every RuntimeDeclaration node and
// DECLARES_RUNTIME edge this domain writes carries. The retracts filter on it,
// so changing it orphans every node already written.
const EvidenceSource = "reducer/runtime_declaration"

// Writer is the graph port the handler writes through. cmd/reducer wires the
// storage/cypher RuntimeDeclarationWriter; the methods are idempotent per
// declaration key so a retried intent rewrites the same nodes.
type Writer interface {
	// WriteRuntimeDeclarations upserts one RuntimeDeclaration node and its
	// DECLARES_RUNTIME edge per row.
	WriteRuntimeDeclarations(ctx context.Context, rows []map[string]any, generationID, evidenceSource string) error
	// RetractRuntimeDeclarations removes every declaration this evidence
	// source owns for the given repositories.
	RetractRuntimeDeclarations(ctx context.Context, repoIDs []string, evidenceSource string) error
	// RetractRuntimeDeclarationsByPath removes the declarations whose
	// relative_path is listed for their own repository.
	RetractRuntimeDeclarationsByPath(ctx context.Context, pathsByRepoID map[string][]string, evidenceSource string) error
}

// Definition returns the registration for the runtime_declaration_projection
// domain. The handler is attached by the reducer root's domain catalog.
func Definition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainRuntimeDeclarationProjection,
		Summary: "project runtime_declaration.version facts into RuntimeDeclaration nodes linked from their Repository by DECLARES_RUNTIME",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "runtime_declaration",
			SourceLayers:  []truth.Layer{truth.LayerSourceDeclaration},
		},
	}
}

// Handler projects runtime_declaration.version facts to the graph (issue
// #8816). Each run retracts the declarations the generation supersedes, then
// writes one RuntimeDeclaration per fact:
//
//   - a full generation retracts every declaration of each repository it
//     covers, because its facts are the repository's complete pin-file set;
//   - a delta generation retracts only the declarations of the files it
//     changed or deleted, because its facts cover only the changed pin files
//     and a whole-repository retract would erase the unchanged ones.
type Handler struct {
	FactLoader           factload.FactLoader
	Writer               Writer
	PriorGenerationCheck reducercontract.PriorGenerationCheck
	// Instruments records eshu_dp_reducer_input_invalid_facts_total for a
	// quarantined fact. A nil Instruments skips the counter only.
	Instruments *telemetry.Instruments
}

var materializationFactKinds = []string{factload.FactKindRepository, facts.RuntimeDeclarationVersionFactKind}

// Handle executes one runtime_declaration_projection intent.
func (h Handler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainRuntimeDeclarationProjection {
		return reducercontract.Result{}, fmt.Errorf("runtime declaration handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("runtime declaration fact loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("runtime declaration writer is required")
	}

	envelopes, err := factload.LoadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, materializationFactKinds)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load facts for runtime declaration projection: %w", err)
	}
	scope := buildRetractScope(envelopes)
	rows, quarantined, err := ExtractRows(envelopes)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("extract runtime declaration rows: %w", err)
	}
	inputInvalid := factdecode.RecordQuarantinedFacts(ctx, h.Instruments, reducercontract.DomainRuntimeDeclarationProjection, intent.ScopeID, intent.GenerationID, quarantined)
	scope.addRowRepositories(rows)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	wholeRepoIDs := scope.wholeRepoIDs()
	if !skipRetract {
		if len(wholeRepoIDs) > 0 {
			if err := h.Writer.RetractRuntimeDeclarations(ctx, wholeRepoIDs, EvidenceSource); err != nil {
				return reducercontract.Result{}, fmt.Errorf("retract runtime declarations (whole repository): %w", err)
			}
		}
		if len(scope.deltaPaths) > 0 {
			if err := h.Writer.RetractRuntimeDeclarationsByPath(ctx, scope.deltaPaths, EvidenceSource); err != nil {
				return reducercontract.Result{}, fmt.Errorf("retract runtime declarations (delta paths): %w", err)
			}
		}
	}
	if len(rows) > 0 {
		if err := h.Writer.WriteRuntimeDeclarations(ctx, rows, intent.GenerationID, EvidenceSource); err != nil {
			return reducercontract.Result{}, fmt.Errorf("write runtime declarations: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "runtime declaration projection completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.Int("declaration_count", len(rows)),
		slog.Int("whole_repo_retract_count", len(wholeRepoIDs)),
		slog.Int("delta_retract_repo_count", len(scope.deltaPaths)),
		slog.Bool("retract_skipped", skipRetract),
		slog.Int("input_invalid_count", inputInvalid),
	)

	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainRuntimeDeclarationProjection,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("projected %d runtime declarations", len(rows)),
		CanonicalWrites: len(rows),
		SubSignals:      factdecode.InputInvalidSubSignals(inputInvalid),
	}, nil
}

// shouldSkipRetract skips the retract only on the first attempt of a
// generation that has no prior generation: nothing can exist to retract. A
// retry always retracts, because the failed attempt may have written.
func (h Handler) shouldSkipRetract(ctx context.Context, intent reducercontract.Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for runtime declaration retract: %w", err)
	}
	return !hasPrior, nil
}
