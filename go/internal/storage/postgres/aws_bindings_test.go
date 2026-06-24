// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file populates the AWS scanner registry from init() for the
// storage/postgres test binary. The aws_freshness_store tests build
// freshness.StoredTrigger values via freshness.NewStoredTrigger, which
// validates service_kind through awsruntime.SupportsServiceKind. The
// registry is empty until at least one runtimebind package init runs.
//
// Production runtimes (workflow-coordinator, webhook-listener,
// collector-aws-cloud) get the same registration by blank-importing
// bindings from their main packages.
import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)
