// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// Runtime-evidence read-model types live in
// internal/query/supplychain/impact (#6060 lane A); the aliases in
// supply_chain_impact_alias.go keep this probe compiling unchanged until
// the hub PR3 moves the probes.

const supplyChainRuntimeContextTruthBasis = "read_time_resolved"

// supplyChainImpactRuntimeContextReader is the optional store capability the
// findings handler type-asserts on its ImpactFindings store to resolve
// per-repository runtime context at read time. A store that does not
// implement it (legacy, test double) leaves rows untouched — the response
// degrades to the pre-#5746 shape rather than erroring. The final two slices
// carry scoped-token repository and ingestion-scope grants; both empty means
// unrestricted, matching the runtime-filter SQL contract.
type supplyChainImpactRuntimeContextReader interface {
	ListSupplyChainImpactRuntimeContext(
		context.Context,
		[]string,
		[]string,
		[]string,
	) (map[string]SupplyChainRuntimeContext, error)
}

// SupplyChainRuntimeEnvironmentCandidate moved to
// internal/query/supplychain/impact with the other runtime-evidence
// read-model types (#6060 lane A); see supply_chain_impact_alias.go.

// SupplyChainRuntimeEnvironmentEvidenceProbe moved to
// internal/query/supplychain/impact with the other runtime-evidence
// read-model types (#6060 lane A); see supply_chain_impact_alias.go.

type supplyChainImpactRuntimeEnvironmentReader interface {
	ListSupplyChainImpactRuntimeEnvironmentEvidence(
		context.Context,
		[]SupplyChainRuntimeEnvironmentCandidate,
		[]string,
		[]string,
	) (map[string]map[string]string, error)
}

const maxSupplyChainRuntimeEnvironmentCandidates = supplyChainImpactFindingMaxLimit

type supplyChainRuntimeEnvironmentPlan struct {
	candidates []SupplyChainRuntimeEnvironmentCandidate
	metadata   *SupplyChainRuntimeEnvironmentEvidenceProbe
}

