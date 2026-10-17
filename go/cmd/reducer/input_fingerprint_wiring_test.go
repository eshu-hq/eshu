// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The deployable_unit_correlation input fingerprint (#8283) switches on through
// a runtime type assertion in the reducer (loader.(InputFingerprintedResolved
// RelationshipLoader)). A signature that drifts, or a decorator wrapped around
// the store, would turn the gate off silently: every skip counter would stay 0
// and nothing would fail. These assertions make the production types satisfy
// the two ports at compile time. They live here because cmd/reducer imports
// both packages and the reducer package must not import postgres.
var (
	_ reducer.InputFingerprintedResolvedRelationshipLoader = (*postgres.RelationshipStore)(nil)
	_ reducer.DeployableUnitInputFingerprintWriter         = postgres.InputFingerprintStore{}
)
