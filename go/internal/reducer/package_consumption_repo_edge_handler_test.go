// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestPackageSourceCorrelationHandlerEmitsRefreshIntentWhenOwnerDisappears proves
// the handler wires BuildPackageConsumptionRepoEdgeRefreshIntents into the shared
// repo-dependency lane: a consumer that declares a package dependency whose owner
// cannot be resolved this generation must enqueue a refresh/retract intent so any
// package-consumption edge it held in a prior generation is removed instead of
// left orphaned (issue #3579, review comment 3455350032).
func TestPackageSourceCorrelationHandlerEmitsRefreshIntentWhenOwnerDisappears(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	loader := &stubPackageSourceFactLoader{
		// Package exists with a source hint, but no repository fact matches the
		// hint, so ownership stays unresolved while consumption is declared.
		scopeFacts: []facts.Envelope{
			packageRegistryPackageFact(
				"pkg:npm://registry.example/orphan-api",
				"npm",
				"orphan-api",
				"",
				observedAt,
			),
			packageSourceHintFact(
				"pkg:npm://registry.example/orphan-api",
				"repository",
				"https://github.com/acme/orphan-api",
				observedAt,
			),
		},
		manifestDependencies: []facts.Envelope{
			packageManifestDependencyFact(
				"repo-consumer",
				"consumer",
				"package.json",
				"orphan-api",
				"npm",
				"^1.0.0",
				observedAt,
			),
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := PackageSourceCorrelationHandler{
		FactLoader:                 loader,
		Writer:                     &recordingPackageCorrelationWriter{},
		RepoDependencyIntentWriter: intentWriter,
		Now:                        func() time.Time { return observedAt },
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-orphan",
		ScopeID:         "package-registry:npm:orphan-api",
		GenerationID:    "generation-2",
		SourceSystem:    "package_registry",
		Domain:          DomainPackageSourceCorrelation,
		Cause:           "package registry source hints observed",
		RelatedScopeIDs: []string{"package-registry:npm:orphan-api"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}

	if len(intentWriter.rows) != 1 {
		t.Fatalf("RepoDependencyIntentWriter calls = %d, want 1", len(intentWriter.rows))
	}
	rows := intentWriter.rows[0]
	if len(rows) != 1 {
		t.Fatalf("enqueued intents = %d, want 1 refresh/retract intent", len(rows))
	}
	row := rows[0]
	if got := anyToString(row.Payload["action"]); got != "retract" {
		t.Fatalf("intent action = %q, want retract", got)
	}
	if got := anyToString(row.Payload["evidence_source"]); got != packageConsumptionEvidenceSource {
		t.Fatalf("intent evidence_source = %q, want %q", got, packageConsumptionEvidenceSource)
	}
	if got := anyToString(row.Payload["repo_id"]); got != "repo-consumer" {
		t.Fatalf("intent repo_id = %q, want repo-consumer", got)
	}
	// The refresh intent must carry the stable scope-only source-run id so it
	// targets the same acceptance unit a prior generation's upsert wrote.
	if want := packageConsumptionRepoEdgeSourceRunID("package-registry:npm:orphan-api", "generation-2"); row.SourceRunID != want {
		t.Fatalf("intent SourceRunID = %q, want %q", row.SourceRunID, want)
	}
}
