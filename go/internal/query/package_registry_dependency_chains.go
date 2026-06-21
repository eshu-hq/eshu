package query

import (
	"context"
	"fmt"
	"sort"
)

const (
	packageConsumptionRelationshipKind = "consumption"
)

// PackageDependencyChainRequest bounds a repo-scoped package dependency chain
// resolution. RepositoryID is the already-resolved canonical consumer repository
// id; Limit bounds the consumption page (and therefore the distinct package set
// the batched publisher read fans out over).
type PackageDependencyChainRequest struct {
	RepositoryID         string
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
) ([]PackageDependencyChain, error) {
	if store == nil {
		return nil, fmt.Errorf("package registry correlation store is required")
	}
	if req.RepositoryID == "" {
		return nil, fmt.Errorf("repository_id is required")
	}
	if req.Limit <= 0 || req.Limit > packageRegistryMaxLimit {
		return nil, fmt.Errorf("limit must be between 1 and %d", packageRegistryMaxLimit)
	}

	consumption, err := store.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		RepositoryID:         req.RepositoryID,
		RelationshipKind:     packageConsumptionRelationshipKind,
		AllowedRepositoryIDs: req.AllowedRepositoryIDs,
		AllowedScopeIDs:      req.AllowedScopeIDs,
		Limit:                req.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve package dependency chains (consumption): %w", err)
	}
	if len(consumption) == 0 {
		return []PackageDependencyChain{}, nil
	}

	packageIDs := distinctConsumedPackageIDs(consumption)
	publishersByPackage, err := loadPackagePublishers(ctx, store, req, packageIDs)
	if err != nil {
		return nil, err
	}

	chains := make([]PackageDependencyChain, 0, len(consumption))
	for _, row := range consumption {
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
	return chains, nil
}

// loadPackagePublishers performs the single batched publication/ownership read
// and groups the provenance-only publisher legs by package id, excluding
// self-references (publisher repo == consumer repo) so a repo never appears to
// depend on itself through its own published package.
func loadPackagePublishers(
	ctx context.Context,
	store PackageRegistryCorrelationStore,
	req PackageDependencyChainRequest,
	packageIDs []string,
) (map[string][]PackageDependencyChainPublisher, error) {
	publishersByPackage := make(map[string][]PackageDependencyChainPublisher, len(packageIDs))
	if len(packageIDs) == 0 {
		return publishersByPackage, nil
	}
	publishers, err := store.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		PackageIDs:           packageIDs,
		AllowedRepositoryIDs: req.AllowedRepositoryIDs,
		AllowedScopeIDs:      req.AllowedScopeIDs,
		Limit:                packageRegistryMaxLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve package dependency chains (publishers): %w", err)
	}
	for _, row := range publishers {
		if row.RelationshipKind == packageConsumptionRelationshipKind {
			continue
		}
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
	return publishersByPackage, nil
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
