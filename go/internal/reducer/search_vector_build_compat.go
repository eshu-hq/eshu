// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/searchvector"

// This file is the transitional compatibility surface for the search-vector
// build family that moved to [searchvector] (issue #6061). It carries only
// the names still referenced from the reducer root: the Service struct field
// that wires the runner as a side-goroutine, and this package's own
// TestServiceStartsSearchVectorBuildRunner wiring proof. Every other caller
// (cmd/reducer's runner construction and adapters) imports searchvector
// directly. Each entry here is deleted once its last root caller has moved.

// SearchVectorBuildRunner builds derived vector rows beside normal reducer
// work, wired as a Service side-runner. See
// [searchvector.SearchVectorBuildRunner].
type SearchVectorBuildRunner = searchvector.SearchVectorBuildRunner

// SearchVectorBuildRunnerConfig configures the reducer sidecar that builds
// derived vector rows for the semantic/hybrid search read path. See
// [searchvector.SearchVectorBuildRunnerConfig].
type SearchVectorBuildRunnerConfig = searchvector.SearchVectorBuildRunnerConfig

// SearchVectorBuildPendingScope identifies one active scope that needs
// vector rows for its curated search documents. See
// [searchvector.SearchVectorBuildPendingScope].
type SearchVectorBuildPendingScope = searchvector.SearchVectorBuildPendingScope

// SearchVectorBuildPendingRequest bounds pending vector build discovery. See
// [searchvector.SearchVectorBuildPendingRequest].
type SearchVectorBuildPendingRequest = searchvector.SearchVectorBuildPendingRequest

// SearchVectorBuildRequest identifies one bounded vector build for a scope.
// See [searchvector.SearchVectorBuildRequest].
type SearchVectorBuildRequest = searchvector.SearchVectorBuildRequest

// SearchVectorBuildResult summarizes a vector build attempt. See
// [searchvector.SearchVectorBuildResult].
type SearchVectorBuildResult = searchvector.SearchVectorBuildResult
