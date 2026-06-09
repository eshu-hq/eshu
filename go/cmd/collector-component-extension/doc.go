// Package main wires the process-backed component extension collector worker.
//
// The binary reads trusted, claim-capable component activations from the local
// component registry, launches the configured process adapter through
// extensionhost, and commits accepted SDK facts through collector.ClaimedService.
// It does not execute OCI artifacts yet and never gives extensions direct
// Postgres, graph, reducer, API, MCP, or workflow-control handles.
package main
