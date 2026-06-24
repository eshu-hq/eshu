// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"sort"
	"strings"
	"time"
)

// SemanticExtractionQueueSnapshot captures aggregate semantic queue lifecycle
// state without raw prompts, responses, source identifiers, or chunk hashes.
type SemanticExtractionQueueSnapshot struct {
	Total                 int
	Pending               int
	Claimed               int
	Retrying              int
	Succeeded             int
	DeadLetter            int
	Skipped               int
	NoProvider            int
	PolicyDenied          int
	BudgetExhausted       int
	Unsafe                int
	ProviderUnavailable   int
	Unchanged             int
	Stale                 int
	StatusCounts          []NamedCount
	SourceClassCounts     []NamedCount
	FailureClassCounts    []NamedCount
	ProviderProfileCounts []SemanticExtractionProviderProfileQueueCount
	PolicyDecisionCounts  []SemanticExtractionDecisionCount
	GuardDecisionCounts   []SemanticExtractionDecisionCount
	UpdatedAt             time.Time
}

// SemanticExtractionProviderProfileQueueCount is a bounded queue aggregate for
// one configured provider profile.
type SemanticExtractionProviderProfileQueueCount struct {
	ProviderKind         string
	ProviderProfileID    string
	ProviderProfileClass string
	Count                int
}

// SemanticExtractionDecisionCount aggregates policy or guard decisions by
// bounded state and reason values.
type SemanticExtractionDecisionCount struct {
	State  string
	Reason string
	Count  int
}

// SemanticExtractionBudgetSnapshot captures redacted semantic budget totals.
type SemanticExtractionBudgetSnapshot struct {
	EstimatedInputTokens  int64
	EstimatedOutputTokens int64
	EstimatedCostMicros   int64
	ActualInputTokens     int64
	ActualOutputTokens    int64
	ActualCostMicros      int64
	RemainingTokens       int64
	RemainingCostMicros   int64
	Exhausted             int
	DecisionCounts        []SemanticExtractionBudgetDecisionCount
}

// SemanticExtractionBudgetDecisionCount aggregates semantic budget decisions.
type SemanticExtractionBudgetDecisionCount struct {
	State      string
	Reason     string
	BudgetUnit string
	Count      int
}

// SemanticExtractionAuditSnapshot captures audit-safe enablement and egress
// classes without principals, source IDs, prompts, or provider responses.
type SemanticExtractionAuditSnapshot struct {
	ActorClassCounts []NamedCount
	ACLStateCounts   []NamedCount
	LastProcessedAt  time.Time
}

func normalizeSemanticExtractionQueueSnapshot(snapshot SemanticExtractionQueueSnapshot) SemanticExtractionQueueSnapshot {
	out := snapshot
	out.StatusCounts = normalizeNamedCounts(snapshot.StatusCounts)
	out.SourceClassCounts = normalizeNamedCounts(snapshot.SourceClassCounts)
	out.FailureClassCounts = normalizeNamedCounts(snapshot.FailureClassCounts)
	out.ProviderProfileCounts = normalizeProviderProfileQueueCounts(snapshot.ProviderProfileCounts)
	out.PolicyDecisionCounts = normalizeDecisionCounts(snapshot.PolicyDecisionCounts)
	out.GuardDecisionCounts = normalizeDecisionCounts(snapshot.GuardDecisionCounts)
	out.UpdatedAt = snapshot.UpdatedAt.UTC()
	out.Total = nonNegativeInt(out.Total)
	out.Pending = nonNegativeInt(out.Pending)
	out.Claimed = nonNegativeInt(out.Claimed)
	out.Retrying = nonNegativeInt(out.Retrying)
	out.Succeeded = nonNegativeInt(out.Succeeded)
	out.DeadLetter = nonNegativeInt(out.DeadLetter)
	out.Skipped = nonNegativeInt(out.Skipped)
	out.NoProvider = nonNegativeInt(out.NoProvider)
	out.PolicyDenied = nonNegativeInt(out.PolicyDenied)
	out.BudgetExhausted = nonNegativeInt(out.BudgetExhausted)
	out.Unsafe = nonNegativeInt(out.Unsafe)
	out.ProviderUnavailable = nonNegativeInt(out.ProviderUnavailable)
	out.Unchanged = nonNegativeInt(out.Unchanged)
	out.Stale = nonNegativeInt(out.Stale)
	return out
}

func normalizeSemanticExtractionBudgetSnapshot(snapshot SemanticExtractionBudgetSnapshot) SemanticExtractionBudgetSnapshot {
	out := snapshot
	out.DecisionCounts = normalizeBudgetDecisionCounts(snapshot.DecisionCounts)
	out.EstimatedInputTokens = nonNegativeInt64(out.EstimatedInputTokens)
	out.EstimatedOutputTokens = nonNegativeInt64(out.EstimatedOutputTokens)
	out.EstimatedCostMicros = nonNegativeInt64(out.EstimatedCostMicros)
	out.ActualInputTokens = nonNegativeInt64(out.ActualInputTokens)
	out.ActualOutputTokens = nonNegativeInt64(out.ActualOutputTokens)
	out.ActualCostMicros = nonNegativeInt64(out.ActualCostMicros)
	out.RemainingTokens = nonNegativeInt64(out.RemainingTokens)
	out.RemainingCostMicros = nonNegativeInt64(out.RemainingCostMicros)
	out.Exhausted = nonNegativeInt(out.Exhausted)
	return out
}