// applySupplyChainRuntimeContext resolves each finding row's runtime context
// (workloads, services, deployments, environments, catalog refs) from its
// repository_id at QUERY time and attaches it to the row as
// RuntimeContext — read-time resolution instead of reduce-time baking
// (issue #5746; reduce-time baking was disproven because the workload facts
// commit seconds after the finding and nothing re-triggers it).
//
// Rows with no repository anchor are skipped untouched. Rows whose repository
// has no runtime facts yet (fresh ingest) get an honest empty, labeled
// context — absence is "current state of knowledge", not an error, and it
// self-heals on the next read. The probe NEVER writes the resolved IDs into
// the baked WorkloadIDs/ServiceIDs/Environments fields; #5747 makes filters
// consult current active runtime facts directly instead.
//
// A reader error is propagated (the caller maps graph sentinels to the
// bounded retryable envelope and everything else to a plain 500) rather than
// serving an empty context that could be misread as "nothing runs this",
// matching the cloud-runtime probe's fail-loud contract. Scoped access is
// forwarded to the reader so response hydration and runtime filters cannot
// disagree about which current facts the caller may observe.
func (h *SupplyChainHandler) applySupplyChainRuntimeContext(
	ctx context.Context,
	rows []impact.SupplyChainImpactFindingRow,
	access repositoryAccessFilter,
) error {
	if h == nil || len(rows) == 0 {
		return nil
	}
	reader, ok := h.ImpactFindings.(supplyChainImpactRuntimeContextReader)
	if !ok || reader == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	repositoryIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		repositoryID := strings.TrimSpace(row.RepositoryID)
		if repositoryID == "" {
			continue
		}
		if _, dup := seen[repositoryID]; dup {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	if len(repositoryIDs) == 0 {
		return nil
	}
	var allowedRepositoryIDs, allowedScopeIDs []string
	if access.Scoped() {
		allowedRepositoryIDs = access.AllowedRepositoryIDs
		allowedScopeIDs = access.AllowedScopeIDs
	}
	byRepo, err := reader.ListSupplyChainImpactRuntimeContext(
		ctx,
		repositoryIDs,
		allowedRepositoryIDs,
		allowedScopeIDs,
	)
	if err != nil {
		return err
	}
	byDigest := map[string]map[string]string{}
	environmentPlans := make([]supplyChainRuntimeEnvironmentPlan, len(rows))
	if environmentReader, ok := h.ImpactFindings.(supplyChainImpactRuntimeEnvironmentReader); ok && environmentReader != nil {
		candidates, plans := planSupplyChainRuntimeEnvironmentCandidates(rows, byRepo)
		environmentPlans = plans
		if len(candidates) > 0 {
			byDigest, err = environmentReader.ListSupplyChainImpactRuntimeEnvironmentEvidence(
				ctx,
				candidates,
				allowedRepositoryIDs,
				allowedScopeIDs,
			)
			if err != nil {
				return err
			}
		}
	}
	for i := range rows {
		repositoryID := strings.TrimSpace(rows[i].RepositoryID)
		if repositoryID == "" {
			continue
		}
		resolved := byRepo[repositoryID]
		digestEvidence := supplyChainRuntimeEnvironmentEvidenceForPlan(
			environmentPlans[i],
			byDigest,
		)
		environments := sortedUniqueNonEmptyStrings(append(
			append([]string(nil), resolved.Environments...),
			mapStringKeys(digestEvidence)...,
		))
		rows[i].RuntimeContext = &SupplyChainRuntimeContextResult{
			TruthBasis:               supplyChainRuntimeContextTruthBasis,
			WorkloadIDs:              sortedUniqueNonEmptyStrings(resolved.WorkloadIDs),
			ServiceIDs:               sortedUniqueNonEmptyStrings(resolved.ServiceIDs),
			DeploymentIDs:            sortedUniqueNonEmptyStrings(resolved.DeploymentIDs),
			Environments:             environments,
			EnvironmentEvidence:      digestEvidence,
			EnvironmentEvidenceProbe: environmentPlans[i].metadata,
			CatalogEntityRefs:        sortedUniqueNonEmptyStrings(resolved.CatalogEntityRefs),
			CatalogOwnerRefs:         sortedUniqueNonEmptyStrings(resolved.CatalogOwnerRefs),
		}
	}
	return nil
}

func supplyChainRuntimeEnvironmentEvidenceForPlan(
	plan supplyChainRuntimeEnvironmentPlan,
	byDigest map[string]map[string]string,
) map[string]string {
	var out map[string]string
	for _, candidate := range plan.candidates {
		digest := strings.TrimSpace(candidate.SubjectDigest)
		environment := strings.TrimSpace(candidate.Environment)
		evidence, ok := byDigest[digest][environment]
		if !ok {
			continue
		}
		out = impact.RecordSupplyChainRuntimeEnvironmentEvidence(out, environment, evidence)
	}
	return out
}

func planSupplyChainRuntimeEnvironmentCandidates(
	rows []impact.SupplyChainImpactFindingRow,
	byRepo map[string]SupplyChainRuntimeContext,
) ([]SupplyChainRuntimeEnvironmentCandidate, []supplyChainRuntimeEnvironmentPlan) {
	plans := make([]supplyChainRuntimeEnvironmentPlan, len(rows))
	available := make([][]SupplyChainRuntimeEnvironmentCandidate, len(rows))
	for rowIndex, row := range rows {
		digest := strings.TrimSpace(row.SubjectDigest)
		if digest == "" {
			continue
		}
		repositoryContext := byRepo[strings.TrimSpace(row.RepositoryID)]
		environments := sortedUniqueNonEmptyStrings(append(
			append([]string(nil), row.Environments...),
			repositoryContext.Environments...,
		))
		for _, environment := range environments {
			available[rowIndex] = append(available[rowIndex], SupplyChainRuntimeEnvironmentCandidate{
				SubjectDigest: digest,
				Environment:   environment,
			})
		}
	}
	remaining := maxSupplyChainRuntimeEnvironmentCandidates
	for round := 0; remaining > 0; round++ {
		progress := false
		for rowIndex := range available {
			if round >= len(available[rowIndex]) || remaining == 0 {
				continue
			}
			plans[rowIndex].candidates = append(plans[rowIndex].candidates, available[rowIndex][round])
			remaining--
			progress = true
		}
		if !progress {
			break
		}
	}
	unique := make(map[string]SupplyChainRuntimeEnvironmentCandidate)
	for rowIndex := range plans {
		if len(available[rowIndex]) == 0 {
			continue
		}
		plans[rowIndex].metadata = &SupplyChainRuntimeEnvironmentEvidenceProbe{
			CandidateLimit:      len(plans[rowIndex].candidates),
			CandidatesTruncated: len(plans[rowIndex].candidates) < len(available[rowIndex]),
		}
		for _, candidate := range plans[rowIndex].candidates {
			unique[candidate.SubjectDigest+"\x00"+candidate.Environment] = candidate
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]SupplyChainRuntimeEnvironmentCandidate, 0, len(keys))
	for _, key := range keys {
		out = append(out, unique[key])
	}
	return out, plans
}

func mapStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
