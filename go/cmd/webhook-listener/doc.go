// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-webhook-listener binary, the public webhook intake
// runtime for GitHub, GitLab, Bitbucket, AWS freshness triggers, GCP freshness
// triggers, PagerDuty, and Jira.
//
// The runtime verifies provider authentication, normalizes webhook payloads,
// persists trigger decisions in Postgres, and exposes the shared Eshu admin
// surface. Provider delivery identity is required before normalization; GitHub
// ping deliveries are accepted as verified no-op handshakes, GitLab delivery
// identity prefers Idempotency-Key so provider retries dedupe against the same
// durable trigger, and Bitbucket delivery identity prefers X-Request-UUID with
// X-Hub-Signature verification. The runtime does not mount the repository
// workspace, connect to the graph backend, or mark webhook metadata as graph
// truth. AWS EventBridge and AWS Config deliveries are normalized into
// service-tuple wake-up triggers and never write graph truth directly. GCP
// Cloud Asset Inventory Pub/Sub push deliveries are normalized into
// parent-scope/asset-type/location wake-up triggers, drop the raw asset data
// blob, and never write graph truth directly; the route accepts two
// independent, fail-closed auth paths — the shared X-Eshu-GCP-Freshness-Token
// (or Authorization: Bearer) and a verified Pub/Sub push OIDC token (Google
// signature, audience, and allowlisted service-account email/email_verified
// claims) — either sufficient, and stays unmounted until at least one is
// configured. PagerDuty and Jira deliveries are normalized into
// scoped incident freshness wake-ups and never emit incident, change,
// work-item, deployment, code, or PR facts directly. Jira intake admits only
// issue created, updated, and deleted events as collector wake-ups. Request
// body handling returns 413 only when MaxRequestBodyBytes is exceeded; other
// body read failures are rejected as bad requests. Provider intake records
// bounded structured logs plus OTEL counters, histograms, and spans through
// telemetry.Instruments and telemetry.SpanWebhookHandle/
// telemetry.SpanWebhookStore. Provider, event kind, decision, status, outcome,
// reason, AWS freshness kind, AWS freshness action, GCP freshness kind, GCP
// freshness action, and GCP freshness auth_path (shared_token/oidc/none) are
// bounded metric labels; repository, delivery identity, incident IDs, issue
// keys, resource names, ARNs, and OIDC token/claim values stay out of
// metrics.
package main
