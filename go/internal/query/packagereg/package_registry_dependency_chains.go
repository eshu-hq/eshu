// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"context"
	"fmt"
	"sort"
)

const (
	packageConsumptionRelationshipKind = "consumption"
)

// packagePublisherRelationshipKinds are the fact relationship_kind values that
// identify a repository as a publisher of a package. They are the only kinds
// fetched in the phase-2 batched read so that the bounded LIMIT page is never
// filled by consumption rows for popular packages, which would silently drop
// publisher rows off the page.
var packagePublisherRelationshipKinds = []string{
	"publication",
	"ownership",
}

// PackageDependencyChainRequest bounds a repo-scoped package dependency chain
// resolution. RepositoryID is the already-resolved canonical consumer repository
// id; Limit bounds the consumption page (and therefore the distinct package set
// the batched publisher read fans out over).
//
// AfterCorrelationID is the keyset cursor for the consumption page: when set the
// query returns consumption correlations whose fact_id is strictly greater than
// this value, enabling callers to fetch subsequent pages using the
// next_cursor.after_correlation_id value returned by the handler.
type PackageDependencyChainRequest struct {
	RepositoryID         string
	AfterCorrelationID   string
	Limit                int
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// PackageDependencyChainPublisher is one provenance-only publisher leg of a
// package dependency chain. It is sourced from package publication/ownership
// correlations that the reducer intentionally holds provenance-only
// (canonical_writes=0) until corroborating build/release/CI evidence exists, so
// it MUST be surfaced as an inferred link, never an asserted canonical edge.
type PackageDependencyChainPublisher struct {
	CorrelationID    string
	RelationshipKind string
	RepositoryID     string
	RepositoryName   string
	SourceURL        string
	Outcome          string
	Reason           string
	ProvenanceOnly   bool
	CanonicalWrites  int
}

// PackageDependencyChain is one consumer-repo -> package -> publisher-repo chain
// resolved entirely in the read lane. The consumption leg is canonical
// (manifest-backed, canonical_writes>=1); the publisher legs are provenance-only
// inferred links. The chain never materializes a graph edge.
type PackageDependencyChain struct {
	ConsumerRepositoryID      string
	ConsumerRepositoryName    string
	PackageID                 string
	PackageName               string
	Ecosystem                 string
	DependencyRange           string
	ConsumptionCorrelationID  string
	ConsumptionProvenanceOnly bool
	ConsumptionCanonicalWrite int
	Publishers                []PackageDependencyChainPublisher
	// Ambiguous is true when more than one candidate publisher repository
	// remains after self-references are removed. The resolver never collapses
	// that to a single asserted publisher.
	Ambiguous bool
}

// PackageDependencyChainPage is one bounded page of consumer -> package ->
// publisher dependency chains. Truncated and NextCursorCorrelationID are
// forwarded from the underlying phase-1 consumption read's
// PackageRegistryCorrelationPage -- derived from the RAW fetched consumption
// fact count/fact_id sequence, never from len(Chains) -- so a malformed or
// unsupported-version consumption fact inside the visible window cannot make
// a truncated page report itself complete or corrupt the forward cursor
// (#5816 finding on #5461).
type PackageDependencyChainPage struct {
	Chains                  []PackageDependencyChain
	Truncated               bool
	NextCursorCorrelationID string
	// PublishersTruncated reports whether the phase-2 batched
	// publication/ownership read (loadPackagePublishers) hit its own
	// packageRegistryMaxLimit cap for the distinct package set this page
	// consumes. It is a separate signal from Truncated, which describes only
	// the phase-1 consumption page: a page can be Truncated=false (every
	// consumption correlation for this request fit) while still carrying
	// PublishersTruncated=true (one of those packages has more
	// publisher/ownership facts than the batched read's LIMIT), because the
	// two reads are bounded independently. Before this field existed,
	// publisher facts beyond the cap were silently dropped from every
	// dependency chain with no signal to the caller (#5816 finding on
	// #5461).
	PublishersTruncated bool
}

// ResolvePackageDependencyChains joins admitted consumption correlations
// (consumer repo -> package) with provenance-only publication/ownership
// correlations (package -> publisher repo) for a single repository, in two
// bounded reads:
//
//  1. one consumption read anchored on the consumer repository, and
//  2. one batched publication/ownership read keyed by the distinct set of
//     consumed package ids (payload package_id = ANY ...).
//
// It deliberately resolves on the read side and writes no graph edge: the
// publisher legs are provenance-only and the package canonical writer is barred
// from adding Repository/ownership edges, so promoting these into a hard
// DEPENDS_ON edge would over-admit provenance-only truth.
func ResolvePackageDependencyChains(
	ctx context.Context,
	store PackageRegistryCorrelationStore,
	req PackageDependencyChainRequest,
) (PackageDependencyChainPage, error) {
	if store == nil {
		return PackageDependencyChainPage{}, fmt.Errorf("package registry correlation store is required")
	}
	if req.RepositoryID == "" {
		return PackageDependencyChainPage{}, fmt.Errorf("repository_id is required")
	}
	if req.Limit <= 0 || req.Limit > packageRegistryMaxLimit {
		return PackageDependencyChainPage{}, fmt.Errorf("limit must be between 1 and %d", packageRegistryMaxLimit)
	}

	consumptionPage, err := store.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		RepositoryID:         req.RepositoryID,
		RelationshipKind:     packageConsumptionRelationshipKind,
		AfterCorrelationID:   req.AfterCorrelationID,
		AllowedRepositoryIDs: req.AllowedRepositoryIDs,
		AllowedScopeIDs:      req.AllowedScopeIDs,
		Limit:                req.Limit,
	})
	if err != nil {
		return PackageDependencyChainPage{}, fmt.Errorf("resolve package dependency chains (consumption): %w", err)
	}
	if len(consumptionPage.Rows) == 0 {
		return PackageDependencyChainPage{
			Chains:                  []PackageDependencyChain{},
			Truncated:               consumptionPage.Truncated,
			NextCursorCorrelationID: consumptionPage.NextCursorCorrelationID,
		}, nil
	}

	packageIDs := distinctConsumedPackageIDs(consumptionPage.Rows)
	publishersByPackage, publishersTruncated, err := loadPackagePublishers(ctx, store, req, packageIDs)
	if err != nil {
		return PackageDependencyChainPage{}, err
	}

	chains := make([]PackageDependencyChain, 0, len(consumptionPage.Rows))
	for _, row := range consumptionPage.Rows {
		if row.PackageID == "" {
			continue
		}
		publishers := publishersByPackage[row.PackageID]
		chains = append(chains, PackageDependencyChain{
			ConsumerRepositoryID:      row.RepositoryID,
			ConsumerRepositoryName:    row.RepositoryName,
			PackageID:                 row.PackageID,
			PackageName:               row.PackageName,
			Ecosystem:                 row.Ecosystem,
			DependencyRange:           row.DependencyRange,
			ConsumptionCorrelationID:  row.CorrelationID,
			ConsumptionProvenanceOnly: row.ProvenanceOnly,
			ConsumptionCanonicalWrite: row.CanonicalWrites,
			Publishers:                publishers,
			Ambiguous:                 len(publishers) > 1,
		})
	}
	return PackageDependencyChainPage{
		Chains:                  chains,
		Truncated:               consumptionPage.Truncated,
		NextCursorCorrelationID: consumptionPage.NextCursorCorrelationID,
		PublishersTruncated:     publishersTruncated,
	}, nil
}

