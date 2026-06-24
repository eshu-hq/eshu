// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Amazon Detective control-plane calls
// into the metadata-only records the detective scanner consumes.
//
// The adapter's internal apiClient interface is intentionally limited to the
// three read-only list operations the scanner needs: ListGraphs, ListMembers,
// and ListTagsForResource. It exposes no investigation, indicator, or
// finding-datasource read and no mutation API. A reflection gate in
// client_test.go fails the build if any forbidden operation becomes reachable.
//
// Member contact emails (MemberDetail.EmailAddress) are personal data and are
// never read into the scanner type, and the deprecated usage-volume and
// graph-utilization fields are dropped. Behavior graph ARNs are passed through
// unchanged, so synthesized identities inherit the graph's partition rather than
// hardcoding one.
package awssdk
