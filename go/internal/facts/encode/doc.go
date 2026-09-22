// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package encode holds the payload-encoding substrate shared by the facts
// root and its nested fact families: the deterministic stable-ID function
// and the pointer/decode helpers encoders use to build fact payloads.
//
// It exists to break an import cycle created by the facts-package nesting
// (issue #6776): go/internal/facts imports its nested families (its
// schemaVersionFamilies table references cloud through compat_cloud.go,
// and its semantic and documentation encoders reference the docs
// package), so a nested family cannot import the facts root back to
// reach a shared helper. This package depends on neither facts nor any
// nested family, so both sides can import it.
//
// StableID(factType, identity) moved verbatim from the facts root's
// former stableid.go. go/internal/facts.StableID is now a thin forwarder
// to [StableID], so every existing caller keeps compiling unchanged and
// computes the byte-identical id it did before the move: the same
// SHA-256 hash over the same
// {"fact_type": factType, "identity": normalize(identity)} encoding, the
// same UTC RFC3339Nano time normalization, and the same recursive
// map/slice normalization.
//
// IntPtr, StringPtr, BoolPtr, StringValue, StringPtrFromMap, IntPtrFromMap,
// and JSONShapeMap moved from unexported helpers at the bottom of the facts
// root's former documentation_encode.go (now docs/encode.go) and from
// semantic_encode.go's unexported intPtr, and were exported so both the docs
// package and the facts root's own semantic_encode.go can call them without
// either owning the other. Behavior is unchanged: a zero int, an empty
// string, and a false bool all encode as an absent (nil) optional field,
// never a present zero/empty/false value; StringPtrFromMap, IntPtrFromMap,
// and StringValue decode a payload map permissively, returning the zero
// value on a missing key or a type mismatch rather than an error.
//
// JSONShapeMap takes and returns only the map. The two-value
// (payload, err) form each encoder call site wants stays unexported in the
// owning package -- docs.jsonShapePayload and the facts root's own
// jsonShapePayload -- so the error an encoder returns is returned by
// same-package code and keeps its original text. Routing it back out through
// this package would make every encoder return an error from an external
// package with no context to add, which wrapcheck correctly rejects.
//
// This package holds no I/O and no fact-kind vocabulary; every StableID
// caller supplies its own identity map, and every JSONShapeMap/Ptr caller
// supplies its own payload.
package encode
