// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsfreshnessstore

// This file populates the AWS scanner registry from init() for this
// package's test binary. store_test.go builds freshness.StoredTrigger
// values via freshness.NewStoredTrigger, which validates service_kind
// through awsruntime.SupportsServiceKind. The registry is empty until at
// least one runtimebind package init runs. Root's own copy
// (aws_bindings_test.go) stays there for tests remaining in that package;
// this package needs its own because Go test binaries are built and
// initialized per package (#6693).
//
// Production runtimes (workflow-coordinator, webhook-listener,
// collector-aws-cloud) get the same registration by blank-importing
// bindings from their main packages.
import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)
