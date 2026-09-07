// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"strings"
)

// The repository-story and evidence read models live here for the same reason
// as the documentation ones: the shared ContentStore double that answers them
// has to be constructible from outside package query (#6060, epic #6053).
// Package query keeps an unexported alias for each, so its call sites are
// unchanged.
//
// Each of these is answered through a narrow optional port that package query
// type-asserts the ContentStore against. Available is what carries "this store
// could not answer" through a signature that has no room to say so, and a
// handler must check it before reading Rows -- an unavailable read model is
// not the same as one that legitimately found nothing.

// RepositoryEntryPointReadModel carries content-derived repository entry
// points.
type RepositoryEntryPointReadModel struct {
	Available bool
	Rows      []map[string]any
}

// RepositoryDeploymentEvidenceReadModel carries content-derived deployment
// evidence rows for one repository.
//
// Truncated reports that Limit cut the row set, so the response says the
// evidence is partial rather than presenting a short list as complete.
type RepositoryDeploymentEvidenceReadModel struct {
	Available bool
	Rows      []map[string]any
	Limit     int
	Truncated bool
}

// RelationshipEvidenceReadModel carries the evidence row for one resolved
// relationship.
type RelationshipEvidenceReadModel struct {
	Available bool
	Row       map[string]any
}

// ServiceStoryTargetSupportFilter selects the documentation support evidence
// attached to one service story.
type ServiceStoryTargetSupportFilter struct {
	Repository string
	TargetKind string
	TargetID   string
	ServiceID  string
	Limit      int
}

// ServiceStoryTargetSupportReadModel carries the support block a service story
// embeds. A nil Support means the store had nothing to attach.
type ServiceStoryTargetSupportReadModel struct {
	Support map[string]any
}

// RepositoryEntryPointReadModelStore is the narrow optional port a
// ContentStore implements to answer entry-point reads directly. It lives
// here (promoted from root package query for #6060 lane B B3) so the moved
// repository handler family can assert a store against it without importing
// root; root keeps its unexported spelling as a structural twin.
type RepositoryEntryPointReadModelStore interface {
	RepositoryEntryPoints(context.Context, string) (RepositoryEntryPointReadModel, error)
}

// LoadRepositoryEntryPoints returns content-derived entry points when the
// content store can answer the narrow query directly. It lives here for the
// same reason as the port above; root keeps an unexported forwarder so its
// stayers and read-model tripwires compile unchanged.
func LoadRepositoryEntryPoints(ctx context.Context, content ContentStore, repoID string) []map[string]any {
	store, ok := content.(RepositoryEntryPointReadModelStore)
	if !ok || repoID == "" {
		return nil
	}
	readModel, err := store.RepositoryEntryPoints(ctx, repoID)
	if err != nil || !readModel.Available {
		return nil
	}
	return readModel.Rows
}

// ServiceStoryTargetSupportLimit bounds target-support reads. Root keeps an
// unexported alias so its service entries share the bound.
const ServiceStoryTargetSupportLimit = 10

// ServiceStoryTargetSupportStore is the narrow optional port a ContentStore
// implements to answer target-support reads directly. It lives here
// (promoted from root package query for #6060 lane B B3) so the moved
// repository handler family can assert a store against it without importing
// root; root keeps its unexported spelling as a structural twin.
type ServiceStoryTargetSupportStore interface {
	ServiceStoryTargetSupportEvidence(context.Context, ServiceStoryTargetSupportFilter) (ServiceStoryTargetSupportReadModel, error)
}

// LoadRepositoryStoryTargetSupport returns target-support evidence for one
// repository when the content store can answer the narrow query directly.
// It lives here for the same reason as the port above; root keeps an
// unexported forwarder so its stayers compile unchanged.
func LoadRepositoryStoryTargetSupport(ctx context.Context, content ContentStore, repoID string) (map[string]any, error) {
	store, ok := content.(ServiceStoryTargetSupportStore)
	if !ok || store == nil {
		return nil, nil
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil, nil
	}
	readModel, err := store.ServiceStoryTargetSupportEvidence(ctx, ServiceStoryTargetSupportFilter{
		Repository: repoID,
		TargetKind: "repository",
		TargetID:   repoID,
		Limit:      ServiceStoryTargetSupportLimit,
	})
	if err != nil {
		return nil, err
	}
	return readModel.Support, nil
}

// IsRepositoryEntryPointName reports whether a function name is a known
// repository entry point. The implementation moved here for #6060 so both the
// repository handler family and the root ContentReader stayers share one
// spelling; root keeps its unexported wrapper.
func IsRepositoryEntryPointName(name string) bool {
	switch name {
	case "main", "handler", "app", "create_app", "lambda_handler",
		"Main", "Handler", "App", "CreateApp", "LambdaHandler":
		return true
	default:
		return false
	}
}
