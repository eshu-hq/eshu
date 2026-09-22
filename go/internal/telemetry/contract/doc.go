// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract holds the frozen, per-family Go data-plane telemetry
// declarations that root package telemetry used to define directly in its
// flat contract_*.go files (issue #6777): span name, metric-dimension, and
// log-key constants, their bounded label values, and the small Attr* helpers
// and exported vocabularies that go with them (VulnerabilitySuppressionMutationOutcomes,
// AttrLanguage, AttrStage, AttrSourceTool).
//
// This package holds ONLY declarations. It never imports root package
// telemetry (root imports this package, so the reverse would cycle) and it
// registers nothing: it has no func init() and does not append to any
// ordered slice. The explicit, load-bearing order in which each family's
// span/dimension/log-key set is spliced into the root spanNames,
// metricDimensionKeys, and logKeys slices lives entirely in root
// registration.go and registration_steps.go, not here.
//
// Every root telemetry.* identifier that used to be a bare package-level
// name in this package's old contract_*.go home is still available as a
// compat alias from root (see compat_contract.go); existing callers outside
// go/internal/telemetry are unaffected by this package's existence and are
// not expected to import it directly. New callers inside
// go/internal/telemetry may reference contract.* directly instead of the
// root alias.
//
// File names carry the z_, zz_, zzz_, and zzzz_ prefixes their pre-move
// contract_z_*.go, contract_zz_*.go, etc. counterparts had. Those prefixes no
// longer control registration order — root registration.go's explicit
// registrationSteps slice does — and are kept only for history and to match
// the owner-approved target tree.
package contract
