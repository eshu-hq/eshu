// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Per-repo read-set waits (#10877): the production wiring composes the real
// postgres.RelationshipStore as the restricted loader, so these assertions
// pin store-to-interface satisfaction at compile time in the only package
// that imports both sides (postgres cannot import the reducer root without
// a cycle). A method-set drift that satisfies only test stubs would compile
// silently and disable fingerprint recording in production: the handlers
// fall back to the non-fingerprinted branch and the reopen gate reopens
// every succeeded item on every pass.
var (
	_ reducer.RestrictedCorpusFencedResolvedRelationshipLoader                     = (*postgres.RelationshipStore)(nil)
	_ reducer.FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader        = (*postgres.RelationshipStore)(nil)
	_ servicecatalog.RestrictedCorpusFencedResolvedRelationshipLoader              = (*postgres.RelationshipStore)(nil)
	_ servicecatalog.FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader = (*postgres.RelationshipStore)(nil)
)
