// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// idempotencyReplayFencingToken is the single fencing token stamped on every
// fact a replay case loads. The acceptance criterion (#3799) requires replaying
// each reducer's emit path twice "with the same FencingToken"; stamping a shared,
// non-zero token on the input facts makes the input byte-identical across both
// replays, so the only variable under test is the replay itself.
const idempotencyReplayFencingToken int64 = 7

// idempotencyExemptDomains records DefaultDomainDefinitions() domains that the
// B-6 replay suite deliberately does not unit-replay, each with a one-line
// reason. Keep this set minimal: prefer adding a real replay case.
//
// Both exempt domains are heavy cross-source fan-in handlers whose emit path has
// no single constructible unit fixture: they require a live graph read-back and
// a web of collaborators (readiness lookups, cross-repo resolvers, generation
// replayers, infrastructure-platform lookups, repair queues) before they emit
// anything. Their idempotency is already proven by their own dedicated suites,
// cited below, rather than by this registry-driven unit replay.
var idempotencyExemptDomains = map[Domain]string{
	// deployment_mapping (PlatformMaterializationHandler) emits only after the
	// cross-repo resolver + readiness lookups admit work; its reprojection
	// idempotency is covered by cross_repo_resolution_*_test.go and
	// platform_materialization_test.go. No constructible single-fixture emit path.
	DomainDeploymentMapping: "requires cross-repo resolver + readiness graph read-back; idempotency covered by cross_repo_resolution_*_test.go and platform_materialization_test.go",
	// workload_materialization needs a materializer, multiple loaders, an
	// infrastructure-platform lookup, a repair queue, and an endpoint-presence
	// writer wired before it emits; its reprojection idempotency is covered by
	// workload_materialization_*_test.go. No constructible single-fixture emit path.
	DomainWorkloadMaterialization: "requires materializer + multi-loader + repair-queue graph read-back; idempotency covered by workload_materialization_*_test.go",
}

// idempotencyReplayCases returns one replay case per covered reducer domain.
//
// The covered set is the 12 of 14 DefaultDomainDefinitions() base-catalog domains
// whose emit path is drivable with an in-memory recording fake and a static fact
// fixture. The two base graph-read-back domains are exempted above with cited
// coverage. The additive, adapter-gated domains registered by
// appendAdditiveDomainDefinitions are held to the same coverage bar by
// TestReducerIdempotencyCoverageGuard and exempted in
// idempotencyAdditiveExemptDomains (idempotency_additive_test.go), each citing
// the dedicated suite that proves its reprojection idempotency.
func idempotencyReplayCases() []idempotencyReplayCase {
	return []idempotencyReplayCase{
		workloadIdentityReplayCase(),
		cloudAssetResolutionReplayCase(),
		codeCallReplayCase(),
		sqlRelationshipReplayCase(),
		shellExecReplayCase(),
		inheritanceReplayCase(),
		rationaleReplayCase(),
		semanticEntityReplayCase(),
		documentationReplayCase(),
		platformInfraReplayCase(),
		codeownersOwnershipReplayCase(),
		submodulePinReplayCase(),
	}
}

// fencedFacts returns a copy of envs with the shared replay fencing token
// stamped on every envelope, so both replays load byte-identical input.
func fencedFacts(envs []facts.Envelope) []facts.Envelope {
	out := make([]facts.Envelope, len(envs))
	for i, env := range envs {
		clone := env.Clone()
		clone.FencingToken = idempotencyReplayFencingToken
		out[i] = clone
	}
	return out
}

// sharedIntentRows maps captured durable shared-projection intent rows to
// identity-keyed replay rows. The durable upsert MERGEs on IntentID
// (BuildSharedProjectionIntent derives it deterministically from the stable
// scope/generation/partition identity), so IntentID is the MERGE identity.
func sharedIntentRows(rows []SharedProjectionIntentRow) []idempotencyRow {
	out := make([]idempotencyRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, idempotencyRow{
			identity: row.IntentID,
			contents: fmt.Sprintf(
				"domain=%s;partition=%s;scope=%s;repo=%s;gen=%s;run=%s;payload=%s",
				row.ProjectionDomain, row.PartitionKey, row.ScopeID, row.RepositoryID,
				row.GenerationID, row.SourceRunID, stableRowContents(row.Payload),
			),
		})
	}
	return out
}

func replayIntent(domain Domain, scopeID string, keys []string) Intent {
	now := time.Date(2026, time.June, 28, 12, 0, 0, 0, time.UTC)
	return Intent{
		IntentID:        "intent-replay-" + string(domain),
		ScopeID:         scopeID,
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          domain,
		Cause:           "b6 idempotency replay",
		EntityKeys:      keys,
		RelatedScopeIDs: []string{scopeID},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}
}

func workloadIdentityReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainWorkloadIdentity,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingWorkloadIdentityWriter{result: WorkloadIdentityWriteResult{CanonicalWrites: 1}}
			handler := WorkloadIdentityHandler{Writer: writer}
			intent := replayIntent(DomainWorkloadIdentity, "scope-wi", []string{"workload:checkout", "workload:cart"})
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("workload identity Handle: %v", err)
			}
			out := make([]idempotencyRow, 0, len(writer.requests))
			for _, req := range writer.requests {
				// The canonical workload-identity record MERGEs on the reconciled
				// scope generation; replaying the same intent rewrites the same row.
				out = append(out, idempotencyRow{
					identity: req.ScopeID + ":" + req.GenerationID,
					contents: fmt.Sprintf("keys=%v;related=%v;source=%s;cause=%s",
						req.EntityKeys, req.RelatedScopeIDs, req.SourceSystem, req.Cause),
				})
			}
			return out
		},
	}
}

func cloudAssetResolutionReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainCloudAssetResolution,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingCloudAssetResolutionWriter{result: CloudAssetResolutionWriteResult{CanonicalWrites: 1}}
			handler := CloudAssetResolutionHandler{Writer: writer}
			intent := replayIntent(DomainCloudAssetResolution, "scope-ca", []string{"arn:aws:s3:::bucket-a", "arn:aws:s3:::bucket-b"})
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("cloud asset Handle: %v", err)
			}
			out := make([]idempotencyRow, 0, len(writer.requests))
			for _, req := range writer.requests {
				out = append(out, idempotencyRow{
					identity: req.ScopeID + ":" + req.GenerationID,
					contents: fmt.Sprintf("keys=%v;related=%v;source=%s;cause=%s",
						req.EntityKeys, req.RelatedScopeIDs, req.SourceSystem, req.Cause),
				})
			}
			return out
		},
	}
}

func codeCallReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainCodeCallMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingCodeCallIntentWriter{}
			handler := CodeCallMaterializationHandler{
				FactLoader:   &stubFactLoader{envelopes: fencedFacts(codeCallReplayFacts())},
				IntentWriter: writer,
			}
			if _, err := handler.Handle(drainContext(), replayIntent(DomainCodeCallMaterialization, "scope-cc", nil)); err != nil {
				t.Fatalf("code call Handle: %v", err)
			}
			return sharedIntentRows(writer.rows)
		},
	}
}

func sqlRelationshipReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainSQLRelationshipMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingSQLRelationshipIntentWriter{}
			handler := SQLRelationshipMaterializationHandler{
				FactLoader:   &stubFactLoader{envelopes: fencedFacts(sqlRelationshipEntityFacts())},
				IntentWriter: writer,
			}
			if _, err := handler.Handle(drainContext(), replayIntent(DomainSQLRelationshipMaterialization, "scope-db", nil)); err != nil {
				t.Fatalf("sql relationship Handle: %v", err)
			}
			return sharedIntentRows(writer.rows)
		},
	}
}

func shellExecReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainShellExecMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingSQLRelationshipIntentWriter{}
			handler := ShellExecMaterializationHandler{
				FactLoader:   &stubFactLoader{envelopes: fencedFacts(shellExecReplayFacts())},
				IntentWriter: writer,
			}
			if _, err := handler.Handle(drainContext(), replayIntent(DomainShellExecMaterialization, "scope-db", nil)); err != nil {
				t.Fatalf("shell exec Handle: %v", err)
			}
			return sharedIntentRows(writer.rows)
		},
	}
}

func inheritanceReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainInheritanceMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingInheritanceIntentWriter{}
			handler := InheritanceMaterializationHandler{
				FactLoader:   &stubFactLoader{envelopes: fencedFacts(inheritanceEntityFacts())},
				IntentWriter: writer,
			}
			if _, err := handler.Handle(drainContext(), replayIntent(DomainInheritanceMaterialization, "scope-1", []string{"repo-1"})); err != nil {
				t.Fatalf("inheritance Handle: %v", err)
			}
			return sharedIntentRows(writer.rows)
		},
	}
}

func rationaleReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainRationaleMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingRationaleIntentWriter{}
			handler := RationaleEdgeMaterializationHandler{
				FactLoader:   &stubFactLoader{envelopes: fencedFacts(rationaleDeltaEntityFacts())},
				IntentWriter: writer,
			}
			intent := replayIntent(DomainRationaleMaterialization, "scope-code", nil)
			intent.GenerationID = "gen-2"
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("rationale Handle: %v", err)
			}
			return sharedIntentRows(writer.rows)
		},
	}
}

func semanticEntityReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainSemanticEntityMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingSemanticEntityWriter{result: SemanticEntityWriteResult{CanonicalWrites: 1}}
			handler := SemanticEntityMaterializationHandler{
				FactLoader: &stubFactLoader{envelopes: fencedFacts(semanticEntityReplayFacts())},
				Writer:     writer,
			}
			if _, err := handler.Handle(drainContext(), replayIntent(DomainSemanticEntityMaterialization, "scope-se", nil)); err != nil {
				t.Fatalf("semantic entity Handle: %v", err)
			}
			var out []idempotencyRow
			for _, write := range writer.writes {
				for _, row := range write.Rows {
					// Semantic nodes MERGE on (repo, entity id, type); replaying the
					// same parser facts re-emits the identical node identity.
					out = append(out, idempotencyRow{
						identity: row.RepoID + ":" + row.EntityID + ":" + row.EntityType,
						contents: fmt.Sprintf("name=%s;path=%s;lang=%s;start=%d;end=%d",
							row.EntityName, row.RelativePath, row.Language, row.StartLine, row.EndLine),
					})
				}
			}
			return out
		},
	}
}

func documentationReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainDocumentationMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingDocumentationEdgeWriter{}
			handler := DocumentationEdgeMaterializationHandler{
				FactLoader: &stubFactLoader{envelopes: fencedFacts(documentationDeltaFacts())},
				EdgeWriter: writer,
			}
			intent := replayIntent(DomainDocumentationMaterialization, "scope-doc", nil)
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("documentation Handle: %v", err)
			}
			return sharedIntentRows(writer.writeRows)
		},
	}
}

func codeownersOwnershipReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainCodeownersOwnership,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingCodeownersOwnershipEdgeWriter{}
			handler := CodeownersOwnershipEdgeMaterializationHandler{
				FactLoader: &stubFactLoader{envelopes: fencedFacts(codeownersOwnershipReplayFacts())},
				EdgeWriter: writer,
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
					return true, nil
				},
			}
			intent := replayIntent(DomainCodeownersOwnership, "scope-codeowners", nil)
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("codeowners ownership Handle: %v", err)
			}
			return sharedIntentRows(writer.writeRows)
		},
	}
}

func codeownersOwnershipReplayFacts() []facts.Envelope {
	return []facts.Envelope{
		codeownersOwnershipEnvelope("repo-co", "CODEOWNERS", "*.go", []string{"@org/backend"}, 0),
	}
}

func submodulePinReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainSubmodulePin,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &recordingSubmodulePinEdgeWriter{}
			handler := SubmodulePinEdgeMaterializationHandler{
				FactLoader: &stubFactLoader{envelopes: fencedFacts(submodulePinReplayFacts())},
				EdgeWriter: writer,
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
					return true, nil
				},
			}
			intent := replayIntent(DomainSubmodulePin, "scope-submodule", nil)
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("submodule pin Handle: %v", err)
			}
			return sharedIntentRows(writer.writeRows)
		},
	}
}

func submodulePinReplayFacts() []facts.Envelope {
	return []facts.Envelope{
		submodulePinEnvelope("repo-parent", "vendor/lib-a", strPtr("repo-lib-a"), strPtr("abc123")),
	}
}

func platformInfraReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainPlatformInfraMaterialization,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			executor := &recordingCypherExecutor{}
			handler := PlatformInfraMaterializationHandler{
				FactLoader:                 &stubFactLoader{envelopes: fencedFacts(platformInfraTerraformEnvelopes())},
				InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(executor),
			}
			intent := replayIntent(DomainPlatformInfraMaterialization, "scope-infra-eks", nil)
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("platform infra Handle: %v", err)
			}
			var out []idempotencyRow
			for _, call := range executor.calls {
				rows, ok := call.params["rows"].([]map[string]any)
				if !ok {
					continue
				}
				for _, row := range rows {
					// PROVISIONS_PLATFORM rows MERGE on (repo_id, platform_kind);
					// replaying the same Terraform facts re-emits the identical edge.
					out = append(out, idempotencyRow{
						identity: fmt.Sprintf("%v:%v", row["repo_id"], row["platform_kind"]),
						contents: stableRowContents(row),
					})
				}
			}
			return out
		},
	}
}
