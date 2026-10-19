// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// The uses_service_account family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.SecretsIAMGraphWriter through the WriteUsesServiceAccountEdges
// port. The relationship type is SECRETS_IAM_USES_SERVICE_ACCOUNT, the
// static token baked into the edge template's MERGE. Read it off the
// template, never by deriving from the port or family name.
//
// This Odù carries reducer read-model facts
// (reducer_secrets_iam_identity_trust_chain), NOT collector facts, and that
// is deliberate rather than a shortcut. The production extractor feeding
// this port — secretsiam.ExtractSecretsIAMGraphRows, called by the
// SecretsIAMGraphProjectionHandler — switches on exactly these read-model
// kinds; the guard runs that same function over these same facts, so every
// line the guard executes is production code with no guard-side translation
// layer. Composing the trust-chain analyzer inside the guard instead would
// invent a model-to-envelope translation Postgres performs in production,
// and a guard-side copy of that translation can drift from the writer's own
// payload function. The analyzer half (collector facts to exact vs partial
// chains) stays proven by secretsiam's own build tests, not re-proven here.
//
// The payload keys mirror secretsIAMIdentityTrustChainPayload's
// extractor-read subset (state, confidence, service_account_join_key,
// workload_object_id, scope_id, generation_id, evidence_fact_ids): keys the
// extractor never reads are weight the fixture cannot assert. The fact kinds
// are aliased from the secretsiam package, never copied, so a kind rename
// reds here instead of silently starving the extractor's switch.
//
// Scope note: the read-model facts of one projection generation share one
// scope and generation, so this Odù carries a single scope/generation pair
// exactly as one handler invocation would load.

const (
	// UsesServiceAccountFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:uses_service_account coverage row would name to
	// resolve through it.
	UsesServiceAccountFamilyOduName = "odu:ifa-uses-service-account-family"

	// usesServiceAccountFamilyScopeID is the single scope every fact in this
	// Odù belongs to. The reducer handler loads one scope generation's
	// facts, so a fixture spanning scopes would not mirror any real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges — diagnosed live 2026-09-27 on the shared scope during the
	// iam_can_assume drive. One live generation per scope per cell is the
	// contract.
	usesServiceAccountFamilyScopeID = "reducer:eshu-fixture-uses-service-account"

	// usesServiceAccountFamilyGenerationID is the one scope generation the
	// Odù replays. The reducer's handler loads a single scope generation's
	// facts, so every fact below shares it.
	usesServiceAccountFamilyGenerationID = "gen-ifa-uses-service-account-family-1"

	// usesServiceAccountFamilyConfidence is the confidence every edge
	// producer carries. Exact-only admission is the family contract: the
	// extractor counts any other state as a skip, never an edge.
	usesServiceAccountFamilyConfidence = "exact"
)

// usesServiceAccountFamilyChainFixture describes one identity trust-chain
// read-model fact in the Odù.
type usesServiceAccountFamilyChainFixture struct {
	// ServiceAccountJoinKey is the stable ServiceAccount identity: the
	// edge's target uid.
	ServiceAccountJoinKey string
	// WorkloadObjectID is the KubernetesWorkload uid: the edge's source
	// uid. Empty proves the missing-workload skip.
	WorkloadObjectID string
	// State is the reducer read-model truth state. Only exact chains
	// promote; any other state is counted and never fabricated.
	State string
	// Tombstone marks a chain a later generation retracted: it
	// materializes nothing.
	Tombstone bool
}

// usesServiceAccountFamilyChains are the chains: two exact producers and
// three deliberate non-producers covering the non-exact-state,
// missing-workload, and tombstone branches.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — a partial chain, a chain with no workload id,
// and a tombstoned chain each resolve to nothing — and the writer's MATCH
// clauses would no-op on a missing node anyway. Without them, a regression
// that started emitting an edge for every chain regardless of state would
// still reproduce the expected set exactly and this fixture would report
// green.
var usesServiceAccountFamilyChains = []usesServiceAccountFamilyChainFixture{
	{
		// EDGE: exact chain binding the orders workload to its service
		// account.
		ServiceAccountJoinKey: "sha256:orders-service-account",
		WorkloadObjectID:      "workload-orders-api",
		State:                 "exact",
	},
	{
		// EDGE: exact chain binding the billing workload to its service
		// account — the second edge proving fan-out, not a singleton.
		ServiceAccountJoinKey: "sha256:billing-service-account",
		WorkloadObjectID:      "workload-billing-api",
		State:                 "exact",
	},
	{
		// NO EDGE: partial chain — some source evidence exists but a
		// required hop is missing, so the projection counts the skip and
		// emits nothing.
		ServiceAccountJoinKey: "sha256:stale-service-account",
		WorkloadObjectID:      "workload-stale-api",
		State:                 "partial",
	},
	{
		// NO EDGE: exact chain with no workload id — the ServiceAccount
		// subgraph still projects, but the workload edge is counted as a
		// missing-workload skip rather than fabricated.
		ServiceAccountJoinKey: "sha256:orphan-service-account",
		WorkloadObjectID:      "",
		State:                 "exact",
	},
	{
		// NO EDGE: retracted by a later generation — the tombstone skip.
		ServiceAccountJoinKey: "sha256:retired-service-account",
		WorkloadObjectID:      "workload-retired-api",
		State:                 "exact",
		Tombstone:             true,
	},
}

// usesServiceAccountFamilyStableFactKey derives one fact's durable dedup
// key from the chain identity, in a readable fixture-local format rather
// than a hex digest, instead of hand-typing a string the expected-edge
// fixture cannot independently check.
func usesServiceAccountFamilyStableFactKey(serviceAccountJoinKey string) string {
	return fmt.Sprintf(
		"%s:%s",
		secretsiam.SecretsIAMIdentityTrustChainFactKind, serviceAccountJoinKey,
	)
}

// UsesServiceAccountFamilyOdu builds the cataloged Odù for the
// uses_service_account direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name.
func UsesServiceAccountFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(usesServiceAccountFamilyChains))
	for _, fixture := range usesServiceAccountFamilyChains {
		payload := map[string]any{
			"state":                    fixture.State,
			"confidence":               usesServiceAccountFamilyConfidence,
			"service_account_join_key": fixture.ServiceAccountJoinKey,
			"workload_object_id":       fixture.WorkloadObjectID,
			"scope_id":                 usesServiceAccountFamilyScopeID,
			"generation_id":            usesServiceAccountFamilyGenerationID,
			"evidence_fact_ids":        []string{"fixture:" + fixture.ServiceAccountJoinKey},
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          usesServiceAccountFamilyScopeID,
			GenerationID:     usesServiceAccountFamilyGenerationID,
			FactKind:         secretsiam.SecretsIAMIdentityTrustChainFactKind,
			StableFactKey:    usesServiceAccountFamilyStableFactKey(fixture.ServiceAccountJoinKey),
			CollectorKind:    "reducer",
			SourceConfidence: facts.SourceConfidenceInferred,
			Payload:          payload,
			IsTombstone:      fixture.Tombstone,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: UsesServiceAccountFamilyOduName, Facts: factsForOdu},
		Detail: "five reducer read-model facts for the direct-materialization uses_service_account family: two exact identity chains binding distinct workloads to distinct service accounts and three deliberate non-producers covering the non-exact-state, missing-workload, and tombstone branches, so the SECRETS_IAM_USES_SERVICE_ACCOUNT expected set proves exact-only admission and restraint",
	}
}
