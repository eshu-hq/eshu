// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// filterTerraformConfigStateDriftEvidence redacts the scope_id on any
// Evidence atom whose scope_id is outside a scoped caller's grant (#5442 P3).
//
// An exact finding's Evidence[] carries per-atom provenance for the address,
// drift-kind, config, state, and (for removed_from_state) prior atoms
// (tfconfigstate.buildOneCandidate). The address/drift-kind/state/prior atoms
// all use the finding's own granted state_snapshot scope, but the config atom
// uses tfstatebackend.CommitAnchor.ScopeID -- the CONFIG repo's own
// repo-snapshot scope, a different identifier the caller's state_snapshot
// grant never covers. That scope_id previously reached a scoped caller
// verbatim: terraformConfigStateDriftRowFromPostgres copies row.Evidence into
// the response with no grant gate, and filterTerraformConfigStateDriftAmbiguousOwnerCandidates
// only touches AmbiguousOwnerCandidates.
//
// Unlike the ambiguous-owner-candidates filter, this redacts rather than
// drops: the config atom's non-identifying fields (its address value) are
// data the caller legitimately needs to see the drift, so only the ungranted
// scope_id key is removed from the atom's map, mirroring
// blankDeploymentEvidenceEndpointIdentity's delete-the-key convention
// (repository_deployment_evidence.go) over writing an empty-string sentinel
// that could be mistaken for missing data. An unscoped (admin) caller is
// unaffected and always sees every atom's real scope_id.
func filterTerraformConfigStateDriftEvidence(
	findings []TerraformConfigStateDriftFindingRow,
	access repositoryAccessFilter,
) []TerraformConfigStateDriftFindingRow {
	if !access.scoped() || len(findings) == 0 {
		return findings
	}
	for i := range findings {
		if len(findings[i].Evidence) == 0 {
			continue
		}
		redacted := make([]map[string]any, len(findings[i].Evidence))
		for j, atom := range findings[i].Evidence {
			redacted[j] = redactTerraformConfigStateDriftEvidenceAtom(atom, access)
		}
		findings[i].Evidence = redacted
	}
	return findings
}

// redactTerraformConfigStateDriftEvidenceAtom removes scope_id from one
// evidence atom map when its value is a non-empty identifier outside the
// caller's grant. An empty scope_id is left as-is (nothing to leak); an
// in-grant scope_id is left visible. The atom's other fields (id,
// source_system, evidence_type, key, value, confidence) are never touched --
// only the identifier that names an ungranted repository/scope is withheld.
func redactTerraformConfigStateDriftEvidenceAtom(atom map[string]any, access repositoryAccessFilter) map[string]any {
	scopeID := StringVal(atom, "scope_id")
	if scopeID == "" || access.allowsRepositoryID(scopeID) {
		return atom
	}
	clone := make(map[string]any, len(atom))
	for k, v := range atom {
		if k == "scope_id" {
			continue
		}
		clone[k] = v
	}
	return clone
}
