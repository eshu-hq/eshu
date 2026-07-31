// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ociruntime scans configured OCI registry repositories and returns
// collected generations for the shared collector boundary.
//
// Source implements collector.Source for configured-target polling, while
// ClaimedSource implements collector.ClaimedSource for workflow-owned claims.
// Both call a provider-supplied RegistryClient for ping, bounded tag list,
// manifest, and referrer reads. The tag-list contract carries explicit
// completeness so a short paginated response cannot become authoritative
// absence, including when the bounded partial window is empty. The runtime
// parses OCI and Docker-compatible manifest bytes, preserves digest identity,
// emits warning facts for incomplete collection and non-fatal capability gaps
// such as missing referrers, and records bounded OCI registry metrics and spans
// without placing registry hosts, repository names, tags, or digests in metric
// labels.
package ociruntime
