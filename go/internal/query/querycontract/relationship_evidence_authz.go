// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"maps"
	"slices"
	"strings"
)

// relationshipTargetAttributableByVerb is the single source of truth for the
// verb→targetAttributable classification: which typed-edge verbs carry a
// target endpoint with tenant attribution a #5167 scoped-token grant can
// bind, and which target a shared/global entity with no repo_id to protect
// (Module for IMPORTS, Platform for RUNS_ON, SqlTable for QUERIES_TABLE,
// CloudAction for INVOKES_CLOUD_ACTION, Directory/workflow nodes for the
// Atlantis-governance verbs, ContainerImage for
// AWS_lambda_function_uses_image).
//
// It was promoted out of the relationships catalog for #6642 (evidence-leaf
// pre-slice): the catalog's relationshipVerbEntry.targetAttributable field is
// deleted, and both readers -- the relationships/edges scope predicate (which
// binds the target endpoint only for attributable verbs) and the relationship
// evidence drilldown 404-mask (which enforces the target grant only for
// attributable verbs) -- resolve through this table. There is exactly one
// copy: do not re-add a per-entry flag or a second table anywhere else.
//
// The map is keyed by the UPPER-CASED verb, mirroring the catalog's
// relationshipVerbByName. Every lookup site upper-cases the input first, so
// the key MUST stay upper-cased too: keying by the raw entry verb silently
// misclassifies AWS_lambda_function_uses_image -- the only catalog verb whose
// canonical form is not already all-uppercase (#5450 P1-B).
var relationshipTargetAttributableByVerb = map[string]bool{
	"CALLS":                          true,
	"IMPORTS":                        false,
	"INHERITS":                       true,
	"REFERENCES":                     true,
	"OVERRIDES":                      true,
	"QUERIES_TABLE":                  false,
	"DEPLOYS_FROM":                   true,
	"INSTANCE_OF":                    true,
	"RECONCILES_FROM":                true,
	"PROVISIONS_DEPENDENCY_FOR":      true,
	"USES_MODULE":                    true,
	"DISCOVERS_CONFIG_IN":            true,
	"MANAGES":                        false,
	"ATLANTIS_DEPENDS_ON":            false,
	"USES_WORKFLOW":                  false,
	"RUNS_ON":                        false,
	"AWS_LAMBDA_FUNCTION_USES_IMAGE": false,
	"DEPENDS_ON":                     true,
	"INVOKES_CLOUD_ACTION":           false,
	"READS_CONFIG_FROM":              true,
	"TAINT_FLOWS_TO":                 true,
}

// RelationshipVerbs returns the sorted upper-cased verbs keying the
// target-attribution table, one per relationships-catalog verb. It is the
// enumeration behind the catalog/contract verb-set parity test: callers
// compare this set against the catalog's verb set instead of re-listing the
// table. The returned slice is a fresh copy.
func RelationshipVerbs() []string {
	return slices.Sorted(maps.Keys(relationshipTargetAttributableByVerb))
}

// RelationshipVerbTargetAttributable reports whether verb's target endpoint
// carries tenant attribution a scoped grant must gate on (the scope-binding
// direction). The input is trimmed and upper-cased before the lookup, so any
// casing resolves through the same key. A verb outside the catalog reports
// false, mirroring the deleted entry field's zero value; that path is
// unreachable on the edges surface (unknown verbs 400 before any scope
// predicate renders), so the default documents the fallback rather than a
// reachable branch.
func RelationshipVerbTargetAttributable(verb string) bool {
	return relationshipTargetAttributableByVerb[strings.ToUpper(strings.TrimSpace(verb))]
}

// RelationshipEvidenceTargetAttributable reports whether the row's target
// endpoint carries a tenant attribution that a scoped grant must gate on (the
// 404-mask direction). It resolves the row's relationship_type against the
// fixed verb table; a known verb uses its classification. An unknown verb
// (outside the 21-verb catalog) falls back fail-closed to the row's own
// target repo_id: a non-empty target repo_id names a real repository that
// must be protected, an empty one has no tenant secret, so source ownership
// suffices.
func RelationshipEvidenceTargetAttributable(row map[string]any) bool {
	verb := strings.ToUpper(strings.TrimSpace(StringVal(row, "relationship_type")))
	if attributable, ok := relationshipTargetAttributableByVerb[verb]; ok {
		return attributable
	}
	return RelationshipEvidenceEndpointRepoID(row, "target") != ""
}

// RelationshipEvidenceEndpointRepoID reads the repo_id string off the
// "source" or "target" endpoint map of a relationship evidence row. A
// missing, nil, or non-map endpoint reads as "", which never matches a
// grant, so the readers above fail closed on malformed rows.
func RelationshipEvidenceEndpointRepoID(row map[string]any, key string) string {
	endpoint, _ := row[key].(map[string]any)
	if endpoint == nil {
		return ""
	}
	return StringVal(endpoint, "repo_id")
}

// RelationshipEvidenceRowWithinAccess reports whether the resolved
// relationship row is visible to access's granted repositories/ingestion
// scopes. Shared/admin/local callers (access.scoped() == false) always pass;
// a scoped caller with no grants fails closed.
//
// A scoped caller must always own the SOURCE repository (a resolved
// relationship always carries a source repo). The TARGET grant is required
// only when the target endpoint carries a tenant attribution, mirroring the
// relationships/edges scope model: for a target-unattributable verb the
// target is a shared/global entity with no repo_id, so there is no
// cross-tenant secret to protect and source ownership alone authorizes the
// read. For a target-attributable verb the target names a real repository,
// so the target grant is enforced.
func RelationshipEvidenceRowWithinAccess(row map[string]any, access RepositoryAccessFilter) bool {
	if !access.Scoped() {
		return true
	}
	if access.Empty() {
		return false
	}
	if !access.AllowsRepositoryID(RelationshipEvidenceEndpointRepoID(row, "source")) {
		return false
	}
	if !RelationshipEvidenceTargetAttributable(row) {
		return true
	}
	return access.AllowsRepositoryID(RelationshipEvidenceEndpointRepoID(row, "target"))
}
