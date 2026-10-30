// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

// This file exists to populate the AWS scanner registry from init() for
// every freshness test binary. The freshness package calls
// awsruntime.SupportsServiceKind to validate incoming freshness targets,
// and the registry is empty until at least one runtimebind package init
// runs. Blank-importing the canonical bindings aggregator keeps every
// freshness test honest about the scanner set the runtime will see.
//
// Production runtimes (workflow-coordinator, webhook-listener,
// collector-aws-cloud) get the same registration by blank-importing
// bindings from their main packages.
import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)
