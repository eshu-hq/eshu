// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command fact-envelope-adapter generates the shared fact-envelope adapter.
//
// It emits go/internal/factenvelope/adapter_generated.go from one source so
// extensionhost, reducer, and projector code use the same SDK-to-durable and
// durable-to-factschema field mapping.
package main
