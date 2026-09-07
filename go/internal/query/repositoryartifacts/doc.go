// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repositoryartifacts holds the file-content artifact readers for
// the repository handler family (Issue #6060, lane B): config artifacts
// (Ansible, Compose, HCL, Kustomize), deployment artifacts, runtime
// artifacts (Dockerfile), controller artifacts, CloudFormation artifacts,
// and workflow artifacts (GitHub Actions), plus the shared candidate-file
// hydration and the config/deployment/workflow artifact loaders. It imports
// only the standard library, querycontract, and content-parsing libraries,
// never the query root.
package repositoryartifacts
