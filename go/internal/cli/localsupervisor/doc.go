// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package localsupervisor runs the local Eshu service: it takes ownership of a
// workspace, starts embedded Postgres and (in the authoritative profile) a
// NornicDB graph backend, supervises the ingester, reducer, and MCP server as
// child processes, and tears all of it down again on exit.
//
// One process owns one workspace at a time. RunOwnedHostWithLayout holds the
// workspace owner lock for its whole run and writes an owner record naming the
// pids, ports, and credentials it manages, so a second `eshu graph start`,
// `eshu mcp start`, or `eshu vuln scan` attaches to the running owner instead of
// starting a competing one. Every resource the owner starts is unwound in
// reverse on the way out, including when startup fails part-way, so a crashed or
// interrupted run does not leave an embedded Postgres or a graph backend behind.
// A stale record left by a killed owner is reclaimed under that same lock, never
// by signalling a pid that may have been reused.
//
// Two profiles. local_lightweight runs Postgres and the ingester only.
// local_authoritative additionally resets per-run state, starts the graph
// backend, applies the graph schema, and runs the reducer plus two one-shot
// finalizers that wait for the first queue drain (deferred content-search
// indexes and IaC reachability materialization).
//
// The graph backend runs either as a child process or in-process. The
// in-process runtime is linked in only under the nolocalllm build tag; a plain
// build reports it unavailable and requires ESHU_NORNICDB_RUNTIME=process.
// graph_embedded_nornicdb.go and graph_embedded_stub.go declare the same two
// names under mutually exclusive tags, so exactly one is ever compiled.
//
// This package holds no cobra wiring and writes to no process stream of its
// own. Callers pass an io.Writer for progress output and read the results the
// functions return; go/cmd/eshu's graph.go, local_host.go, and service.go are
// the cobra wrappers that read flags, print, and own the exit-code contract.
// That split is mechanical: go/cmd/eshu is `package main`, so nothing can import
// it, and any symbol reading a flag has to stay there.
//
// The environment is this package's main configuration surface, in both
// directions. It READS ESHU_QUERY_PROFILE and ESHU_GRAPH_BACKEND (the owner
// profile), ESHU_LOCAL_PROGRESS_MODE, ESHU_LOCAL_LOG_MODE, ESHU_LOCAL_LOG_DIR
// (progress and child log wiring), ESHU_NORNICDB_RUNTIME and
// ESHU_NORNICDB_BINARY (which graph runtime and which binary),
// ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES, and the worker-count
// variables it will not overwrite (ESHU_REDUCER_WORKERS, ESHU_SNAPSHOT_WORKERS,
// ESHU_PARSE_WORKERS, ESHU_PROJECTOR_WORKERS). It WRITES a much larger set into
// the child environments it builds: the store DSNs, listen addresses, watch and
// filesystem settings, and the ESHU_NEO4J_*/NEO4J_*/NORNICDB_* graph connection
// variables. procexec.Environ is the seam it reads the parent environment
// through, and procexec.MergeEnvironment is how those child environments are
// layered; both live in go/internal/cli/procexec so the CLI wrapper and this
// package share one seam a test can stub.
package localsupervisor
