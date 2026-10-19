// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "github.com/eshu-hq/eshu/go/internal/scope"

// ClaimQuota is the enforced claim-grant budget for one collector kind
// (#8471). Only the generations-per-hour bound is enforced today; the
// manifest's facts-per-generation bound has no per-generation emitted-fact
// counter yet, so it stays declared-but-unenforced (follow-up on #8471).
// A non-positive limit is a configuration error, never "unlimited": an
// unlimited component is expressed by a lookup that reports no quota.
type ClaimQuota struct {
	MaxGenerationsPerHour int
}

// QuotaLookup resolves the enforced quota for a collector kind. The second
// return reports whether a quota is configured at all: false means the
// component claims unthrottled. The coordinator builds the lookup from the
// component manifest declarations with the operator override applied per
// field (component.Quota.Resolve); this package only consumes the result
// so neither package imports the other.
type QuotaLookup func(collectorKind scope.CollectorKind) (ClaimQuota, bool)
