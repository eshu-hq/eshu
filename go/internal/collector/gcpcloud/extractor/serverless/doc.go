// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package serverless holds the GCP serverless typed-depth extractors: one file
// per CAI asset type for App Engine, Cloud Functions, Cloud Scheduler, Cloud
// Tasks, Composer, Eventarc, Cloud Run, and Workflows.
//
// Each asset type registers its extractor from an init function against the
// extractor leaf registry. No in-tree binary imports this package anymore:
// the in-tree command was deleted on the #8856 extraction, and the external
// satellite cannot import a go/internal package, so it carries its own copy
// (gcpcloud/extractor/serverless in eshu-hq/eshu-collector-gcp-cloud, wired
// by its command). Registration in this tree is pinned by
// TestServerlessExtractorsRegistered in the gcpcloud root package, which
// holds the side-effect import and fails if it is ever dropped. Extractors
// receive the raw CAI resource.data
// transiently and return only bounded, redaction-safe attributes, correlation
// anchors, and typed relationships. They never persist the data blob and never
// return secrets, data-plane content, raw policy JSON, IPs, or response bodies.
//
// The package depends on the extractor leaf and the access/posture package
// only. It must never import the parent gcpcloud package: both this package
// and the parent depend on the leaf, so a parent edge would be an import cycle.
package serverless
