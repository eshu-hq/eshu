// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entitymap builds the bounded code-to-cloud entity neighborhood for
// `eshu map`: posting the request to the API's entity-map route, classifying
// the outcome, and rendering the canonical envelope as either a grouped text
// summary or JSON.
//
// The package name follows the command with the Go keyword removed -- `map`
// cannot be a package name -- and matches the entity_map capability and the
// /api/v0/impact/entity-map route the command reads.
//
// The package reads no cobra flags, resolves no Eshu config or credential from
// the process environment, opens no file, and never calls os.Exit. It talks to
// the API only through the EnvelopePoster interface a caller supplies, and
// writes only to the io.Writer a caller supplies. go/cmd/eshu/map.go is the
// thin cobra wrapper that resolves flags (--from, --type, --repo, --env,
// --relationship, --depth, --limit, --json), builds the API client, and maps a
// Failure to the CLI's exit-code contract.
//
// Resolve fixes the order the CLI checks an entity map in: transport error,
// envelope error, index freshness, then resolution status. It classifies the
// outcome into a FailureKind and deliberately stops there, because the exit
// code a kind produces belongs with the rest of the CLI's exit-code table in
// go/cmd/eshu, not here.
package entitymap
