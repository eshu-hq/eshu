// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iac implements the IaC-quality and replatforming handler family
// (#6642 Part A, split off the #6060 lane A query-root restructure): Handler,
// its Mount method, and eleven routes -- the bounded Terraform/IaC resource
// browse (GET /api/v0/iac/resources), the dead-IaC scan
// (POST /api/v0/iac/dead), the unmanaged-cloud-resource finder
// (POST /api/v0/iac/unmanaged-resources), the IaC management status lookup
// and its explanation (POST /api/v0/iac/management-status,
// POST /api/v0/iac/management-status/explain), the Terraform import plan
// candidate builder (POST /api/v0/iac/terraform-import-plan/candidates), the
// AWS runtime drift findings list (POST /api/v0/aws/runtime-drift/findings),
// and the four replatforming routes -- the plan composer
// (POST /api/v0/replatforming/plans), the unmanaged-resource ownership packet
// builder (POST /api/v0/replatforming/ownership-packets), the drift/readiness
// rollups (POST /api/v0/replatforming/rollups), and the active AWS collector
// scope selector inventory (GET /api/v0/replatforming/selectors).
//
// Every route gates its read on one of this package's nine owned
// capabilities (DeadCapability, ManagementCapability,
// ManagementStatusCapability, ManagementExplainCapability,
// TerraformImportCapability, AWSRuntimeDriftFindingsCapability,
// ResourcesCapability, ReplatformingOwnershipCapability,
// ReplatformingRollupsCapability -- querycontract.CapabilityUnsupported) or on
// one of two capabilities this family's routes gate but does not own
// (ReplatformingPlanReadinessCapability, ReplatformingSelectorInventoryCapability
// -- registered in production by root's contract_replatforming.go, Part C);
// see capabilities.go's file doc comment for why this family carries its own
// copy of those two strings. Every route resolves the caller's
// repository/scope grant through querycontract.RepositoryAccessFilterFromContext
// (or selector.ResolveExactForAccess for a selector-bearing route) before
// binding it into the query, and reports a bounded, deterministically
// ordered, truncation-aware result.
//
// Currency for the resource browse: scoped and pre-ready reads resolve the
// active generation's facts through the current-inventory CTE, while
// unscoped reads served from infra_resource_entities serve last-projected
// content. The table mirrors content_entities rather than active facts, so
// it keeps repositories whose newest generation failed or is pending -- the
// same semantic the infra aggregate reads already ship -- and that is the
// intended answer on this path: it agrees with the authoritative graph the
// route hydrates from, where the CTE view would serve partial content or
// disagree and fail. The live parity suite pins both the healthy-corpus
// agreement and the failed-generation divergence.
//
// This package imports querycontract (profiles, envelopes, capability
// registration, HTTP helpers, RepositoryAccessFilterFromContext,
// DriftedAttributeView), selector (repository selector resolution),
// tracing (the shared handler-span seam), queryauth (AuthContext, only in
// tests), and storage/postgres (the concrete Postgres adapters and their row
// types); it MUST NOT import the query root, or root would cycle back through
// its own compatibility aliases in iac_alias.go, which import this package
// for the type aliases and forwarders that root, cmd/api, and cmd/mcp-server
// wiring still use. See README.md for the file layout and move evidence, and
// AGENTS.md for the per-symbol export rationale.
package iac
