// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serverless holds the GCP serverless typed-depth extractors: one file
// per CAI asset type for App Engine, Cloud Functions, Cloud Scheduler, Cloud
// Tasks, Composer, Eventarc, Cloud Run, and Workflows.
//
// Each asset type registers its extractor from an init function against the
// extractor leaf registry; the collector binary imports this package for its
// side effects (see TestServerlessExtractorsRegistered in
// cmd/collector-gcp-cloud). Extractors receive the raw CAI resource.data
// transiently and return only bounded, redaction-safe attributes, correlation
// anchors, and typed relationships. They never persist the data blob and never
// return secrets, data-plane content, raw policy JSON, IPs, or response bodies.
//
// The package depends on the extractor leaf and the access/posture package
// only. It must never import the parent gcpcloud package: both this package
// and the parent depend on the leaf, so a parent edge would be an import cycle.
package serverless
