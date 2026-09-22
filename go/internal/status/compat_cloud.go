// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the cloud family
// that moved to [cloud] (issue #6775). It carries no behavior change: every
// alias below names the same type the root declared before the nest, so the
// packages importing internal/status keep compiling unchanged. Each entry is
// deleted once its last caller has moved to the leaf; see the
// importer-migration issue #6949. A later cloud move adds a stanza here and
// never creates a second compat file for this family.
//
// Stanzas:
//   - aws_cloud.go (scan status)
//   - aws_freshness.go (vulnerability-scan freshness)

import "github.com/eshu-hq/eshu/go/internal/status/cloud"

// Stanza: aws_cloud.go.

// AWSCloudScanStatus reports one AWS cloud scan's outcome.
// See [cloud.AWSScanStatus].
type AWSCloudScanStatus = cloud.AWSScanStatus

// Stanza: aws_freshness.go.

// AWSFreshnessSnapshot reports AWS cloud-scan freshness backlog.
// See [cloud.AWSFreshnessSnapshot].
type AWSFreshnessSnapshot = cloud.AWSFreshnessSnapshot
