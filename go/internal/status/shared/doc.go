// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shared holds the value primitives every status section is built
// from, so the family leaves under internal/status can use them without
// importing the parent status package.
//
// The package exists for one structural reason: internal/status aggregates
// every family leaf into RawSnapshot and Report, so the root imports the
// leaves. A leaf that needed NamedCount or a JSON scalar helper from the root
// would close that loop into an import cycle. Everything here is therefore
// leaf-safe by construction: it depends on nothing else in internal/status and
// must stay that way.
//
// NamedCount is the name/count pair that scope, generation, coordinator,
// registry, backpressure and semantic-extraction sections all report. CountMap
// folds those pairs into a total per name, accumulating repeats rather than
// overwriting them and dropping blank names. FormatTotals renders such a map
// as operator text in lifecycle order — active, pending, completed, succeeded,
// failed, then anything else alphabetically — omitting non-positive buckets
// and returning "none" when nothing is left.
//
// NamedCountJSON, NamedCountsJSON and NullableRFC3339Value carry the
// operator-facing wire contract. Their struct tags and formatting are part of
// the published status JSON: NamedCountsJSON always returns a non-nil slice so
// an empty section renders as [] rather than null, and NullableRFC3339Value
// renders UTC RFC3339 or the empty string for an unset timestamp. Changing any
// of them changes the API, not just this package.
//
// NonNegativeDuration clamps status-age fields that can briefly go negative
// when database timestamps are newer than the status read clock.
package shared
