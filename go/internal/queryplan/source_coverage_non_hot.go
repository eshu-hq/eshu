// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func validateNonHotDisposition(key string, disposition NonHotDisposition) []string {
	var violations []string
	if len(disposition.SourceDigest) != sha256.Size*2 {
		violations = append(violations, fmt.Sprintf("%s: non_hot requires a SHA-256 source_sha256", key))
	}
	// fan_out_multiplier is scoped to keyed_support bounded_key_batch rows
	// only: it declares the per-key row multiplicity of that one statement
	// shape, so it means nothing on any other class or key_bound (#6590).
	if disposition.FanOutMultiplier != 0 &&
		(disposition.Class != NonHotClassKeyedSupport || disposition.KeyBound != NonHotKeyBoundBatch) {
		violations = append(violations, fmt.Sprintf(
			"%s: fan_out_multiplier only applies to keyed_support bounded_key_batch rows", key,
		))
	}
	switch disposition.Class {
	case NonHotClassKeyedSupport:
		if disposition.KeyBound != NonHotKeyBoundSingle && disposition.KeyBound != NonHotKeyBoundBatch {
			violations = append(violations, fmt.Sprintf("%s: keyed_support requires key_bound", key))
		}
		if disposition.KeyBound == NonHotKeyBoundBatch && disposition.MaxKeys <= 0 {
			violations = append(violations, fmt.Sprintf("%s: bounded_key_batch requires max_keys", key))
		}
		if disposition.MaxResults <= 0 {
			violations = append(violations, fmt.Sprintf("%s: keyed_support requires max_results", key))
		}
		if disposition.KeyBound == NonHotKeyBoundBatch && disposition.FanOutMultiplier != 0 {
			violations = append(violations, validateFanOutMultiplier(key, disposition)...)
		}
	case NonHotClassLabelInventory:
		if strings.TrimSpace(disposition.Label) == "" {
			violations = append(violations, fmt.Sprintf("%s: label_inventory requires label", key))
		}
		if disposition.MaxResults <= 0 {
			violations = append(violations, fmt.Sprintf("%s: label_inventory requires max_results", key))
		}
	case NonHotClassDelegated:
		if disposition.Delegate != "graph_session" && disposition.Delegate != "profiled_callee" {
			violations = append(violations, fmt.Sprintf("%s: delegated requires delegate", key))
		}
	case NonHotClassOperatorQuery:
		if disposition.Policy != "authenticated_read_endpoint" && disposition.Policy != "validated_query_endpoint" {
			violations = append(violations, fmt.Sprintf("%s: operator_query requires policy", key))
		}
	case NonHotClassBackendMetadata:
		if disposition.Operation != "relationship_types" {
			violations = append(violations, fmt.Sprintf("%s: backend_metadata requires operation", key))
		}
	case NonHotClassDegreeBounded:
		if disposition.KeyBound != NonHotKeyBoundSingle {
			violations = append(violations, fmt.Sprintf(
				"%s: degree_bounded requires key_bound %q (got %q); single-anchor one-hop CALLS reads use single_key, batched reads use keyed_support",
				key,
				NonHotKeyBoundSingle,
				disposition.KeyBound,
			))
		}
		violations = append(violations, validateNonHotMaxDegree(key, disposition.Class, disposition.MaxDegree)...)
	case NonHotClassDepthBounded:
		if disposition.KeyBound != NonHotKeyBoundSingle {
			violations = append(violations, fmt.Sprintf(
				"%s: depth_bounded requires key_bound %q (got %q); single-anchor traversals use single_key, batched reads use keyed_support",
				key,
				NonHotKeyBoundSingle,
				disposition.KeyBound,
			))
		}
		violations = append(violations, validateNonHotMaxDegree(key, disposition.Class, disposition.MaxDegree)...)
		if disposition.MaxDepth != nonHotTransitiveMaxDepth {
			violations = append(violations, fmt.Sprintf(
				"%s: depth_bounded requires max_depth == %d (got %d); %d is the handler-enforced clamp ceiling, so a lower bound describes no production path — a tighter production clamp needs a validator update, not a smaller number here",
				key,
				nonHotTransitiveMaxDepth,
				disposition.MaxDepth,
				nonHotTransitiveMaxDepth,
			))
		}
	default:
		violations = append(violations, fmt.Sprintf("%s: unsupported non-hot class %q", key, disposition.Class))
	}
	return violations
}

// validateFanOutMultiplier checks a keyed_support bounded_key_batch row's
// fan_out_multiplier: it must be at least 1, and max_results must equal
// max_keys * fan_out_multiplier exactly -- max_results is derived from the
// declared per-key multiplicity, never picked independently of it (#6590).
func validateFanOutMultiplier(key string, disposition NonHotDisposition) []string {
	if disposition.FanOutMultiplier < 1 {
		return []string{fmt.Sprintf(
			"%s: fan_out_multiplier must be >= 1 (got %d)",
			key,
			disposition.FanOutMultiplier,
		)}
	}
	want := disposition.MaxKeys * disposition.FanOutMultiplier
	if disposition.MaxResults != want {
		return []string{fmt.Sprintf(
			"%s: bounded_key_batch requires max_results == max_keys x fan_out_multiplier (%d x %d = %d, got %d); max_results is derived, not picked",
			key,
			disposition.MaxKeys,
			disposition.FanOutMultiplier,
			want,
			disposition.MaxResults,
		)}
	}
	return nil
}

// validateNonHotMaxDegree floors max_degree at the corpus-measured maximum
// CALLS degree (both directions) so a disposition cannot certify a fan-out
// below observed reality. A max_degree under the floor is either a stale
// measurement (the staged corpus grew — re-measure and raise the floor) or
// a number picked to fit the entry (never the fix).
func validateNonHotMaxDegree(key, class string, maxDegree int) []string {
	if maxDegree < nonHotCorpusMaxCALLSDegree {
		return []string{fmt.Sprintf(
			"%s: %s requires max_degree >= %d (got %d); %d is the maximum CALLS degree measured both directions on the ops-qa reference corpus (#6649)",
			key,
			class,
			nonHotCorpusMaxCALLSDegree,
			maxDegree,
			nonHotCorpusMaxCALLSDegree,
		)}
	}
	return nil
}
