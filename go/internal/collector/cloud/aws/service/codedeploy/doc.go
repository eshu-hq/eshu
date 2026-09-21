// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codedeploy maps AWS CodeDeploy metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level normalization only. It never calls the AWS
// SDK directly, never reads or persists appspec.yml lifecycle-hook bodies, and
// never persists raw on-premises instance tag values. SDK adapters supply
// scanner-owned records with on-premises tag values already redacted, and
// Scanner emits aws_resource facts for applications, deployment groups,
// deployment configs, and recent deployments plus aws_relationship facts for
// the deployment-group edges CodeDeploy reports directly. Scan fails closed
// when the redaction key is zero so PII-shaped tag values cannot leak.
package codedeploy
