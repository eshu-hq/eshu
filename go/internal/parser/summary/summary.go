// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// FunctionID is a durable, generation-independent identity for a function.
type FunctionID string

// NewFunctionID builds a stable FunctionID from durable attributes. None of the
// inputs may encode a generation or commit, so the identity is stable across
// runs (the StableFactKey discipline). Receiver may be empty for free functions.
func NewFunctionID(repo, pkg, receiver, name string) FunctionID {
	return FunctionID(strings.Join([]string{repo, pkg, receiver, name}, "\x1f"))
}

// ParamSink records that a parameter flows to a sink of a given kind within the
// function.
type ParamSink struct {
	Param    int
	SinkKind string
}

// CallArgFlow records that a parameter flows into a callee's argument (TITO).
type CallArgFlow struct {
	Callee FunctionID
	Param  int
	Arg    int
}

// Effects is a function's structural taint summary, independent of any callee's
// version. It is the "own facts" hashed into the content version.
type Effects struct {
	// ParamToReturn lists parameter indices that flow to the return value.
	ParamToReturn []int
	// ParamToSink lists parameters that flow to a sink within the function.
	ParamToSink []ParamSink
	// SourceToReturn lists internal source kinds that flow to the return value.
	SourceToReturn []string
	// ParamToCallArg lists parameter flows into callee arguments.
	ParamToCallArg []CallArgFlow
}

// Summary is a function summary with its resolved content version.
type Summary struct {
	ID      FunctionID
	Effects Effects
	// Callees are the distinct functions this one calls, sorted. Derived from
	// Effects.ParamToCallArg.
	Callees []FunctionID
	// Version is the content version: hash(structural facts ∪ external callee
	// versions).
	Version string
}

// callees returns the distinct, sorted callee IDs referenced by the effects.
func (e Effects) callees() []FunctionID {
	seen := map[FunctionID]struct{}{}
	for _, flow := range e.ParamToCallArg {
		if flow.Callee != "" {
			seen[flow.Callee] = struct{}{}
		}
	}
	out := make([]FunctionID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// StructuralHash returns the deterministic hash of a function's own effects,
// excluding callee versions. Persistence adapters store it as a diagnostic and
// idempotency aid, but Store versioning remains the source of truth.
func StructuralHash(e Effects) string {
	return structuralHash(e)
}

// structuralHash returns a deterministic hash of a function's own effects,
// excluding any callee version, so it changes only when the function's own facts
// change.
func structuralHash(e Effects) string {
	var b strings.Builder

	params := append([]int(nil), e.ParamToReturn...)
	sort.Ints(params)
	b.WriteString("pr:")
	for _, p := range params {
		b.WriteString(strconv.Itoa(p))
		b.WriteByte(',')
	}

	sinks := append([]ParamSink(nil), e.ParamToSink...)
	sort.Slice(sinks, func(i, j int) bool {
		if sinks[i].Param != sinks[j].Param {
			return sinks[i].Param < sinks[j].Param
		}
		return sinks[i].SinkKind < sinks[j].SinkKind
	})
	b.WriteString("|ps:")
	for _, s := range sinks {
		b.WriteString(strconv.Itoa(s.Param))
		b.WriteByte(':')
		b.WriteString(s.SinkKind)
		b.WriteByte(',')
	}

	sources := append([]string(nil), e.SourceToReturn...)
	sort.Strings(sources)
	b.WriteString("|sr:")
	for _, s := range sources {
		b.WriteString(s)
		b.WriteByte(',')
	}

	flows := append([]CallArgFlow(nil), e.ParamToCallArg...)
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].Callee != flows[j].Callee {
			return flows[i].Callee < flows[j].Callee
		}
		if flows[i].Param != flows[j].Param {
			return flows[i].Param < flows[j].Param
		}
		return flows[i].Arg < flows[j].Arg
	})
	b.WriteString("|ca:")
	for _, f := range flows {
		b.WriteString(string(f.Callee))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(f.Param))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(f.Arg))
		b.WriteByte(',')
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// contentVersion hashes a function's structural hash together with the sorted
// versions of its callees outside its own strongly-connected component.
func contentVersion(structHash string, externalCalleeVersions []string) string {
	versions := append([]string(nil), externalCalleeVersions...)
	sort.Strings(versions)
	var b strings.Builder
	b.WriteString(structHash)
	b.WriteByte('|')
	for _, v := range versions {
		b.WriteString(v)
		b.WriteByte(',')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
