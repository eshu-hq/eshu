// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command ifa is the CLI entry point for Eshu's Ifá conformance platform.
//
// `ifa -version` prints the command's version banner. `ifa coverage`
// reconciles go/internal/ifa's derived expectations against
// specs/ifa-coverage-manifest.v1.yaml and writes the JSON coverage report;
// `ifa expectations [-kind K]` prints the derived expectations themselves as
// JSON. Both subcommands are thin flag/IO wrappers over go/internal/ifa;
// conformance logic lives there, not here.
package main
