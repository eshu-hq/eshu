// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packagesource builds the package-source-correlation reducer intent
// from one immutable scope generation: when the generation carries at least
// one package_registry.source_hint fact, or failing that at least one
// package_registry.package identity fact, it asks the reducer to classify the
// generation's registry source hints and manifest-backed package consumption
// against active Git facts once. The anchor is chosen by kind priority, then
// original input order — the earliest source hint wins even when an identity
// fact precedes it — so the reducer claim is stable across reprojections of
// the same generation. Only the fact kind is read; no payload is decoded, so
// a malformed hint never fails the build, and a generation with neither kind
// enqueues nothing. Source hints stay provenance-only here: the reducer's
// package_source_correlation handler owns ownership, publication, and
// consumption admission. Root projector assembly owns lookup construction and
// lifetime, invocation order, queue writes, retries, and telemetry.
package packagesource
