// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DotNetRuntimeValue is one bounded literal SDK or target-framework declaration.
type DotNetRuntimeValue struct {
	SourceKind string
	Value      string
	Line       int
}

// DotNetRuntimeEvidence is private parser output bound to the exact source bytes.
type DotNetRuntimeEvidence struct {
	Values   []DotNetRuntimeValue
	Rejected int
	State    string
	ByteSize int
	Digest   string
}

// DotNetRuntimeDigest identifies the exact source revision, including line endings.
func DotNetRuntimeDigest(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

// NewDotNetRuntimeEvidence creates a bounded, digest-bound parser result.
func NewDotNetRuntimeEvidence(source []byte) DotNetRuntimeEvidence {
	e := DotNetRuntimeEvidence{State: "parsed_empty", ByteSize: len(source), Digest: DotNetRuntimeDigest(source)}
	if len(source) > 1<<20 {
		e.State = "over_cap"
	}
	return e
}

// Add records one detached version string after the credential and byte guard.
func (e *DotNetRuntimeEvidence) Add(kind, value string, line int) {
	if e.State == "over_cap" || e.State == "unavailable" || e.State == "over_tokens" {
		return
	}
	value = strings.TrimSpace(value)
	if line < 1 || !SafeVersionSpec(value) {
		e.Rejected++
		return
	}
	if len(e.Values) >= 128 {
		e.Values = nil
		e.State = "over_tokens"
		return
	}
	e.Values = append(e.Values, DotNetRuntimeValue{SourceKind: kind, Value: strings.Clone(value), Line: line})
	e.State = "parsed_values"
}
