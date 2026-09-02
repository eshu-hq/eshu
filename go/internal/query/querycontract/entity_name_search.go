// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
)

// EntityNameSearchMaxLimit bounds a global entity-name search's page size.
const EntityNameSearchMaxLimit = 200

// EntityNameSearchProbeLimit is EntityNameSearchMaxLimit plus one, requested
// from the store so a full page can be distinguished from a truncated one.
const EntityNameSearchProbeLimit = EntityNameSearchMaxLimit + 1

// EntityNameMatch controls the case-sensitive entity_name predicate.
type EntityNameMatch string

const (
	// EntityNameMatchExact requires a case-sensitive complete name match.
	EntityNameMatchExact EntityNameMatch = "exact"
	// EntityNameMatchSubstring requires a case-sensitive substring match.
	EntityNameMatchSubstring EntityNameMatch = "substring"
)

// EntityNameScope controls repository authorization for an entity-name search.
type EntityNameScope string

const (
	// EntityNameScopeAll searches every repository visible to an all-scopes caller.
	EntityNameScopeAll EntityNameScope = "all"
	// EntityNameScopeRepositories searches one explicit authorized repository set.
	EntityNameScopeRepositories EntityNameScope = "repositories"
)

// EntityNameSearch is the bounded, authorization-aware content name-search contract.
type EntityNameSearch struct {
	Name          string
	Match         EntityNameMatch
	Scope         EntityNameScope
	RepositoryIDs []string
	Languages     []string
	EntityType    string
	MetadataKey   string
	MetadataValue string
	Limit         int
}

// EntityNameSearcher is the narrow extension used by global entity-name routes.
//
// This type, EntityNameSearch, and the two sentinel errors below moved here
// from root package query's content_reader_entity_names.go (#6060) so the
// planned CodeHandler family can assert *ContentReader against this interface
// and share the identical sentinel error values with root's EntityHandler,
// without either package importing the other -- root cannot
// import a family package's own root-facing aliases back, and a family
// package cannot import root without an import cycle. ContentReader's
// implementation of this interface, and the Postgres query it runs, stay in
// root's content_reader_entity_names.go; only the request/response contract
// shape and the shared errors live here.
type EntityNameSearcher interface {
	SearchEntityNames(context.Context, EntityNameSearch) ([]EntityContent, error)
}

// ErrEntityNameSearchUnavailable reports that the content store passed to a
// handler does not implement EntityNameSearcher, so global entity-name search
// has no content-index backend to run against. Root's EntityHandler and the
// CodeHandler family both compare returned errors against this exact value
// with errors.Is, so it must stay a single shared instance.
var ErrEntityNameSearchUnavailable = errors.New("global entity-name content index is unavailable")

// ErrGlobalGraphEntitySearchUnsupported reports that a global (repository_ids
// unset) graph entity search was requested but the running profile has no
// graph-backed fallback registered for it.
var ErrGlobalGraphEntitySearchUnsupported = errors.New("global graph entity search is unsupported")
