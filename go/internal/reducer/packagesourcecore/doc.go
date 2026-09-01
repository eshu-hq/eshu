// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packagesourcecore holds the reducer's package-registry source-hint
// matching primitives: the Hint and Repository shapes package source
// correlation reduces facts into, the repository extraction that reads them
// out of a fact-envelope batch, and the canonical-URL matching that decides
// whether a hint's source URL names an active repository.
//
// It exists below the reducer root, not as a `packagesource` family, because
// its callers are not that family. BuildPackageSourceCorrelationDecisions and
// the handler that classifies a hint into a correlation outcome stay in the
// reducer root; seven other reducer-root files read these symbols directly
// without ever calling that handler -- package_consumption_correlation.go and
// package_publication_correlation.go read Repository/Hint and
// ExtractRepositories; container_image_identity_provenance.go reads Hint,
// Repository, ExtractRepositories, MatchRepositories, and CanonicalURLKey;
// container_image_identity_slsa.go reads only ExtractRepositories;
// service_catalog_correlation_classify.go and
// service_catalog_correlation_lookup.go read only CanonicalURLKey; and
// supply_chain_impact_python_reachability.go reads only
// RepositoryIDFromScope. A family move would drag the handler's ~650 lines
// along to deliver these ~65 (issue #6379, epic #6061).
package packagesourcecore
