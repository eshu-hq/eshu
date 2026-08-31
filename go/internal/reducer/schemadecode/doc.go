// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schemadecode turns a stored fact payload into the typed value a
// reducer handler works with.
//
// Each exported function is one decode seam: it takes a fact envelope, decodes
// the payload against the matching sdk/go/factschema domain package, and either
// returns the typed value or classifies the payload as a terminal dead letter
// through [factdecode]. A decode failure is never fatal to the run — the fact is
// quarantined and the pass continues.
//
// The package sits below the reducer root and below the domain families, above
// [factdecode]. It imports the per-domain factschema packages by design, which
// is why these seams live here rather than in factdecode: that package is the
// dependency-light mechanism tier and deliberately keeps domain schema packages
// out of its import set.
//
// A decoder is named for the fact kind it decodes, not for a family that owns
// it. Several families consume the same fact kind — the ci.run decoders are
// called from both the ci_cd_run correlation and the container-image identity
// paths — so these are shared leaf functions rather than family-local helpers.
//
// Filenames matter here. The payload-usage manifest gate resolves decode seams
// by the factschema_decode*.go basename and searches one directory below the
// reducer root (#6055), so a file keeps its basename when it moves into this
// package. Renaming one makes its fact kinds invisible to that gate.
package schemadecode
