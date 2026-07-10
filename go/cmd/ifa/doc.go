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
//
// `ifa graph-dump [-out FILE] [-digest]` (issue #4396, parent epic #4389) is
// the graph-truth half of Ifá's P3 determinism matrix. It opens a live Bolt
// connection to the configured graph backend via graphdump_reader.go's
// boltGraphReader (a go/internal/ifa/graphdump.Reader implementation), reads
// every node and relationship, and writes graphdump.Canonicalize's stable
// canonical byte form (or, with -digest, its sha256 hex digest) to -out or
// stdout. It is read-only: it applies no schema DDL and performs no write.
//
// `ifa synth-cassette -seed N [-projects K] [-resources R] -out FILE` (issue
// #4396 slice 6b) wraps go/internal/synth/gcp.GenerateMultiScope, generating a
// deterministic, seeded cassette with K independent GCP project scopes and
// writing its canonical bytes to -out. It exists because a single-scope
// cassette gives concurrentreplay.Driver exactly one work unit for ANY
// -workers count, making `ifa drive -workers N` inert; a multi-scope cassette
// gives the driver K genuinely independent work units to fan out across. It
// performs no I/O beyond writing -out.
package main
