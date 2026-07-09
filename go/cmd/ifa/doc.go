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
//
// `ifa drive -cassette <path> [-workers N] [-postgres-dsn]` (design doc 4102,
// issue #4395, parent epic #4389) drives
// go/internal/replay/concurrentreplay.Driver over the cassette at -cassette,
// committing each collected generation through a Postgres IngestionStore.
// -workers defaults to 1, the mode issue #4395's acceptance clause names
// explicitly. Draining the fact_work_items rows it enqueues to a terminal
// state requires cmd/projector and cmd/reducer running separately against the
// same database, exactly as scripts/verify-ifa-replay-drive.sh orchestrates.
package main
