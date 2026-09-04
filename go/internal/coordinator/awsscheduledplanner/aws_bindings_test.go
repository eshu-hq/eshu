// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsscheduledplanner

// This file exists to populate the AWS scanner registry from init() for every
// awsscheduledplanner test binary. The planner reaches
// awsfreshnessplanner.ParseTargetScopes, which calls
// awsruntime.SupportsServiceKind to validate target-scope service_kind values,
// and the registry is empty until at least one runtimebind package init runs.
// Without this the scheduled-planner tests fail with an "unsupported AWS
// service_kind" error that has nothing to do with the planner: the root
// package used to get the registration transitively from its other tests, and
// a standalone child test binary does not.
//
// Production runtimes get the same registration by blank-importing bindings
// from cmd/workflow-coordinator/main.go. The sibling awsfreshnessplanner
// package carries an identical file for the same reason.
import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)
