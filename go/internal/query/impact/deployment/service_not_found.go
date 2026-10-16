// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"net/http"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// service_not_found.go is the shared #7767 Gap 3 typed refusal for the
// release-name 404 both deployment-trace routes answer when no workload
// context resolves (impact/trace_deployment.go and
// impact/deployment_config_influence.go). One helper serves both branches so
// the two responses cannot drift apart.
//
// An envelope client gets error code service_not_found with
// details.references: the granted content entities exactly named by the
// request, projected to repo_id/relative_path/entity_type/entity_name file
// handles (sorted, capped at serviceNotFoundReferenceCap with a truncated
// flag, mirroring the 409 scope_ids precedent). A plain client gets the
// historical bare 404 byte-for-byte: WriteErrorEnvelope's plain fallback
// renders only the message, which stays "service not found".
//
// The references are a same-name hint, never workload truth: they come only
// from SearchEntitiesByExactNameAnyRepo over the ArgoCDApplication,
// ArgoCDApplicationSet, and HelmChart entity types (the release records a
// name-lookup admit would consult), grant-filtered in Go with
// ImpactRepoIDAllowed because that reader carries no grant predicate. An
// empty grant probes nothing and a reader failure fails its type closed to
// no rows, so an ungranted name renders byte-identical to an unknown one
// and no cross-tenant string ever reaches the body.

// serviceNotFoundMessage is the historical 404 message both routes answered
// before the typed refusal. It stays fixed so plain clients observe no change.
const serviceNotFoundMessage = "service not found"

// serviceNotFoundReferenceCap bounds details.references. The probe reads cap+1
// rows per entity type; more granted rows than the cap sets truncated.
const serviceNotFoundReferenceCap = 20

// serviceNotFoundReferenceTypes are the only content entity types that may
// appear in details.references.
var serviceNotFoundReferenceTypes = []string{"ArgoCDApplication", "ArgoCDApplicationSet", "HelmChart"}

// WriteServiceNotFound answers a nil deployment-trace workload context with
// the typed service_not_found refusal. It is the single 404 writer for both
// deployment-trace routes; callers pass their own capability and the request
// service name (or workload id selector, used only as a name hint).
//
// Exported because the impact package calls it from outside this package.
// See #6060.
func WriteServiceNotFound(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
	serviceName string,
	content querycontract.ContentStore,
) {
	querycontract.WriteErrorEnvelope(w, r, http.StatusNotFound, &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodeServiceNotFound,
		Message:    serviceNotFoundMessage,
		Capability: capability,
		Details:    serviceNotFoundDetails(r, serviceName, content),
	})
}

// serviceNotFoundDetails builds the envelope-only details. Both keys are
// always present so the zero-evidence shape (no granted references) is
// deterministic: an ungranted name and an unknown name serialize identically.
func serviceNotFoundDetails(
	r *http.Request,
	serviceName string,
	content querycontract.ContentStore,
) map[string]any {
	references, truncated := collectServiceNotFoundReferences(r, serviceName, content)
	return map[string]any{"references": references, "truncated": truncated}
}

// collectServiceNotFoundReferences returns the granted same-name deployment
// records as sorted file handles, capped, with whether granted rows beyond
// the cap exist. It returns the zero-evidence shape without probing when the
// client cannot see details (plain), holds no grant (empty), names nothing,
// or wires no content store; a reader failure fails the affected type closed
// to no rows rather than failing the 404.
func collectServiceNotFoundReferences(
	r *http.Request,
	serviceName string,
	content querycontract.ContentStore,
) ([]map[string]any, bool) {
	empty := []map[string]any{}
	if r == nil || content == nil || serviceName == "" {
		return empty, false
	}
	if !querycontract.AcceptsEnvelope(r) {
		return empty, false
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		return empty, false
	}
	admitted := make(map[string]struct{}, len(serviceNotFoundReferenceTypes))
	for _, entityType := range serviceNotFoundReferenceTypes {
		admitted[entityType] = struct{}{}
	}
	var granted []querycontract.EntityContent
	seen := map[string]struct{}{}
	for _, entityType := range serviceNotFoundReferenceTypes {
		rows, err := content.SearchEntitiesByExactNameAnyRepo(r.Context(), entityType, serviceName, serviceNotFoundReferenceCap+1)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if row.EntityName != serviceName {
				continue
			}
			if _, ok := admitted[row.EntityType]; !ok {
				continue
			}
			if !ImpactRepoIDAllowed(row.RepoID, access) {
				continue
			}
			key := row.RepoID + "\x00" + row.RelativePath + "\x00" + row.EntityType + "\x00" + row.EntityName
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			granted = append(granted, row)
		}
	}
	sort.Slice(granted, func(i, j int) bool {
		if granted[i].RepoID != granted[j].RepoID {
			return granted[i].RepoID < granted[j].RepoID
		}
		if granted[i].RelativePath != granted[j].RelativePath {
			return granted[i].RelativePath < granted[j].RelativePath
		}
		if granted[i].EntityType != granted[j].EntityType {
			return granted[i].EntityType < granted[j].EntityType
		}
		return granted[i].EntityName < granted[j].EntityName
	})
	truncated := len(granted) > serviceNotFoundReferenceCap
	references := make([]map[string]any, 0, serviceNotFoundReferenceCap)
	for _, row := range granted {
		if len(references) == serviceNotFoundReferenceCap {
			break
		}
		references = append(references, map[string]any{
			"repo_id":       row.RepoID,
			"relative_path": row.RelativePath,
			"entity_type":   row.EntityType,
			"entity_name":   row.EntityName,
		})
	}
	return references, truncated
}
