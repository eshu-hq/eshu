// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// Pinned to querycontract so impact/ tests can reference the bound without
	// importing the query root. See #6060.
	defaultIndirectEvidenceSearchLimit = querycontract.DefaultIndirectEvidenceSearchLimit
	maxIndirectEvidenceSearchLimit     = querycontract.MaxIndirectEvidenceSearchLimit
)

// provisioningRepositoryCandidate aliases the impacttrace home (moved there
// with lane B2 of #6060); the root producer and readers keep the
// package-local name.
type provisioningRepositoryCandidate = impacttrace.ProvisioningRepositoryCandidate

// loadProvisioningSourceChains loads the provisioning source chains for a
// service repository, returning the chains and whether the read underneath
// them hit its bound. #5720 round-7 P3-1: this wrapper and the two below used
// to drop the truncated bool on the floor with a comment noting no production
// caller existed yet -- exactly the discarded-flag pattern this issue exists
// to remove, and one a future production caller would silently inherit.
// loadProvisioningSourceChainsFromCandidates is 1:1 over the candidate slice
// with no capping of its own, so the candidate read's bool is the whole
// signal.
func loadProvisioningSourceChains(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
) ([]map[string]any, bool, error) {
	return loadProvisioningSourceChainsWithLimit(ctx, graph, content, serviceRepoID, 0)
}

// loadProvisioningSourceChainsWithLimit is loadProvisioningSourceChains with
// an explicit candidate-read bound. It returns the same truncation signal.
func loadProvisioningSourceChainsWithLimit(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	limit int,
) ([]map[string]any, bool, error) {
	candidates, truncated, err := impacttrace.QueryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
	if err != nil {
		return nil, false, err
	}
	chains, err := impacttrace.LoadProvisioningSourceChainsFromCandidates(ctx, content, candidates)
	if err != nil {
		return nil, false, err
	}
	return chains, truncated, nil
}

// loadConsumerRepositoryEnrichment loads the consumer repositories for a
// service at the default indirect-evidence search limit, returning the same
// merged truncation signal loadConsumerRepositoryEnrichmentFromCandidates
// documents (#5720 round-7 P3-1).
func loadConsumerRepositoryEnrichment(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
) ([]map[string]any, bool, error) {
	return loadConsumerRepositoryEnrichmentWithLimit(
		ctx,
		graph,
		content,
		serviceRepoID,
		serviceName,
		hostnames,
		defaultIndirectEvidenceSearchLimit,
	)
}

// loadConsumerRepositoryEnrichmentWithLimit is loadConsumerRepositoryEnrichment
// with an explicit bound. It returns the merged truncation signal from both
// stages.
//
// It passes false for source 0 (evidenceFilesTruncated) because it takes
// `hostnames` from its caller and never reads the service repository's file
// list itself, so it has nothing to report about that bound. A caller that
// derives those hostnames from loadServiceQueryEvidence owns the signal and
// must thread it in the way enrichServiceQueryContextWithOptions does; this
// wrapper has no production callers today.
func loadConsumerRepositoryEnrichmentWithLimit(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
) ([]map[string]any, bool, error) {
	candidates, candidatesTruncated, err := impacttrace.QueryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
	if err != nil {
		return nil, false, err
	}
	return impacttrace.LoadConsumerRepositoryEnrichmentFromCandidates(ctx, graph, content, serviceRepoID, serviceName, hostnames, limit, candidates, candidatesTruncated, false)
}

// queryProvisioningRepositoryCandidates probes the provisioning-candidate
// read one row past the caller's limit. The implementation moved to
// impacttrace for #6060; this wrapper keeps the staying deployment-trace
// tests calling the package-local name.
func queryProvisioningRepositoryCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceRepoID string,
	limit int,
) ([]provisioningRepositoryCandidate, bool, error) {
	return impacttrace.QueryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
}

// loadConsumerRepositoryEnrichmentFromCandidates merges graph-derived
// provisioning candidates with content-evidence consumer matches. The
// implementation moved to impacttrace for #6060; this wrapper keeps the
// staying deployment-trace tests calling the package-local name.
func loadConsumerRepositoryEnrichmentFromCandidates(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
	candidates []provisioningRepositoryCandidate,
	candidatesTruncated bool,
	evidenceFilesTruncated bool,
) ([]map[string]any, bool, error) {
	return impacttrace.LoadConsumerRepositoryEnrichmentFromCandidates(ctx, graph, content, serviceRepoID, serviceName, hostnames, limit, candidates, candidatesTruncated, evidenceFilesTruncated)
}

// BoundedIndirectEvidenceHostnamesForService chooses the hostnames most
// likely to identify the service itself before spending cross-repo content
// searches. The implementation moved to impacttrace for #6060; this wrapper
// keeps the staying deployment-trace tests calling the package-local name.
func BoundedIndirectEvidenceHostnamesForService(hostnames []string, serviceName string) ([]string, bool) {
	return impacttrace.BoundedIndirectEvidenceHostnamesForService(hostnames, serviceName)
}

// boundedTraceEnrichmentLimit converts a caller-supplied max_depth into a
// bounded indirect-evidence search limit. The implementation moved to
// querycontract for #6060; this wrapper keeps the staying investigation
// stayer and deployment-trace tests calling the package-local name.
func boundedTraceEnrichmentLimit(maxDepth int) int {
	return querycontract.BoundedTraceEnrichmentLimit(maxDepth)
}
