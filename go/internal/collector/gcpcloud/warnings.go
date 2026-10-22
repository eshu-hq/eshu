// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"

// validWarningKinds and validOutcomes bound the warning-kind and per-shard
// outcome strings the validators below accept; the enums themselves live in
// the catalog leaf (#6696 slice L). Each value is a low-cardinality enum safe
// for telemetry labels and durable status keys, mirroring the multi-cloud
// contract per-path evidence states.

var validWarningKinds = map[string]struct{}{
	catalog.WarningKindPartialPermission: {},
	catalog.WarningKindUnsupported:       {},
	catalog.WarningKindQuota:             {},
	catalog.WarningKindUnavailable:       {},
	catalog.WarningKindStale:             {},
	catalog.WarningKindRedaction:         {},
	catalog.WarningKindPageTokenExpired:  {},
}

var validOutcomes = map[string]struct{}{
	catalog.OutcomePartial:     {},
	catalog.OutcomeUnsupported: {},
	catalog.OutcomeUnavailable: {},
	catalog.OutcomeStale:       {},
}

// ValidWarningKind reports whether kind is one of the bounded warning kinds.
func ValidWarningKind(kind string) bool {
	_, ok := validWarningKinds[kind]
	return ok
}

// ValidOutcome reports whether outcome is one of the bounded source-state
// outcomes.
func ValidOutcome(outcome string) bool {
	_, ok := validOutcomes[outcome]
	return ok
}
