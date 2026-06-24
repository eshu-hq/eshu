// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shield

import "context"

// Client is the AWS Shield Advanced read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned metadata
// records. The adapter must never call a Shield mutation API and must never
// read or persist billing detail beyond the subscription state and auto-renew
// summary.
type Client interface {
	// ListProtections returns every Shield Advanced protection visible to the
	// claimed account, with the protection ARN, name, and protected resource
	// ARN needed to project the protection-to-protected-resource edge.
	ListProtections(context.Context) ([]Protection, error)
	// DescribeSubscription returns the per-account Shield Advanced subscription
	// summary, or nil when the account has no active subscription. Only the
	// state and auto-renew fields are populated; no billing detail is read.
	DescribeSubscription(context.Context) (*Subscription, error)
}

// Protection is the scanner-owned representation of one Shield Advanced
// protection. It carries identity and the protected resource ARN only; the
// protected ARN comes from the API already partition-correct and is used
// directly as the relationship join key.
type Protection struct {
	// ARN is the protection's own ARN (the protection resource_id).
	ARN string
	// ID is the unique protection id reported by AWS.
	ID string
	// Name is the operator-assigned protection name.
	Name string
	// ResourceARN is the ARN of the protected AWS resource. Its service segment
	// selects the protection-to-protected-resource edge target type.
	ResourceARN string
}

// Subscription is the scanner-owned representation of the per-account Shield
// Advanced subscription summary. It carries the state and auto-renew flag only.
// Subscription limits, time commitment, end time, and any other billing detail
// are intentionally outside this contract.
type Subscription struct {
	// ARN is the subscription ARN reported by AWS, when present.
	ARN string
	// State is the subscription state reported by GetSubscriptionState
	// (ACTIVE or INACTIVE).
	State string
	// AutoRenew is the subscription auto-renew flag (ENABLED or DISABLED).
	AutoRenew string
}
