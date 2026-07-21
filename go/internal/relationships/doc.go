// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationships extracts Terraform, Helm, Kustomize, Argo CD, Flux,
// GCP provider-resource, Terraform-provider schema, and related deployment
// evidence before reducer admission.
//
// The package describes evidence rather than inventing deployment truth:
// extractors emit candidate references, template parameters, and
// first-party reference signals that the reducer later admits or rejects.
// Argo CD extraction treats Application source_repos as independent deployment
// source evidence while preserving singular source_repo compatibility and
// positional source tuple details for path, root, and revision metadata.
// GCP provider relationship extraction is deliberately narrow: it emits
// evidence only when supported resource endpoints each match one distinct
// catalog repository.
// Ambiguous signals must remain ambiguous in the output of this package
// until a stronger contract admits them. Extractors should be
// deterministic over the same input bytes and schema inputs so repeated runs
// over a snapshot converge.
package relationships