// loadPackagePublishers performs the single batched publication/ownership read
// and groups the provenance-only publisher legs by package id, excluding
// self-references (publisher repo == consumer repo) so a repo never appears to
// depend on itself through its own published package. The second return value
// is publisherPage.Truncated: true when the distinct package set this request
// consumes carries more publisher/ownership facts than the batched read's
// packageRegistryMaxLimit cap, so facts beyond it were dropped from
// publishersByPackage. Callers MUST propagate it rather than discarding it --
// silently dropping publishers beyond the cap with no signal was the #5816
// finding on #5461.
func loadPackagePublishers(
	ctx context.Context,
	store PackageRegistryCorrelationStore,
	req PackageDependencyChainRequest,
	packageIDs []string,
) (map[string][]PackageDependencyChainPublisher, bool, error) {
	publishersByPackage := make(map[string][]PackageDependencyChainPublisher, len(packageIDs))
	if len(packageIDs) == 0 {
		return publishersByPackage, false, nil
	}
	publisherPage, err := store.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		PackageIDs:           packageIDs,
		RelationshipKinds:    packagePublisherRelationshipKinds,
		AllowedRepositoryIDs: req.AllowedRepositoryIDs,
		AllowedScopeIDs:      req.AllowedScopeIDs,
		Limit:                packageRegistryMaxLimit,
	})
	if err != nil {
		return nil, false, fmt.Errorf("resolve package dependency chains (publishers): %w", err)
	}
	for _, row := range publisherPage.Rows {
		if row.RepositoryID == "" {
			continue
		}
		if row.RepositoryID == req.RepositoryID {
			// Self-reference: a repo publishing a package it also consumes is
			// not a repo-to-repo dependency.
			continue
		}
		publishersByPackage[row.PackageID] = append(publishersByPackage[row.PackageID], PackageDependencyChainPublisher{
			CorrelationID:    row.CorrelationID,
			RelationshipKind: row.RelationshipKind,
			RepositoryID:     row.RepositoryID,
			RepositoryName:   row.RepositoryName,
			SourceURL:        row.SourceURL,
			Outcome:          row.Outcome,
			Reason:           row.Reason,
			ProvenanceOnly:   row.ProvenanceOnly,
			CanonicalWrites:  row.CanonicalWrites,
		})
	}
	return publishersByPackage, publisherPage.Truncated, nil
}

// distinctConsumedPackageIDs returns the sorted, de-duplicated set of non-empty
// package ids in the consumption page so the batched publisher read carries a
// stable, bounded key set.
func distinctConsumedPackageIDs(rows []PackageRegistryCorrelationRow) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.PackageID == "" {
			continue
		}
		if _, ok := seen[row.PackageID]; ok {
			continue
		}
		seen[row.PackageID] = struct{}{}
		ids = append(ids, row.PackageID)
	}
	sort.Strings(ids)
	return ids
}
