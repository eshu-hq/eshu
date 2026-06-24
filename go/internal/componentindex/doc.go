// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package componentindex validates the static community extension index.
//
// The package treats index membership as advisory metadata. It checks reviewed
// entries for deterministic schema, digest, lifecycle, revocation, fact-kind
// ownership, source-confidence, consumer-contract, signature, and conformance
// proof errors, but it never installs packages, pulls artifacts, or overrides
// local component trust policy.
package componentindex
