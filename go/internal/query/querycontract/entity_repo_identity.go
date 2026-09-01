package querycontract

import "strings"

// ClearResolvedEntityRepoProjectionPlaceholders blanks repo_id/repo_name when
// the graph backend returned the literal text of the projection expression
// instead of the property value.
//
// A second-hop node property reached through OPTIONAL MATCH can come back as
// its own projection expression -- repo_id arriving as the string "r.id"
// rather than a repository id (#6408). A caller that trusts it hands a
// fabricated repository id to whatever consumes the entity.
//
// This lives in the contract leaf, rather than being copied into each family
// package, because it guards an OPEN bug. The four shapes below are
// reconstructed by hand from the queries that build that second hop, so they
// have a pending edit attached: when the backend is fixed, or when a query
// grows a fifth shape, exactly one place should change. Two copies would
// silently keep the stale enumeration in one of them, and nothing would fail.
//
// The query that produces these rows is NOT here. A complete statement with
// its own MATCH/RETURN belongs in a query-owning package (see AGENTS.md);
// hydrateResolvedEntityRepoIdentity in the root query package owns it and
// calls this to scrub what comes back.
func ClearResolvedEntityRepoProjectionPlaceholders(entity map[string]any) {
	if resolvedEntityRepoProjectionPlaceholder(entityRepoIdentityString(entity, "repo_id"), "id") {
		entity["repo_id"] = ""
	}
	if resolvedEntityRepoProjectionPlaceholder(entityRepoIdentityString(entity, "repo_name"), "name") {
		entity["repo_name"] = ""
	}
}

// resolvedEntityRepoProjectionPlaceholder reports whether value is one of the
// projection expressions the affected queries emit for property.
func resolvedEntityRepoProjectionPlaceholder(value string, property string) bool {
	value = strings.TrimSpace(value)
	property = strings.TrimSpace(property)
	switch value {
	case "r." + property,
		"repo." + property,
		"repoViaInstance." + property,
		"coalesce(repo." + property + ", repoViaInstance." + property + ")":
		return true
	default:
		return false
	}
}

// entityRepoIdentityString reads key from entity as a trimmed string.
func entityRepoIdentityString(entity map[string]any, key string) string {
	raw, ok := entity[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}
