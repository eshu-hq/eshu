// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deploy owns the eshu-deploy extraction census: the inventory of
// deployment assets (Helm, Compose, GitOps examples, deployment defaults,
// and the cross-repository compatibility matrix) that move to
// eshu-hq/eshu-deploy under issue #8871, and the contract tests that keep
// the census honest while the assets still live in-tree.
//
// The census record is docs/internal/evidence/8871-eshu-deploy-census.md.
// The tests assert it against the tree so the record cannot drift silently
// when a family moves before extraction. Schema-bootstrap and repair
// implementation (go/cmd/bootstrap-data-plane) stays core-owned per the
// approved ecosystem migration plan; the chart Job that invokes it moves.
package deploy
