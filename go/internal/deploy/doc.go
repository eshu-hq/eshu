// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deploy owns the eshu-deploy extraction census: the inventory of
// deployment assets (Helm, Compose, GitOps examples, deployment defaults,
// and the cross-repository compatibility matrix) that moved to
// eshu-hq/eshu-deploy under issue #8871, and the contract tests that keep
// the census honest after the in-tree removal.
//
// The census record is docs/internal/evidence/8871-eshu-deploy-census.md
// and the deploy contract is
// docs/internal/evidence/8871-eshu-deploy-contract.md. Both records are
// frozen extraction evidence: the tests pin their prose, and the living
// core-owned boundary (schema-bootstrap and repair) is asserted against
// the pinned eshu-deploy checkout, not the removed in-tree surface.
// Schema-bootstrap and repair implementation
// (go/cmd/bootstrap-data-plane) stays core-owned per the approved
// ecosystem migration plan; the chart Job that invokes it moved.
package deploy
