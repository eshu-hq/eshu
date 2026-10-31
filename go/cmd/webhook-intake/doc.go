// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-webhook-intake binary, core's dedicated
// authenticated versioned webhook intake host (#8876).
//
// The extracted webhook satellite verifies provider signatures, normalizes
// payloads, and submits normalized triggers here over HTTP; this host
// authenticates the satellite (Bearer), validates the v1 wire form, bounds
// the body, and persists durable triggers in Postgres through the store
// interfaces in go/internal/webhook/intake. It serves no provider-facing
// routes, emits no facts, and performs no graph writes.
//
// The intake host is the canonical owner of EnsureSchema for the five
// trigger tables (webhook_refresh_triggers, cicd_actions_deliveries,
// incident_freshness_triggers, aws_freshness_triggers,
// gcp_freshness_triggers). The workflow coordinator also runs EnsureSchema
// for the four freshness/Actions families it already owns; that overlap is
// convergent idempotent DDL, not a second writer — this host is the only
// intake writer path on the extracted topology.
package main
