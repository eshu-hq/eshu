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
	// RepositoryWorkloadCount is how many Workloads the canonical graph says
	// Repository DEFINES, read once per story and bounded at
	// ServiceStoryRepositoryWorkloadReadLimit. It is 0 when the graph is
	// unavailable, the read failed, or the story context is identity-only.
	// Repository targets ignore it.
	RepositoryWorkloadCount int
	// RepositoryDefinesTarget reports that TargetID is among the Workloads the
	// graph says Repository DEFINES. A service target may receive
	// repository-linked support only when this is true and
	// RepositoryWorkloadCount is 1; a count of 2 or more with this true makes
	// that support ambiguous (#7138).
	RepositoryDefinesTarget bool
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

// ServiceStoryRepositoryWorkloadReadLimit bounds the graph read that lists the
// Workloads a repository DEFINES for the service-story target-support gate. It
// only has to tell zero from one from two-or-more, so three rows are enough.
const ServiceStoryRepositoryWorkloadReadLimit = 3

// ServiceStoryTargetSupportStore is the narrow optional port a ContentStore
// implements to answer target-support reads directly. It lives here
// (promoted from root package query for #6060 lane B B3) so the moved
// repository handler family can assert a store against it without importing
// root; root keeps its unexported spelling as a structural twin.
type ServiceStoryTargetSupportStore interface {
	ServiceStoryTargetSupportEvidence(context.Context, ServiceStoryTargetSupportFilter) (ServiceStoryTargetSupportReadModel, error)
}

// FirstMissingEvidenceReason returns the reason of the first missing_evidence
// entry of a target-support block, or "" when the block is nil, complete, or
// carries no reason. The story stage events log it so an operator can see why a
// story shows no support without reading the response.
func FirstMissingEvidenceReason(support map[string]any) string {
	missing := MapSliceValue(support, "missing_evidence")
	if len(missing) == 0 {
		return ""
	}
	return StringVal(missing[0], "reason")
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
