// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package factdecode holds the reducer's fact-decode failure classification and
// per-fact quarantine mechanism, shared by the parent reducer package and its
// domain-family subpackages.
//
// It owns the mechanism, not the decoders. FactDecodeError classifies a
// malformed payload as a terminal dead letter rather than a retry;
// PartitionDecodeFailures separates a quarantinable input_invalid fact from a
// fatal error; QuarantinedFact carries the per-fact dead letter until
// RecordQuarantinedFacts persists it through a QuarantinedFactWriter taken from
// the context. The per-fact-kind Decode* functions stay with the families that
// own those fact kinds.
//
// The classification string is compared by value, not by import: the contracts
// module cannot import go/internal, so "input_invalid" is byte-equal to
// projector.TriageClassInputInvalid and factschema.ClassificationInputInvalid by
// the Contract System v1 by-value contract rather than by a shared constant.
package factdecode