func normalizeSemanticExtractionAuditSnapshot(snapshot SemanticExtractionAuditSnapshot) SemanticExtractionAuditSnapshot {
	return SemanticExtractionAuditSnapshot{
		ActorClassCounts: normalizeNamedCounts(snapshot.ActorClassCounts),
		ACLStateCounts:   normalizeNamedCounts(snapshot.ACLStateCounts),
		LastProcessedAt:  snapshot.LastProcessedAt.UTC(),
	}
}

func semanticExtractionQueueHasValues(snapshot SemanticExtractionQueueSnapshot) bool {
	return snapshot.Total > 0 || len(snapshot.StatusCounts) > 0 ||
		len(snapshot.ProviderProfileCounts) > 0
}

func semanticExtractionBudgetHasValues(snapshot SemanticExtractionBudgetSnapshot) bool {
	return snapshot.EstimatedInputTokens > 0 || snapshot.EstimatedOutputTokens > 0 ||
		snapshot.EstimatedCostMicros > 0 || snapshot.ActualInputTokens > 0 ||
		snapshot.ActualOutputTokens > 0 || snapshot.ActualCostMicros > 0 ||
		snapshot.RemainingTokens > 0 || snapshot.RemainingCostMicros > 0 ||
		snapshot.Exhausted > 0 || len(snapshot.DecisionCounts) > 0
}

func semanticExtractionAuditHasValues(snapshot SemanticExtractionAuditSnapshot) bool {
	return len(snapshot.ActorClassCounts) > 0 || len(snapshot.ACLStateCounts) > 0 ||
		!snapshot.LastProcessedAt.IsZero()
}

func normalizeNamedCounts(rows []NamedCount) []NamedCount {
	if len(rows) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" || row.Count <= 0 {
			continue
		}
		counts[name] += row.Count
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]NamedCount, 0, len(names))
	for _, name := range names {
		out = append(out, NamedCount{Name: name, Count: counts[name]})
	}
	return out
}

func normalizeProviderProfileQueueCounts(rows []SemanticExtractionProviderProfileQueueCount) []SemanticExtractionProviderProfileQueueCount {
	type key struct {
		providerKind         string
		providerProfileID    string
		providerProfileClass string
	}
	counts := map[key]int{}
	for _, row := range rows {
		k := key{
			providerKind:         strings.TrimSpace(row.ProviderKind),
			providerProfileID:    strings.TrimSpace(row.ProviderProfileID),
			providerProfileClass: strings.TrimSpace(row.ProviderProfileClass),
		}
		if k.providerKind == "" && k.providerProfileID == "" && k.providerProfileClass == "" {
			continue
		}
		if row.Count > 0 {
			counts[k] += row.Count
		}
	}
	out := make([]SemanticExtractionProviderProfileQueueCount, 0, len(counts))
	for k, count := range counts {
		out = append(out, SemanticExtractionProviderProfileQueueCount{
			ProviderKind:         k.providerKind,
			ProviderProfileID:    k.providerProfileID,
			ProviderProfileClass: k.providerProfileClass,
			Count:                count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].ProviderProfileID == out[j].ProviderProfileID {
			return out[i].ProviderKind < out[j].ProviderKind
		}
		return out[i].ProviderProfileID < out[j].ProviderProfileID
	})
	return out
}

func normalizeDecisionCounts(rows []SemanticExtractionDecisionCount) []SemanticExtractionDecisionCount {
	type key struct{ state, reason string }
	counts := map[key]int{}
	for _, row := range rows {
		k := key{state: strings.TrimSpace(row.State), reason: strings.TrimSpace(row.Reason)}
		if k.state == "" && k.reason == "" {
			continue
		}
		if row.Count > 0 {
			counts[k] += row.Count
		}
	}
	out := make([]SemanticExtractionDecisionCount, 0, len(counts))
	for k, count := range counts {
		out = append(out, SemanticExtractionDecisionCount{State: k.state, Reason: k.reason, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State == out[j].State {
			return out[i].Reason < out[j].Reason
		}
		return out[i].State < out[j].State
	})
	return out
}

func normalizeBudgetDecisionCounts(rows []SemanticExtractionBudgetDecisionCount) []SemanticExtractionBudgetDecisionCount {
	type key struct{ state, reason, unit string }
	counts := map[key]int{}
	for _, row := range rows {
		k := key{
			state:  strings.TrimSpace(row.State),
			reason: strings.TrimSpace(row.Reason),
			unit:   strings.TrimSpace(row.BudgetUnit),
		}
		if k.state == "" && k.reason == "" && k.unit == "" {
			continue
		}
		if row.Count > 0 {
			counts[k] += row.Count
		}
	}
	out := make([]SemanticExtractionBudgetDecisionCount, 0, len(counts))
	for k, count := range counts {
		out = append(out, SemanticExtractionBudgetDecisionCount{
			State:      k.state,
			Reason:     k.reason,
			BudgetUnit: k.unit,
			Count:      count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State == out[j].State {
			return out[i].Reason < out[j].Reason
		}
		return out[i].State < out[j].State
	})
	return out
}

func nonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func nonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
