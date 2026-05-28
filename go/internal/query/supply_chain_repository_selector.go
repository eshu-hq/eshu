package query

import (
	"net/http"
	"slices"
	"strings"
)

func (h *SupplyChainHandler) resolveSupplyChainRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
) (string, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", true
	}
	repoID, err := resolveRepositorySelectorExact(r.Context(), h.Neo4j, h.Content, selector)
	if err != nil {
		status := http.StatusBadRequest
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

func (h *SupplyChainHandler) resolveSupplyChainSecurityAlertRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
) (string, []string, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", nil, true
	}

	if h.Content != nil {
		entries, err := h.Content.MatchRepositories(r.Context(), selector)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return "", nil, false
		}
		matches := resolveRepositoryCatalogMatches(entries, selector)
		switch len(matches) {
		case 0:
		case 1:
			scopes := securityAlertRepositoryScopeIDs(matches[0], securityAlertRepositoryScopesForCatalog(matches[0], entries))
			return matches[0], scopes, true
		default:
			WriteError(w, http.StatusBadRequest, repositorySelectorAmbiguousError{Selector: selector, Matches: matches}.Error())
			return "", nil, false
		}
	}

	if looksCanonicalRepositoryID(selector) {
		return selector, securityAlertRepositoryScopeIDs(selector, nil), true
	}
	repoID, ok := h.resolveSupplyChainRepositorySelector(w, r, selector)
	if !ok {
		return "", nil, false
	}
	return repoID, securityAlertRepositoryScopeIDs(repoID, nil), true
}

func securityAlertRepositoryScopesForCatalog(
	repositoryID string,
	entries []RepositoryCatalogEntry,
) []string {
	out := make([]string, 0, 2)
	for _, entry := range entries {
		if entry.ID != repositoryID {
			continue
		}
		for _, raw := range []string{entry.RepoSlug, entry.RemoteURL} {
			if slug := cleanSecurityAlertGitHubSlug(raw); slug != "" {
				out = append(out, "security-alert:github:"+slug)
			}
		}
	}
	return out
}

func securityAlertRepositoryScopeIDs(repositoryID string, scopeIDs []string) []string {
	out := make([]string, 0, len(scopeIDs)+1)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(repositoryID)
	for _, scopeID := range scopeIDs {
		add(scopeID)
	}
	slices.Sort(out)
	return out
}

func cleanSecurityAlertGitHubSlug(value string) string {
	value = strings.Trim(strings.ToLower(strings.TrimSpace(value)), "/")
	value = strings.TrimPrefix(value, "git@github.com:")
	value = strings.TrimPrefix(value, "ssh://git@github.com/")
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimPrefix(value, "http://github.com/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return ""
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(strings.TrimSuffix(parts[1], ".git"))
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}
