// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultPositiveRetractStringSliceBatchSize bounds positive list-seeded
// canonical retract statements before they reach graph backends with strict
// transaction request limits.
const DefaultPositiveRetractStringSliceBatchSize = 25

// ChunkPositiveStringSliceRetractStatement splits a canonical retract statement
// when exactly one string-slice parameter is used by a positive `IN $param`
// predicate. Negative `NOT IN` cleanup statements must stay intact because
// splitting their keep-list would make each chunk delete valid current nodes
// that happen to live in another chunk.
func ChunkPositiveStringSliceRetractStatement(stmt Statement, batchSize int) []Statement {
	paramName, values, ok := positiveStringSliceRetractParam(stmt)
	if !ok || len(values) == 0 || batchSize <= 0 || len(values) <= batchSize {
		return []Statement{stmt}
	}

	chunks := make([]Statement, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		chunk := stmt
		chunk.Parameters = cloneParameters(stmt.Parameters)
		chunk.Parameters[paramName] = append([]string(nil), values[start:end]...)
		chunks = append(chunks, chunk)
	}
	return chunks
}

func positiveStringSliceRetractParam(stmt Statement) (string, []string, bool) {
	if stmt.Operation != OperationCanonicalRetract || stmt.Parameters == nil {
		return "", nil, false
	}
	cypher := stmt.Cypher
	matches := make([]string, 0, 1)
	keys := sortedParameterKeys(stmt.Parameters)
	for _, key := range keys {
		values, ok := stmt.Parameters[key].([]string)
		if !ok || len(values) == 0 {
			continue
		}
		if !cypherHasPositiveInPredicate(cypher, key) {
			continue
		}
		matches = append(matches, key)
	}
	if len(matches) != 1 {
		return "", nil, false
	}
	values := stmt.Parameters[matches[0]].([]string)
	return matches[0], values, true
}

func cypherHasPositiveInPredicate(cypher string, paramName string) bool {
	token := fmt.Sprintf("IN $%s", paramName)
	if !strings.Contains(cypher, token) {
		return false
	}
	negativeToken := fmt.Sprintf("NOT (f.path IN $%s", paramName)
	if strings.Contains(cypher, negativeToken) {
		return false
	}
	negativeToken = fmt.Sprintf("NOT (d.path IN $%s", paramName)
	return !strings.Contains(cypher, negativeToken)
}

func sortedParameterKeys(params map[string]any) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneParameters(params map[string]any) map[string]any {
	clone := make(map[string]any, len(params))
	for key, value := range params {
		clone[key] = value
	}
	return clone
}
