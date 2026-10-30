// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// repositoryCatalogOmittedPayloadKeys are the git repository fact payload keys
// the cross-scope repository catalog read leaves out (issue #9567). They hold
// per-repository bulk data: imports_map (symbol -> file paths) is most of the
// ~94 KB QA payload, and git_refs plus the delta path arrays describe one
// generation's refs and changes. No catalog consumer reads them; the
// storage-side guard test fails if one starts to.
var repositoryCatalogOmittedPayloadKeys = []string{
	"imports_map",
	"git_refs",
	"delta_relative_paths",
	"delta_deleted_relative_paths",
}

// RepositoryCatalogOmittedPayloadKeys returns, in SQL projection order, the
// repository payload keys the cross-scope repository catalog
// (FactStore.ListActiveRepositoryFacts) does not return. The result is a copy
// the caller may modify. A consumer that needs one of these keys must read the
// full repository fact through a different method; the catalog is an identity
// catalog (graph_id, repo_id, name, remote_url and similar small keys), not the
// full fact.
func RepositoryCatalogOmittedPayloadKeys() []string {
	return append([]string(nil), repositoryCatalogOmittedPayloadKeys...)
}

// ProjectRepositoryCatalogPayload returns a shallow copy of a repository fact
// payload without the keys RepositoryCatalogOmittedPayloadKeys names. It is the
// in-memory twin of the catalog's SQL projection: fake loaders and
// differential tests use it to serve exactly the shape the store returns. The
// input map is not modified, and a nil payload stays nil.
func ProjectRepositoryCatalogPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	projected := make(map[string]any, len(payload))
	for key, value := range payload {
		projected[key] = value
	}
	for _, key := range repositoryCatalogOmittedPayloadKeys {
		delete(projected, key)
	}
	return projected
}
