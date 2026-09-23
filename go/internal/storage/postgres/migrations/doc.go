// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package migrations owns the embedded Postgres bootstrap SQL files and the
// ordered Definition list root's postgres package applies.
//
// It carries the //go:embed *.sql directive so this directory stays the
// single source of truth for both the embedded bytes and the ordering logic
// (#6693 decision D2). Root's schema.go keeps postgres.Definition and
// postgres.BootstrapDefinitions as a type alias and thin forwarder so every
// external caller keeps compiling unchanged.
package migrations
