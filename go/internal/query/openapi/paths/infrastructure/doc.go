// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package infrastructure holds the OpenAPI 3.0 path fragments for the
// infra-resource, Kubernetes, observability-coverage, and secrets/IAM
// routes (Issue #6060, lane C): Routes, ResourceAggregate, Kubernetes,
// ObservabilityCoverage, and SecretsIAM — each an exported JSON string
// constant that openapi.Spec concatenates into the published document.
//
// None of these fragments import openapi/schema; each is self-contained.
//
// This package holds route-fragment data only, no handler logic. It MUST
// NOT import the parent openapi package: openapi/spec.go imports every
// paths/<family> leaf, so the reverse import is a cycle.
package infrastructure
