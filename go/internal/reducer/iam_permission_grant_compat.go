// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"

// This file is the reducer root's compatibility surface for the IAM resource
// type vocabulary that moved to [iampolicy] (issue #6061). The permission
// statement, principal-grant, target-resolution, and matcher entries this file
// used to forward lost their last root caller when the iam_escalation family
// moved into internal/reducer/iamescalation (#6061); those entries were
// deleted rather than kept as dead forwarders. Root files outside the IAM
// families still name these four IAM resource_type literals directly, so they
// remain.

const (
	// iamResourceTypeRole is [iampolicy.ResourceTypeRole].
	iamResourceTypeRole = iampolicy.ResourceTypeRole
	// iamResourceTypeUser is [iampolicy.ResourceTypeUser].
	iamResourceTypeUser = iampolicy.ResourceTypeUser
	// iamResourceTypePolicy is [iampolicy.ResourceTypePolicy].
	iamResourceTypePolicy = iampolicy.ResourceTypePolicy
	// iamResourceTypeGroup is [iampolicy.ResourceTypeGroup].
	iamResourceTypeGroup = iampolicy.ResourceTypeGroup
)
