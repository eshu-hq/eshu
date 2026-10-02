// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// dataflowScannedFactEnvelope builds the per-generation marker fact emitted when
// the value-flow gate ran (#2919). It carries no findings and no reducer_domain
// payload — it is a reconciliation signal the projector consumes so the
// value-flow evidence domains project (and retract stale evidence) even when the
// current generation produced no findings. The stable key is repo-scoped so the
// marker is idempotent across re-emission of the same generation.
func dataflowScannedFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reason":  "value-flow gate scanned the repository snapshot",
		"repo_id": repoID,
	}

	return model.FactEnvelope(
		facts.CodeDataflowScannedFactKind,
		scopeID,
		generationID,
		observedAt,
		facts.CodeDataflowScannedFactKind+":"+repoID,
		payload,
		repoPath,
	)
}

// followupEntityKey builds the entity_key every git-collector shared_followup
// fact carries: prefix + repoID. repoID MUST be the repository's canonical ID
// (repositoryidentity.Metadata.ID, `repository:r_<hex>`), never the repository
// fact name or the checkout directory basename. Names are not unique across a
// run and can end in a colon the alias normalizer cannot match, so the
// reducer selects candidates by repository ID (#7384). The ID is also the
// `repo_id` payload field, so the key and the fact can never disagree. All
// follow-up envelopes build the key here so no domain keeps a second
// derivation. Selection equality is effectively on the hex suffix (the
// normalizer compares the last segment), so the CanonicalRepositoryID
// collision bound is a selection-correctness bound.
func followupEntityKey(prefix, repoID string) string {
	return prefix + ":" + repoID
}

func workloadIdentityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_identity",
		"entity_key":     followupEntityKey("workload", repoID),
		"reason":         "repository snapshot emitted shared workload identity follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_identity",
		payload,
		repoPath,
	)
}

func deployableUnitCorrelationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployable_unit_correlation",
		"entity_key":     followupEntityKey("repo", repoID),
		"reason":         "repository snapshot emitted deployable-unit correlation follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployable_unit_correlation",
		payload,
		repoPath,
	)
}

func codeCallMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "code_call_materialization",
		"entity_key":     followupEntityKey("repo", repoID),
		"reason":         "repository snapshot emitted code-call materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":code_call_materialization",
		payload,
		repoPath,
	)
}

// rationaleMaterializationFactEnvelope builds the per-repository reconciliation
// marker for canonical EXPLAINS edges. It is emitted for every admitted full or
// delta generation, including generations with no rationale comments, so the
// reducer can retract stale edges after comments disappear.
func rationaleMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "rationale_materialization",
		"entity_key":     followupEntityKey("rationale", repoID),
		"reason":         "repository generation requested rationale materialization reconciliation",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":rationale_materialization",
		payload,
		repoPath,
	)
}

// platformInfraMaterializationFactEnvelope builds the per-repository follow-up
// fact that triggers the platform_infra_materialization reducer domain. The
// handler extracts Terraform/terragrunt platform-provisioning signals from the
// repository's facts and emits platform_infra shared-projection intents (the
// dedicated home for Repository-[:PROVISIONS_PLATFORM]->Platform). It is emitted
// unconditionally per repository; repos without IaC platform signals reduce to a
// no-op.
func platformInfraMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "platform_infra_materialization",
		"entity_key":     followupEntityKey("repo", repoID),
		"reason":         "repository snapshot emitted platform infra materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":platform_infra_materialization",
		payload,
		repoPath,
	)
}

func workloadMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "workload_materialization",
		"entity_key":     followupEntityKey("workload", repoID),
		"reason":         "repository snapshot emitted workload materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":workload_materialization",
		payload,
		repoPath,
	)
}

func deploymentMappingFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "deployment_mapping",
		"entity_key":     followupEntityKey("deployment", repoID),
		"reason":         "repository snapshot emitted deployment mapping follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":deployment_mapping",
		payload,
		repoPath,
	)
}

func sqlRelationshipMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "sql_relationship_materialization",
		"entity_key":     followupEntityKey("sql", repoID),
		"reason":         "repository snapshot emitted SQL relationship materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":sql_relationship_materialization",
		payload,
		repoPath,
	)
}

func shellExecMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "shell_exec_materialization",
		"entity_key":     followupEntityKey("shell", repoID),
		"reason":         "repository snapshot emitted shell execution materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":shell_exec_materialization",
		payload,
		repoPath,
	)
}

func inheritanceMaterializationFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "inheritance_materialization",
		"entity_key":     followupEntityKey("inheritance", repoID),
		"reason":         "repository snapshot emitted inheritance materialization follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":inheritance_materialization",
		payload,
		repoPath,
	)
}

// codeImportRepoEdgeFactEnvelope builds the per-repository follow-up marker that
// enqueues the code_import_repo_edge reducer domain for one git scope. The
// domain projects repo-to-repo DEPENDS_ON edges from per-file external import
// sources correlated to package-registry ownership (issue #3642). The stable key
// is repo-scoped so re-emitting the same generation is idempotent.
func codeImportRepoEdgeFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "code_import_repo_edge",
		"entity_key":     followupEntityKey("repo", repoID),
		"reason":         "repository snapshot emitted code-import repo-edge follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":code_import_repo_edge",
		payload,
		repoPath,
	)
}

// codeownersOwnershipFactEnvelope builds the per-repository follow-up marker
// that triggers the codeowners_ownership reducer domain (const
// DomainCodeownersOwnership, issue #5419 Phase 3). The handler materializes
// canonical DECLARES_CODEOWNER edges from directly-emitted codeowners.ownership
// facts. It is emitted unconditionally per repository, not gated on CODEOWNERS
// presence: this makes the domain run every generation so its delta-retract
// sweeps stale edges even when a CODEOWNERS file is removed (no
// codeowners.ownership facts survive to re-cycle the retract-then-write).
func codeownersOwnershipFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "codeowners_ownership",
		"entity_key":     followupEntityKey("codeowners", repoID),
		"reason":         "repository snapshot emitted codeowners ownership follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":codeowners_ownership",
		payload,
		repoPath,
	)
}

// submodulePinFactEnvelope builds the per-repository follow-up marker that
// triggers the submodule_pin reducer domain (const DomainSubmodulePin, issue
// #5420 Phase 3). The handler materializes canonical PINS_SUBMODULE edges
// from directly-emitted submodule.pin facts. It is emitted unconditionally
// per repository, not gated on ".gitmodules" presence, mirroring
// codeownersOwnershipFactEnvelope: this makes the domain run every generation
// so its delta-retract sweeps stale edges even when ".gitmodules" is removed
// (no submodule.pin facts survive to re-cycle the retract-then-write).
func submodulePinFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"reducer_domain": "submodule_pin",
		"entity_key":     followupEntityKey("submodule", repoID),
		"reason":         "repository snapshot emitted submodule pin follow-up",
		"repo_id":        repoID,
	}

	return model.FactEnvelope(
		"shared_followup",
		scopeID,
		generationID,
		observedAt,
		"shared_followup:"+repoID+":submodule_pin",
		payload,
		repoPath,
	)
}
