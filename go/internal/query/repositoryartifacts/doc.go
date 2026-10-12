// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repositoryartifacts holds the file-content artifact readers for
// the repository handler family (Issue #6060, lane B): config artifacts
// (Ansible, Compose, HCL, Kustomize), deployment artifacts, runtime
// artifacts (Dockerfile), controller artifacts, CloudFormation artifacts,
// and workflow artifacts (GitHub Actions), plus the shared candidate-file
// hydration and the config/deployment/workflow artifact loaders. It imports
// only the standard library, querycontract, go/internal/content/artifacts and
// content-parsing libraries, never the query root. The config-artifact
// extractors and the per-repository function that fetches file content only for
// the files an extractor reads (#7126) live in go/internal/content/artifacts;
// this package adapts querycontract.FileContent to it and merges the rows of
// every repository. The loader gives each repository a floor of
// contentartifacts.HydrationFloor candidates and shares CandidateHydrationBudget
// more per request in RepoID order (#7951); ConfigArtifactReadStats carries the
// candidate counts and BoundReasons the reasons a response must disclose. See
// README.md.
package repositoryartifacts
