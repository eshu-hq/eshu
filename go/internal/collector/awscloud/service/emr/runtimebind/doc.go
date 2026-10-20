// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the EMR scanner with the awsruntime registry
// from a package init function. Importing this package for its blank side
// effect is the only step a runtime takes to bring the EMR scanner into the
// production registry; no shared file is edited beyond the single blank import
// in awsruntime/bindings/bindings.go.
package runtimebind
