// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package compparity holds the logic behind `eshu competitive-parity
// validate`: assembling the live inventory of CLI commands, API routes, MCP
// tools, console pages, and committed parity docs (Inventory, DocPaths),
// running the offline exercises that prove sibling artifact paths stay wired
// (ExerciseResults, SupportedSupplyChainPacket), and rendering the validated
// report as JSON or Markdown (Artifact).
//
// The scoring itself is not here. Expectations, validation, and rendering
// belong to internal/competitiveparity; this package feeds it a live
// inventory and hands back its rendered report. The exercises reach into
// internal/cli/opdigest, internal/cli/evidpacket, internal/packetdogfood,
// internal/capabilitycatalog, and internal/query, so a breaking change in any
// of those surfaces turns the parity gate red.
//
// Two inputs are injected by the caller instead of computed here, and both
// for the same reason: go/cmd/eshu is `package main`, so nothing can import
// it. Inventory takes the CLI command paths as a []string because walking the
// cobra tree needs rootCmd, and ExerciseResults takes the first-run report
// exercise as a func() error because the first-run evidence helpers still
// live in package main. A nil first-run exercise is recorded as a failed
// exercise, never a silent pass.
//
// Exercise failure details are static per-ID strings. The underlying errors
// can carry local filesystem paths, and the rendered artifact is share-safe
// output, so the real error text never reaches the report.
//
// go/cmd/eshu/competitive_parity_cmd.go is the thin cobra wrapper. It owns
// what this package deliberately does not: registering on rootCmd, reading
// the --repo-root, --json, and --out flags, walking the cobra tree for
// command paths, supplying the first-run exercise, writing the artifact to
// the --out path or cobra's stdout, and mapping a failing report to the
// process exit code.
package compparity
