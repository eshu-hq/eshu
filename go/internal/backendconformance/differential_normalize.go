// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// runScopedGenerationKey is the projector's per-materialization lineage
// stamp. Two runs over the same corpus stamp different generations on
// identical content, so the key identifies the producing run, not the
// content. Excluding it from fingerprints and digests is what makes
// cross-run comparison possible at all (#6782); content drift still
// changes every other key.
const runScopedGenerationKey = "generation_id"

// resolvedIDKey carries relationship lineage ids. Some producers embed the
// run's generation stamp as the middle segment
// (deployable-unit-correlation:<generation>:<correlation key>); the
// deterministic producers (code-imports:<consumer>-><owner>,
// package-consumption:<consumer>-><owner>) carry no stamp. Only the stamped
// shape normalizes.
const resolvedIDKey = "resolved_id"

// generationMiddlePattern matches a resolved_id whose middle segment is a
// 64-hex generation stamp. Bare content hashes never match: the rule prefix
// and the trailing correlation key are required, and the deterministic
// resolved_id shapes carry neither.
var generationMiddlePattern = regexp.MustCompile(`^([^:]+):[0-9a-f]{64}(:.*)$`)

// lineageDigestPatterns match full run-scoped digests that cannot invert to
// content: the cross-generation resolved_ edge ids (sha1 over the generation
// plus the endpoints) and the generation-derived evidence-artifact keys.
// They normalize in digest ROWS only, never in fingerprints: normalizing a
// sweep key would pair different orphans, while normalizing an observed cell
// only blinds the lineage stamp and leaves every other cell verifying.
var lineageDigestPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^resolved_[0-9a-f]+$`),
	regexp.MustCompile(`^evidence-artifact:[0-9a-f]+$`),
}

// lineageDigestToken is the digest-row stand-in for a run-scoped lineage digest.
const lineageDigestToken = "run-scoped-lineage"

// artifactIDKey carries derivation-stamped artifact ids: sha1 over the run's
// resolved id plus the derivation inputs. Two batches carrying the same
// logical writes under different run stamps pair on every other field, so
// the stamp cell normalizes wherever the key names it — in fingerprints and
// digest rows alike. Bare sweep keys carry no artifact_id key and never
// normalize.
const artifactIDKey = "artifact_id"

// artifactIDPattern matches a derivation-stamped artifact id cell.
var artifactIDPattern = regexp.MustCompile(`^evidence-artifact:[0-9a-f]+$`)

// normalizeComparisonValue returns value with every run-scoped lineage key
// removed, recursing into nested maps and slices. The JSON round-trip first
// normalizes driver value types (map[string]string to map[string]any,
// int64 to float64) so stripping sees every nesting level uniformly; the
// encoding is byte-identical to a direct marshal for values without
// run-scoped keys. The caller's value is never mutated.
func normalizeComparisonValue(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal comparison value: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("re-decode comparison value: %w", err)
	}
	return stripRunScoped(normalized), nil
}

// stripRunScoped removes runScopedGenerationKey from every map in value.
// A resolved_id embedding the generation stamp as its middle segment
// normalizes to a fixed stamp token, so the same logical correlation pairs
// across runs; deterministic resolved_id shapes pass through untouched.
// Top-level diagnostic metadata ("_"-prefixed) keys are handled separately
// by normalizeComparisonParams to mirror SanitizeStatementParameters.
func stripRunScoped(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == runScopedGenerationKey {
				delete(typed, key)
				continue
			}
			if key == resolvedIDKey {
				if stamped, ok := item.(string); ok {
					typed[key] = generationMiddlePattern.ReplaceAllString(stamped, "$1:run-generation$2")
					continue
				}
			}
			if key == artifactIDKey {
				if stamped, ok := item.(string); ok && artifactIDPattern.MatchString(stamped) {
					typed[key] = lineageDigestToken
					continue
				}
			}
			typed[key] = stripRunScoped(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = stripRunScoped(item)
		}
		return typed
	default:
		return value
	}
}

// canonicalizeGraphValue converts backend-typed graph values to plain maps
// before JSON encoding, so backend-assigned identity never enters a digest:
// node Id/ElementId drop in favor of sorted labels plus properties, and
// relationship endpoint ids drop in favor of type plus properties. The shapes
// mirror the production decoders (internal/query/neo4j.go,
// internal/query/graph/rows): nodes(path) arrives as a driver Node on both
// backends, while relationships(path) arrives as a driver Relationship on
// Neo4j and as a {type, properties} map on NornicDB. Ordinary maps pass
// through, except the exact NornicDB relationship/node map shapes.
func canonicalizeGraphValue(value any) any {
	switch typed := value.(type) {
	case neo4jdriver.Node:
		return map[string]any{
			"labels": canonicalizeGraphValue(stringSlice(typed.Labels)),
			"props":  canonicalizeGraphValue(typed.Props),
		}
	case neo4jdriver.Relationship:
		return map[string]any{
			"type":  typed.Type,
			"props": canonicalizeGraphValue(typed.Props),
		}
	case neo4jdriver.Path:
		nodes := make([]any, 0, len(typed.Nodes))
		for _, node := range typed.Nodes {
			nodes = append(nodes, canonicalizeGraphValue(node))
		}
		rels := make([]any, 0, len(typed.Relationships))
		for _, rel := range typed.Relationships {
			rels = append(rels, canonicalizeGraphValue(rel))
		}
		return map[string]any{"nodes": nodes, "relationships": rels}
	case map[string]any:
		if relType, props, ok := nornicRelationshipShape(typed); ok {
			return map[string]any{
				"type":  relType,
				"props": canonicalizeGraphValue(props),
			}
		}
		if labels, props, ok := nornicNodeShape(typed); ok {
			return map[string]any{
				"labels": canonicalizeGraphValue(labels),
				"props":  canonicalizeGraphValue(props),
			}
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = canonicalizeGraphValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, canonicalizeGraphValue(item))
		}
		return out
	default:
		return value
	}
}

// nornicRelationshipShape recognizes the NornicDB relationships(path)
// element: exactly {type, properties}. The exact-keys guard keeps an
// ordinary result map with a "type" key from collapsing into a relationship.
func nornicRelationshipShape(value map[string]any) (string, any, bool) {
	if len(value) != 2 {
		return "", nil, false
	}
	relType, ok := value["type"].(string)
	if !ok {
		return "", nil, false
	}
	props, ok := value["properties"].(map[string]any)
	if !ok {
		return "", nil, false
	}
	return relType, props, true
}

// nornicNodeShape recognizes the NornicDB bare-node map fallbacks the
// production decoders already tolerate: a single "properties" map (labels
// unknown), or an explicit labels-plus-properties pair.
func nornicNodeShape(value map[string]any) (any, any, bool) {
	if len(value) == 1 {
		if props, ok := value["properties"].(map[string]any); ok {
			return []any{}, props, true
		}
		return nil, nil, false
	}
	if len(value) != 2 {
		return nil, nil, false
	}
	labels, ok := value["labels"].([]any)
	if !ok {
		return nil, nil, false
	}
	props, ok := value["properties"].(map[string]any)
	if !ok {
		return nil, nil, false
	}
	return labels, props, true
}

// stringSlice converts a string slice to []any for canonical encoding.
func stringSlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// clockKeySuffixes mark wall-clock observation columns. Their values identify
// when a run swept, not what the graph holds, so no cross-run oracle can
// verify them; they normalize to zero in digest rows only, never in
// fingerprints.
var clockKeySuffixes = []string{"_at", "_unix", "_timestamp"}

// isClockKey reports whether key names a wall-clock observation column.
func isClockKey(key string) bool {
	if key == "timestamp" {
		return true
	}
	for _, suffix := range clockKeySuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// canonicalizeDigestValue normalizes one JSON-plain digest row for truth
// comparison. Run-scoped lineage digest cells blind to a fixed token, clock
// columns zero out, and every list sorts by encoding: a backend is free to
// return labels() and collect() in any order, and row order without ORDER BY
// was never truth. Map keys already sort at JSON encoding.
//
// The documented blind spot is stored list-property order: a backend that
// reordered a persisted list would digest identically. No corpus statement
// persists order-sensitive lists as query truth — list projections in the
// corpus are labels() and collect() without ordering guarantees — so the
// trade favors pairing real runs over guarding a shape the corpus never
// exercises.
func canonicalizeDigestValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isClockKey(key) {
				out[key] = 0
				continue
			}
			out[key] = canonicalizeDigestValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, canonicalizeDigestValue(item))
		}
		return sortListByEncoding(out)
	case string:
		for _, pattern := range lineageDigestPatterns {
			if pattern.MatchString(typed) {
				return lineageDigestToken
			}
		}
		return typed
	default:
		return value
	}
}

// sortListByEncoding orders list elements by their canonical JSON encoding.
func sortListByEncoding(out []any) []any {
	slices.SortFunc(out, func(a, b any) int {
		return strings.Compare(canonicalJSON(a), canonicalJSON(b))
	})
	return out
}

// sortElementLists sorts every list inside an UNWIND element view by
// encoding, so the same logical write with evidence in a different order
// keys identically. Unlike [canonicalizeDigestValue] it blinds nothing:
// lineage cells keep distinguishing elements, and only order noise cancels.
func sortElementLists(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = sortElementLists(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sortElementLists(item))
		}
		return sortListByEncoding(out)
	default:
		return value
	}
}

// canonicalJSON encodes value for list ordering. Values here already passed
// the JSON round-trip, so encoding cannot fail; on failure the comparison
// falls back to formatted output rather than failing the digest.
func canonicalJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

// normalizeComparisonParams returns params with top-level diagnostic
// metadata keys (any key prefixed with "_", mirroring
// SanitizeStatementParameters: they never reach either backend's driver)
// and run-scoped lineage keys at every level removed.
func normalizeComparisonParams(params map[string]any) (map[string]any, error) {
	if params == nil {
		return map[string]any{}, nil
	}
	normalized, err := normalizeComparisonValue(params)
	if err != nil {
		return nil, fmt.Errorf("encode differential parameters: %w", err)
	}
	out, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("encode differential parameters: params encode to %T, not an object", normalized)
	}
	for key := range out {
		if strings.HasPrefix(key, "_") {
			delete(out, key)
		}
	}
	return out, nil
}
