// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
)

// applyRepositorySelectorForCapability resolves *selector, and on failure
// writes the response and reports false. Selector resolution issues its own
// graph read, so a Neo4jReader timeout or outage is mapped to the shared
// 503/504 bounded graph-read contract using capability rather than falling
// through to the generic 400 branch, which would tell the client its request
// was malformed during a purely transient backend condition.
func (h *CodeHandler) applyRepositorySelectorForCapability(w http.ResponseWriter, r *http.Request, selector *string, capability string) bool {
	return applyRepositorySelectorForAccess(w, r, h.Neo4j, h.Content, selector, capability)
}

// applyRepositorySelectorForAccess is the handler-independent half of the
// resolution above, for the code-family routes whose handler type is not
// CodeHandler.
//
// POST /api/v0/code/language-query is owned by LanguageQueryHandler, so none of
// the CodeHandler selector methods were reachable from it and req.RepoID was
// used raw: never resolved through queryselector, never checked against the
// caller's grant, and an ungranted repository id was pushed into the query
// instead of being refused. Extracting the body here rather than copying it
// onto a second handler keeps one implementation of "resolve the selector, map
// a transient graph failure to the bounded-read contract, and reject anything
// else with 400" for every route in the family.
func applyRepositorySelectorForAccess(
	w http.ResponseWriter,
	r *http.Request,
	graph GraphQuery,
	content ContentStore,
	selector *string,
	capability string,
) bool {
	if selector == nil {
		return true
	}
	resolved, err := resolveRepositorySelectorExactForAccess(
		r.Context(),
		graph,
		content,
		*selector,
		codeGrantAccessFilter(r.Context()),
	)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return false
		}
		WriteError(w, http.StatusBadRequest, err.Error())
		return false
	}
	*selector = resolved
	return true
}

func (h *CodeHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExactForAccess(
		ctx,
		h.Neo4j,
		h.Content,
		selector,
		codeGrantAccessFilter(ctx),
	)
}

// codeGrantAccessFilter is the repository grant every read in the code family
// binds, with each granted git repository ingestion scope also read back as the
// canonical repository id it owns
// (querycontract.RepositoryAccessFilter.WithCanonicalScopeRepositories).
//
// Without that step a token whose grant is a scope id
// ("git-repository-scope:repository:r_payments") and no repository id pushes
// the scope id into `repo_id = ANY($n)` and into
// `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids`, where the
// stored identity is the canonical `repository:r_payments`. Nothing matches and
// the caller reads an empty page from a repository they were granted -- the
// same scope-versus-canonical mismatch #5052 fixed in keyword search
// (docs/internal/evidence/5052-keyword-search-scope-id.md).
//
// The resolution is additive, so the fail-closed cases are untouched: a grant
// that resolves to no repository still reads nothing, and a caller with no
// grants at all is still Empty.
func codeGrantAccessFilter(ctx context.Context) repositoryAccessFilter {
	return repositoryAccessFilterFromContext(ctx).WithCanonicalScopeRepositories()
}

// codeContentGrantScope resolves the caller's repository grant for a code read
// that is optionally anchored to one repository by repoID.
//
// It exists because applyRepositorySelectorForCapability only binds a grant to
// a selector the caller actually supplied: queryselector.ResolveExactForAccess
// returns "" for an empty selector without consulting the grant at all
// (queryselector/selector.go), so every code route that treats an omitted
// repo_id as "search everything" ran its downstream query with no grant bound.
//
// allowed is the granted repository id list to push into the read's own
// predicate for a corpus-wide search -- nil when the caller is unscoped (the
// shared/admin/local read stays unrestricted) or when the read is already
// anchored to a single granted repository. blocked reports that the caller's
// grant admits nothing, so the route must return its empty page WITHOUT
// touching the store.
//
// The two backends fail in opposite directions on an empty grant, and only one
// of them fails open:
//
//   - SQL. appendRepositoryGrantFilter and the other content builders OMIT the
//     `repo_id = ANY($n)` predicate when the id list is empty, so a grantless
//     scoped caller's statement carries no repository restriction at all and
//     reads the whole corpus. It is the builder's omission that fails open, not
//     the predicate's semantics: `repo_id = ANY('{}')` is false for every row.
//     This is the fail-open blocked exists to close.
//   - Cypher. querycontract.RepositoryAccessFilter.GraphPredicate and
//     GraphCondition gate on Scoped() alone and never on emptiness, so the same
//     caller renders `(repo.id IN $allowed_repository_ids OR repo.id IN
//     $allowed_scope_ids)` against two empty arrays, which matches nothing. The
//     graph builders already fail closed on their own.
//
// blocked stays in front of both. On the graph half it is defense in depth that
// still earns its keep: the request never reaches the backend, and the answer a
// grantless caller gets is the route's own empty page, identical to the answer
// for an empty index, so index existence cannot be probed.
func codeContentGrantScope(ctx context.Context, repoID string) (allowed []string, blocked bool) {
	access := codeGrantAccessFilter(ctx)
	if access.Empty() {
		return nil, true
	}
	if !access.Scoped() {
		return nil, false
	}
	if repoID = strings.TrimSpace(repoID); repoID != "" {
		// Defense in depth: the selector already resolved repoID through the
		// grant, so a mismatch here means a caller reached the read on a
		// selector-free path.
		return nil, !access.AllowsRepositoryID(repoID)
	}
	return access.RepositorySearchIDs(), false
}

// languageQueryGrant is the caller's repository grant, resolved once per
// request and threaded through every read the four dispatch branches make.
//
// The two fields are the same grant expressed for the two backends: access
// renders the Cypher condition and binds $allowed_repository_ids /
// $allowed_scope_ids, and allowedRepositoryIDs is the id list the SQL builder
// binds to `repo_id = ANY($n)`. Both are empty for an unscoped caller, and a
// grantless scoped caller never reaches a read at all -- languageQueryGrantFor
// reports that case as blocked.
type languageQueryGrant struct {
	access               repositoryAccessFilter
	allowedRepositoryIDs []string
}

// languageQueryGrantFor resolves the caller's grant for a read optionally
// anchored to one repository by repoID. blocked reports that the grant admits
// nothing, so the route must answer its own empty page without touching a
// backend.
func languageQueryGrantFor(ctx context.Context, repoID string) (grant languageQueryGrant, blocked bool) {
	allowed, blocked := codeContentGrantScope(ctx, repoID)
	if blocked {
		return languageQueryGrant{}, true
	}
	return languageQueryGrant{
		access:               codeGrantAccessFilter(ctx),
		allowedRepositoryIDs: allowed,
	}, false
}
