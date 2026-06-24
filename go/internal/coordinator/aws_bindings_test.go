// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

// This file exists to populate the AWS scanner registry from init() for
// every coordinator test binary. Coordinator code calls
// awsruntime.SupportsServiceKind to validate target-scope service_kind
// values, and the registry is empty until at least one runtimebind
// package init runs. Blank-importing the canonical bindings aggregator
// keeps every coordinator test honest about the scanner set the
// workflow-coordinator binary will see at runtime.
//
// Production runtimes get the same registration by blank-importing
// bindings from cmd/workflow-coordinator/main.go.
import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)
