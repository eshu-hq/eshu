// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// RuntimeValue is one source-positioned, parser-private version declaration.
type RuntimeValue struct {
	SourceKind string
	Runtime    string
	Value      string
	Line       int
}

// RuntimeEvidence is bounded parser output tied to the exact file revision.
// Only accepted version strings survive; callers must remove it before facts persist.
type RuntimeEvidence struct {
	Values     []RuntimeValue
	Rejected   int
	ByteSize   int
	TokenCount int
	State      string
	Digest     string
}

// RuntimeDigest binds a parser result to the raw bytes, including line endings.
func RuntimeDigest(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

// NewRuntimeEvidence starts an empty, digest-bound result.
func NewRuntimeEvidence(source []byte) RuntimeEvidence {
	state := "parsed_empty"
	if len(source) > 1<<20 {
		state = "over_cap"
	}
	return RuntimeEvidence{State: state, ByteSize: len(source), Digest: RuntimeDigest(source)}
}

// Add keeps a short, safe version source value under the token cap.
func (e *RuntimeEvidence) Add(kind, runtime, raw string, line int) {
	e.TokenCount++
	if e.State == "over_cap" || e.State == "over_tokens" || e.State == "unavailable" {
		return
	}
	if e.TokenCount > RuntimeVersionTokenCap {
		e.Values = nil
		e.State = "over_tokens"
		return
	}
	value := strings.TrimSpace(raw)
	if line < 1 || !SafeVersionSpec(value) {
		e.Rejected++
		return
	}
	e.Values = append(e.Values, RuntimeValue{SourceKind: kind, Runtime: runtime, Value: strings.Clone(value), Line: line})
	e.State = "parsed_values"
}
