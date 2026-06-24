// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main wires the component extension collector worker.
//
// The binary reads trusted, claim-capable component activations from the local
// component registry, launches the manifest-declared adapter through
// extensionhost, and commits accepted SDK facts through collector.ClaimedService.
// It supports the process adapter (local process) and the OCI adapter (a
// digest-pinned artifact run under container isolation, with the image taken
// only from the component's verified manifest artifact). It never gives
// extensions direct Postgres, graph, reducer, API, MCP, or workflow-control
// handles.
package main
